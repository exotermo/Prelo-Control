package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port         string
	BindHost     string
	Database     DatabaseConfig
	Gateway      GatewayConfig
	APIAuth      APIAuthConfig
	RedisAddr    string
	WorkerCount  int
	ToolLimits   ToolLimitsConfig
	Dashboard    DashboardConfig
	Bridge       BridgeConfig
	Servers      ServersConfig
	Integrations IntegrationsConfig
	Files        FilesConfig
}

// FilesConfig backs Fase PA (project files sealed at rest). Key is its own 32-byte Base64 master
// key — never shared with the TOTP, SSH or integrations keys. Without it, file routes are off.
type FilesConfig struct {
	Key string
	Dir string
}

// IntegrationsConfig backs Fase I (API keys + outbound webhooks). Key is a dedicated 32-byte
// Base64 key for webhook signing secrets — never shared with the TOTP or SSH-credential keys.
// AllowPrivateTargets turns off the webhook SSRF guard (and allows plain http://); dev only.
type IntegrationsConfig struct {
	Key                 string
	AllowPrivateTargets bool
}

// ServersConfig backs Fase S1 (server registration + health). CredentialsKey must be a
// dedicated 32-byte Base64 key, never the same value as Dashboard.MfaKey — a server's SSH
// private key and a human's TOTP secret must never share an encryption key.
type ServersConfig struct {
	CredentialsKey string
}

// BridgeConfig backs Fase G2 (prelo-dashboard's Configurações page managing
// prelo-messaging-bridge's owner-contacts list). Optional: prelo-core runs fine without it, the
// settings endpoints just report the integration as unconfigured.
type BridgeConfig struct {
	AdminURL   string
	AdminToken string
}

// DashboardConfig backs the human-login surface (Fase G1) — separate from APIAuth, which
// authenticates machine callers (messaging-core, the bridge).
type DashboardConfig struct {
	MfaKey         string
	PublicURL      string
	SecureCookie   bool
	AdminToken     string
	AllowedOrigins []string
	SMTP           SMTPConfig
}

type SMTPConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Auth     bool
	StartTLS bool
	From     string
}

// APIAuthConfig authenticates callers of prelo-core. It is intentionally separate from the
// gateway credentials: the latter are outbound credentials and must never open the public API.
type APIAuthConfig struct {
	Enabled                bool
	AllowInsecureLocalOnly bool
	Secret                 string
	Issuer                 string
	Audience               string
	ClockSkewSeconds       int
}

type ToolLimitsConfig struct {
	TimeoutSeconds int
	MaxArgsBytes   int
	MaxResultBytes int
	MaxConcurrent  int
}

// GatewayConfig reuses the same env vars the Java service already reads
// (PRELO_GATEWAY_BASE_URL/JWT_SECRET/ISSUER/AUDIENCE), so both services can run against the
// same llm-gateway with a single shared .env.
type GatewayConfig struct {
	BaseURL   string
	JWTSecret string
	Issuer    string
	Audience  string
}

// DatabaseConfig mirrors the Java service's approach of taking host/user/password as
// discrete env vars (SPRING_DATASOURCE_*) rather than a single connection string — a
// password containing URL-reserved characters (':', '@', '/', ...) would otherwise break
// naive string interpolation into a DSN.
type DatabaseConfig struct {
	Host     string
	Port     string
	Name     string
	Schema   string
	User     string
	Password string
}

func Load() Config {
	return Config{
		Port:     getenv("PRELO_PORT", "8080"),
		BindHost: getenv("PRELO_BIND_HOST", "0.0.0.0"),
		Database: DatabaseConfig{
			Host:     getenv("PRELO_DB_HOST", ""),
			Port:     getenv("PRELO_DB_PORT", "5432"),
			Name:     getenv("PRELO_DB_NAME", "prelo"),
			Schema:   getenv("PRELO_DB_SCHEMA", "prelo_app"),
			User:     getenv("PRELO_DB_USER", ""),
			Password: getenv("PRELO_DB_PASSWORD", ""),
		},
		Gateway: GatewayConfig{
			BaseURL:   getenv("PRELO_GATEWAY_BASE_URL", ""),
			JWTSecret: getenv("PRELO_GATEWAY_JWT_SECRET", ""),
			Issuer:    getenv("PRELO_GATEWAY_ISSUER", "prelo-dev"),
			Audience:  getenv("PRELO_GATEWAY_AUDIENCE", "llm-gateway"),
		},
		APIAuth: APIAuthConfig{
			Enabled:                getenvBool("PRELO_API_AUTH_ENABLED", true),
			AllowInsecureLocalOnly: getenvBool("PRELO_ALLOW_INSECURE_LOCAL_ONLY", false),
			Secret:                 getenv("PRELO_API_JWT_SECRET", ""),
			Issuer:                 getenv("PRELO_API_JWT_ISSUER", "messaging-core"),
			Audience:               getenv("PRELO_API_JWT_AUDIENCE", "messaging-core"),
			ClockSkewSeconds:       getenvInt("PRELO_API_JWT_CLOCK_SKEW_SECONDS", 30),
		},
		ToolLimits: ToolLimitsConfig{
			TimeoutSeconds: getenvInt("PRELO_TOOL_TIMEOUT_SECONDS", 30),
			MaxArgsBytes:   getenvInt("PRELO_TOOL_MAX_ARGS_BYTES", 64*1024),
			MaxResultBytes: getenvInt("PRELO_TOOL_MAX_RESULT_BYTES", 64*1024),
			MaxConcurrent:  getenvInt("PRELO_TOOL_MAX_CONCURRENT", 16),
		},
		RedisAddr:   getenv("PRELO_REDIS_ADDR", ""),
		WorkerCount: getenvInt("PRELO_WORKER_COUNT", 4),
		Dashboard: DashboardConfig{
			MfaKey:         getenv("PRELO_DASHBOARD_MFA_KEY", ""),
			PublicURL:      getenv("PRELO_DASHBOARD_PUBLIC_URL", "http://127.0.0.1:5176"),
			SecureCookie:   getenvBool("PRELO_DASHBOARD_SECURE_COOKIE", false),
			AdminToken:     getenv("PRELO_ADMIN_TOKEN", ""),
			AllowedOrigins: getenvList("PRELO_DASHBOARD_ALLOWED_ORIGINS", "http://127.0.0.1:5176,http://localhost:5176"),
			SMTP: SMTPConfig{
				Host:     getenv("PRELO_SMTP_HOST", ""),
				Port:     getenv("PRELO_SMTP_PORT", "587"),
				User:     getenv("PRELO_SMTP_USER", ""),
				Password: getenv("PRELO_SMTP_PASSWORD", ""),
				Auth:     getenvBool("PRELO_SMTP_AUTH", true),
				StartTLS: getenvBool("PRELO_SMTP_STARTTLS", true),
				From:     getenv("PRELO_DASHBOARD_MAIL_FROM", ""),
			},
		},
		Bridge: BridgeConfig{
			AdminURL:   getenv("PRELO_BRIDGE_ADMIN_URL", ""),
			AdminToken: getenv("PRELO_BRIDGE_ADMIN_TOKEN", ""),
		},
		Servers: ServersConfig{
			CredentialsKey: getenv("PRELO_SERVER_CREDENTIALS_KEY", ""),
		},
		Files: FilesConfig{
			Key: getenv("PRELO_FILES_KEY", ""),
			Dir: getenv("PRELO_FILES_DIR", "/data/project-files"),
		},
		Integrations: IntegrationsConfig{
			Key:                 getenv("PRELO_INTEGRATIONS_KEY", ""),
			AllowPrivateTargets: getenvBool("PRELO_WEBHOOK_ALLOW_PRIVATE_TARGETS", false),
		},
	}
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// URL builds a postgres DSN via net/url, which percent-encodes User/Password safely
// regardless of what characters they contain.
func (d DatabaseConfig) URL() string {
	if d.Host == "" {
		return ""
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   d.Host + ":" + d.Port,
		Path:   "/" + d.Name,
	}
	q := u.Query()
	q.Set("search_path", d.Schema)
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getenvList splits a comma-separated env var, trimming whitespace and dropping empty entries —
// used for PRELO_DASHBOARD_ALLOWED_ORIGINS (an explicit allowlist, never "*", since the
// dashboard session cookie makes this a credentialed CORS policy).
func getenvList(key, fallback string) []string {
	raw := getenv(key, fallback)
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
