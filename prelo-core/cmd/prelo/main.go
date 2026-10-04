package main

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/exotermo/prelo-core/internal/api"
	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/config"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/agentregistry"
	"github.com/exotermo/prelo-core/internal/infrastructure/bridgeclient"
	"github.com/exotermo/prelo-core/internal/infrastructure/filestore"
	appgateway "github.com/exotermo/prelo-core/internal/infrastructure/gateway"
	"github.com/exotermo/prelo-core/internal/infrastructure/mail"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
	"github.com/exotermo/prelo-core/internal/infrastructure/queue"
	"github.com/exotermo/prelo-core/internal/infrastructure/security"
	serverssh "github.com/exotermo/prelo-core/internal/infrastructure/ssh"
	"github.com/exotermo/prelo-core/internal/infrastructure/toolregistry"
	"github.com/exotermo/prelo-core/internal/infrastructure/tools"
	"github.com/exotermo/prelo-core/internal/infrastructure/webhook"
	"github.com/exotermo/prelo-core/internal/infrastructure/worker"
	"github.com/exotermo/prelo-core/internal/platform"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	if !cfg.APIAuth.Enabled && (!cfg.APIAuth.AllowInsecureLocalOnly || !isLoopback(cfg.BindHost)) {
		log.Fatalf("API authentication cannot be disabled unless PRELO_ALLOW_INSECURE_LOCAL_ONLY=true and PRELO_BIND_HOST is loopback")
	}

	// projectRepo/projectMemberRepo (Fase W) are filled in below once the DB pool exists —
	// JWTAuthMiddleware is only constructed after that, since resolveProject needs both.
	var projectRepo *postgres.ProjectRepository
	var projectMemberRepo *postgres.ProjectMemberRepository
	// Fase I: stays a nil interface (not a typed nil pointer) unless integrations are configured,
	// so the middleware cleanly rejects every "prl_" bearer in that case.
	var apiKeyAuth api.ApiKeyAuthenticator

	mux := http.NewServeMux()
	mux.HandleFunc("GET /actuator/health", platform.HealthHandler())

	dbURL := cfg.Database.URL()
	if dbURL != "" {
		if err := postgres.Migrate(dbURL); err != nil {
			log.Fatalf("migration failed: %v", err)
		}
		log.Print("migrations applied")

		pool, err := postgres.NewPool(ctx, dbURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer pool.Close()

		agents, err := agentregistry.LoadDefault()
		if err != nil {
			log.Fatalf("failed to load agent catalog: %v", err)
		}

		taskRepo := postgres.NewTaskRepository(pool)
		executionRepo := postgres.NewExecutionRepository(pool)
		jobRepo := postgres.NewExecutionJobRepository(pool)
		manualContextRepo := postgres.NewManualContextRepository(pool)
		snapshotRepo := postgres.NewContextSnapshotRepository(pool)
		llmExecutionRepo := postgres.NewLlmExecutionRepository(pool)

		createTask := application.NewCreateTaskUseCase(taskRepo, manualContextRepo, agents)
		gatewayClient := appgateway.NewClient(appgateway.Config{
			BaseURL:   cfg.Gateway.BaseURL,
			JWTSecret: cfg.Gateway.JWTSecret,
			Issuer:    cfg.Gateway.Issuer,
			Audience:  cfg.Gateway.Audience,
		})
		chatService := application.NewChatService(gatewayClient, llmExecutionRepo)
		contextResolver := application.NewContextResolver(manualContextRepo, snapshotRepo)

		// Publisher/enqueue wired before the tool catalog: delegate_to_agent (Fase C) needs a
		// working EnqueueExecutionUseCase to actually enqueue the child Task it creates.
		var publisher application.JobPublisher = noopPublisher{}
		var stream *queue.Stream
		if cfg.RedisAddr != "" {
			redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
			stream = queue.NewStream(redisClient)
			if err := stream.EnsureGroup(ctx); err != nil {
				log.Fatalf("failed to set up redis consumer group: %v", err)
			}
			publisher = stream
		} else {
			log.Print("PRELO_REDIS_ADDR not set, skipping queue/worker wiring (enqueued jobs will only run once the sweeper lands)")
		}
		enqueueExecution := application.NewEnqueueExecutionUseCase(taskRepo, executionRepo, jobRepo, publisher)

		// Etapa 7/8 (ADR-004): curated tool catalog, permission-gated invocation, and the
		// approval queue for anything MODERATE/HIGH risk. See EchoTool's doc comment — it exists
		// only to exercise the approval path until a real moderate/high-risk tool lands.
		// Fase C adds delegate_to_agent, which needs taskRepo/agents/enqueueExecution to spawn
		// and enqueue a child Task.
		toolCallRepo := postgres.NewToolCallRepository(pool)
		approvalRepo := postgres.NewApprovalRepository(pool)
		turnRepo := postgres.NewExecutionTurnRepository(pool)
		suspensionRepo := postgres.NewExecutionSuspensionRepository(pool)
		toolRegistry := toolregistry.NewStatic(tools.NewCurrentTimeTool(), tools.NewEchoTool(), tools.NewDelegateTool(taskRepo, agents, enqueueExecution))
		permissionPolicy := application.NewDefaultPermissionPolicy()
		toolLimits := application.ToolExecutionLimits{
			Timeout:        time.Duration(cfg.ToolLimits.TimeoutSeconds) * time.Second,
			MaxArgsBytes:   cfg.ToolLimits.MaxArgsBytes,
			MaxResultBytes: cfg.ToolLimits.MaxResultBytes,
			MaxConcurrent:  cfg.ToolLimits.MaxConcurrent,
		}
		invokeTool := application.NewInvokeToolUseCaseWithLimits(executionRepo, agents, toolRegistry, permissionPolicy, toolCallRepo, approvalRepo, toolLimits)
		decideApproval := application.NewDecideApprovalUseCase(approvalRepo, toolCallRepo, toolRegistry, executionRepo, turnRepo, suspensionRepo, jobRepo)
		toolHandler := api.NewToolHandler(invokeTool, toolRegistry)
		approvalHandler := api.NewApprovalHandler(decideApproval, approvalRepo)

		// Fase B: the agent loop owns every Gateway call now (single-turn or multi-turn,
		// tool-gated) — ProcessJobUseCase just claims the job and finalizes whatever the loop
		// returns (including, per Fase C, waking up a delegating parent once its child Task
		// finishes — see resolveParentSubtaskSuspension).
		agentLoop := application.NewRunAgentLoopUseCase(gatewayClient, toolRegistry, invokeTool, turnRepo, suspensionRepo, jobRepo)
		processJob := application.NewProcessJobUseCase(taskRepo, executionRepo, jobRepo, agents, contextResolver, snapshotRepo, agentLoop, turnRepo, suspensionRepo, workerID())

		if stream != nil {
			worker.Start(ctx, cfg.WorkerCount, stream, processJob, workerID())
			log.Printf("worker pool started: %d consumers against %s", cfg.WorkerCount, cfg.RedisAddr)
		}

		sweeper := worker.NewSweeper(jobRepo, processJob, 5*time.Second, 20)
		go sweeper.Run(ctx)

		taskHandler := api.NewTaskHandler(createTask, taskRepo, executionRepo, enqueueExecution)
		chatHandler := api.NewChatHandler(chatService)
		observabilityHandler := api.NewObservabilityHandler(taskRepo, executionRepo, turnRepo)
		pipelineHandler := api.NewPipelineHandler(taskRepo, executionRepo, suspensionRepo)

		api.RegisterRoutes(mux, taskHandler, chatHandler, toolHandler, approvalHandler, observabilityHandler)
		api.RegisterPipelineRoutes(mux, pipelineHandler)

		// Fase G1: human login (password + mandatory TOTP) for prelo-dashboard.
		if cfg.Dashboard.MfaKey == "" {
			log.Fatal("PRELO_DASHBOARD_MFA_KEY is required (32 random bytes, base64: openssl rand -base64 32)")
		}
		mfaCipher, err := security.NewMfaCipher(cfg.Dashboard.MfaKey)
		if err != nil {
			log.Fatalf("invalid PRELO_DASHBOARD_MFA_KEY: %v", err)
		}
		dashboardUsers := postgres.NewDashboardUserRepository(pool)
		dashboardTokens := postgres.NewDashboardAuthTokenRepository(pool)
		dashboardRecoveryCodes := postgres.NewDashboardRecoveryCodeRepository(pool)
		dashboardRateLimiter := postgres.NewDashboardRateLimiter(pool)
		dashboardMailer := mail.NewSMTPMailer(mail.Config{
			Host: cfg.Dashboard.SMTP.Host, Port: cfg.Dashboard.SMTP.Port, User: cfg.Dashboard.SMTP.User,
			Password: cfg.Dashboard.SMTP.Password, Auth: cfg.Dashboard.SMTP.Auth, StartTLS: cfg.Dashboard.SMTP.StartTLS,
			From: cfg.Dashboard.SMTP.From, PublicURL: cfg.Dashboard.PublicURL,
		})
		dashboardAuthService := application.NewDashboardAuthService(dashboardUsers, dashboardTokens, dashboardRecoveryCodes,
			dashboardRateLimiter, dashboardMailer, mfaCipher, security.NewTotpProvider("Prelo Control"), security.NewPasswordHasher(),
			api.NewDashboardSessionIssuer(cfg.APIAuth))
		dashboardAuthHandler := api.NewDashboardAuthHandler(dashboardAuthService, cfg.Dashboard.SecureCookie)
		dashboardBootstrapHandler := api.NewDashboardBootstrapHandler(dashboardAuthService, cfg.Dashboard.AdminToken)
		api.RegisterDashboardAuthRoutes(mux, dashboardAuthHandler, dashboardBootstrapHandler)

		// Fase H: role-gated multi-user management (Usuários page) — reuses DashboardAuthService's
		// Invite/ListUsers/ChangeRole, same repository as login itself.
		userManagementHandler := api.NewUserManagementHandler(dashboardAuthService)
		api.RegisterUserManagementRoutes(mux, userManagementHandler)

		// Fase G2: prelo-dashboard's Configurações page manages the bridge's owner-contacts
		// list through this proxy — optional, prelo-core runs fine without PRELO_BRIDGE_ADMIN_*.
		bridgeClient := bridgeclient.New(cfg.Bridge.AdminURL, cfg.Bridge.AdminToken)
		if !bridgeClient.Configured() {
			log.Print("PRELO_BRIDGE_ADMIN_URL/PRELO_BRIDGE_ADMIN_TOKEN not set, /api/v1/settings/owner-contacts will report the bridge integration as unconfigured")
		}
		settingsHandler := api.NewSettingsHandler(bridgeClient)
		api.RegisterSettingsRoutes(mux, settingsHandler)

		var checkServerHealthUC *application.CheckServerHealthUseCase
		// Fase S1: server registration + on-demand SSH health check. Optional, same
		// graceful-degradation pattern as the bridge integration — a fresh deployment that
		// hasn't set the new key yet keeps running everything else.
		if cfg.Servers.CredentialsKey == "" {
			log.Print("PRELO_SERVER_CREDENTIALS_KEY not set, /api/v1/servers is disabled")
		} else {
			serverCredentialCipher, err := security.NewMfaCipher(cfg.Servers.CredentialsKey)
			if err != nil {
				log.Fatalf("invalid PRELO_SERVER_CREDENTIALS_KEY: %v", err)
			}
			serverRepo := postgres.NewServerRepository(pool)
			registerServer := application.NewRegisterServerUseCase(serverRepo, serverssh.NewRegistrar(serverssh.DefaultTimeout), serverCredentialCipher)
			checkServerHealth := application.NewCheckServerHealthUseCase(serverRepo, serverssh.NewChecker(serverCredentialCipher, serverssh.DefaultTimeout))
			checkServerHealthUC = checkServerHealth
			serverHandler := api.NewServerHandler(registerServer, checkServerHealth, serverRepo)
			api.RegisterServerRoutes(mux, serverHandler)
		}

		// Fase W: projects (isolated work environments) scoping Tasks/Servers/Pipeline/
		// Approvals. projectRepo/projectMemberRepo are also what JWTAuthMiddleware uses below
		// to resolve X-Project-Id and enforce membership.
		projectRepo = postgres.NewProjectRepository(pool)
		projectMemberRepo = postgres.NewProjectMemberRepository(pool)
		projectHandler := api.NewProjectHandler(projectRepo, projectMemberRepo)
		api.RegisterProjectRoutes(mux, projectHandler)

		// Fase PA: project settings (default agent, instructions) feed task creation and context.
		createTask.SetProjectReader(projectRepo)
		contextResolver.SetProjectReader(projectRepo)
		var filePurger *application.ProjectFileService
		if cfg.Files.Key == "" {
			log.Print("PRELO_FILES_KEY not set, project files are disabled")
		} else {
			filesKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cfg.Files.Key))
			if err != nil || len(filesKey) != 32 {
				log.Fatal("invalid PRELO_FILES_KEY (32 random bytes, base64: openssl rand -base64 32)")
			}
			store, err := filestore.NewStore(cfg.Files.Dir, filesKey)
			if err != nil {
				log.Fatalf("project files store: %v", err)
			}
			fileService := application.NewProjectFileService(postgres.NewProjectFileRepository(pool), filestore.Blobs{Store: store})
			filePurger = fileService
			api.RegisterProjectFileRoutes(mux, api.NewProjectFileHandler(fileService, projectRepo, projectMemberRepo))
		}
		// The registry also lists its catalog; the AgentRegistry port just doesn't expose it.
		catalog, _ := agents.(agentLister)
		if filePurger != nil {
			projectHandler.SetSettingsDependencies(catalog, filePurger)
		} else {
			projectHandler.SetSettingsDependencies(catalog, nil)
		}

		// Fase M: model connections live in the llm-gateway's vault; prelo-core only relays them
		// (never stores a provider key — AGENTS.md) using a separate llm:admin service token.
		gatewayAdmin := appgateway.NewAdminClient(appgateway.Config{
			BaseURL: cfg.Gateway.BaseURL, JWTSecret: cfg.Gateway.JWTSecret,
			Issuer: cfg.Gateway.Issuer, Audience: cfg.Gateway.Audience,
		})
		api.RegisterModelConnectionRoutes(mux, api.NewModelConnectionHandler(gatewayAdmin, projectRepo, projectMemberRepo))

		// Fase I: per-project API keys + outbound webhooks. Optional like Servers — without
		// PRELO_INTEGRATIONS_KEY the routes stay off and events publish nowhere.
		if cfg.Integrations.Key == "" {
			log.Print("PRELO_INTEGRATIONS_KEY not set, /api/v1/integrations and webhooks are disabled")
		} else {
			integrationsCipher, err := security.NewMfaCipher(cfg.Integrations.Key)
			if err != nil {
				log.Fatalf("invalid PRELO_INTEGRATIONS_KEY: %v", err)
			}
			if cfg.Integrations.AllowPrivateTargets {
				log.Print("WARNING: PRELO_WEBHOOK_ALLOW_PRIVATE_TARGETS=true — webhook SSRF guard is OFF (dev only)")
			}
			apiKeyRepo := postgres.NewApiKeyRepository(pool)
			webhookRepo := postgres.NewWebhookRepository(pool)
			deliveryRepo := postgres.NewWebhookDeliveryRepository(pool)
			integrationService := application.NewIntegrationService(apiKeyRepo, webhookRepo, deliveryRepo, integrationsCipher, cfg.Integrations.AllowPrivateTargets)
			apiKeyAuth = integrationService
			api.RegisterIntegrationRoutes(mux, api.NewIntegrationHandler(integrationService))

			dispatcher := application.NewWebhookDispatcher(webhookRepo, deliveryRepo)
			processJob.SetEventPublisher(dispatcher)
			agentLoop.SetEventPublisher(dispatcher)
			if checkServerHealthUC != nil {
				checkServerHealthUC.SetEventPublisher(dispatcher)
			}
			sender := webhook.NewSender(deliveryRepo, webhookRepo, integrationsCipher, webhook.NewHTTPClient(cfg.Integrations.AllowPrivateTargets), 3*time.Second, 20)
			go sender.Run(ctx)
		}
	} else {
		log.Print("PRELO_DB_HOST not set, skipping migrations and API wiring")
	}

	var authMiddleware *api.JWTAuthMiddleware
	if cfg.APIAuth.Enabled {
		var err error
		authMiddleware, err = api.NewJWTAuthMiddleware(cfg.APIAuth, projectRepo, projectMemberRepo, apiKeyAuth)
		if err != nil {
			log.Fatalf("invalid API authentication configuration: %v", err)
		}
	}

	addr := cfg.BindHost + ":" + cfg.Port
	log.Printf("prelo-core listening on %s", addr)
	var handler http.Handler = mux
	if authMiddleware != nil {
		handler = authMiddleware.Handler(handler)
	}
	if err := http.ListenAndServe(addr, platform.CORS(handler, cfg.Dashboard.AllowedOrigins)); err != nil {
		log.Fatal(err)
	}
}

type agentLister interface {
	application.AgentRegistry
	List() []domain.AgentDefinition
}

func isLoopback(host string) bool {
	host = strings.TrimSpace(host)
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func workerID() string {
	host, err := os.Hostname()
	if err != nil {
		return "prelo-core"
	}
	return host
}

// noopPublisher is used when Redis isn't configured — enqueued jobs still land durably in
// Postgres, just without a low-latency wake signal, matching ADR-013's "Redis is dispatch
// only, never durability" design.
type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string) {}
