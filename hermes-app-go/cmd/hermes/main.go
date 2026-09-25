package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/exotermo/hermes-app-go/internal/api"
	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/config"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/agentregistry"
	appgateway "github.com/exotermo/hermes-app-go/internal/infrastructure/gateway"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/queue"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/toolregistry"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/tools"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/worker"
	"github.com/exotermo/hermes-app-go/internal/platform"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	var authMiddleware *api.JWTAuthMiddleware
	if cfg.APIAuth.Enabled {
		var err error
		authMiddleware, err = api.NewJWTAuthMiddleware(cfg.APIAuth)
		if err != nil {
			log.Fatalf("invalid API authentication configuration: %v", err)
		}
	} else if !cfg.APIAuth.AllowInsecureLocalOnly || !isLoopback(cfg.BindHost) {
		log.Fatalf("API authentication cannot be disabled unless HERMES_GO_ALLOW_INSECURE_LOCAL_ONLY=true and HERMES_GO_BIND_HOST is loopback")
	}

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
		hermesExecutionRepo := postgres.NewHermesExecutionRepository(pool)

		createTask := application.NewCreateTaskUseCase(taskRepo, manualContextRepo, agents)
		gatewayClient := appgateway.NewClient(appgateway.Config{
			BaseURL:   cfg.Gateway.BaseURL,
			JWTSecret: cfg.Gateway.JWTSecret,
			Issuer:    cfg.Gateway.Issuer,
			Audience:  cfg.Gateway.Audience,
		})
		chatService := application.NewChatService(gatewayClient, hermesExecutionRepo)
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
			log.Print("HERMES_GO_REDIS_ADDR not set, skipping queue/worker wiring (enqueued jobs will only run once the sweeper lands)")
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

		api.RegisterRoutes(mux, taskHandler, chatHandler, toolHandler, approvalHandler, observabilityHandler)
	} else {
		log.Print("HERMES_GO_DB_HOST not set, skipping migrations and API wiring")
	}

	addr := cfg.BindHost + ":" + cfg.Port
	log.Printf("hermes-app-go listening on %s", addr)
	var handler http.Handler = mux
	if authMiddleware != nil {
		handler = authMiddleware.Handler(handler)
	}
	if err := http.ListenAndServe(addr, platform.CORS(handler)); err != nil {
		log.Fatal(err)
	}
}

func isLoopback(host string) bool {
	host = strings.TrimSpace(host)
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func workerID() string {
	host, err := os.Hostname()
	if err != nil {
		return "hermes-go"
	}
	return host
}

// noopPublisher is used when Redis isn't configured — enqueued jobs still land durably in
// Postgres, just without a low-latency wake signal, matching ADR-013's "Redis is dispatch
// only, never durability" design.
type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string) {}
