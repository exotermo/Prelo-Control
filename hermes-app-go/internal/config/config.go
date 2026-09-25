package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port        string
	BindHost    string
	Database    DatabaseConfig
	Gateway     GatewayConfig
	APIAuth     APIAuthConfig
	RedisAddr   string
	WorkerCount int
	ToolLimits  ToolLimitsConfig
}

// APIAuthConfig authenticates callers of hermes-go. It is intentionally separate from the
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
// (HERMES_GATEWAY_BASE_URL/JWT_SECRET/ISSUER/AUDIENCE), so both services can run against the
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
		Port:     getenv("HERMES_GO_PORT", "8080"),
		BindHost: getenv("HERMES_GO_BIND_HOST", "0.0.0.0"),
		Database: DatabaseConfig{
			Host:     getenv("HERMES_GO_DB_HOST", ""),
			Port:     getenv("HERMES_GO_DB_PORT", "5432"),
			Name:     getenv("HERMES_GO_DB_NAME", "hermes"),
			Schema:   getenv("HERMES_GO_DB_SCHEMA", "hermes_app"),
			User:     getenv("HERMES_GO_DB_USER", ""),
			Password: getenv("HERMES_GO_DB_PASSWORD", ""),
		},
		Gateway: GatewayConfig{
			BaseURL:   getenv("HERMES_GATEWAY_BASE_URL", ""),
			JWTSecret: getenv("HERMES_GATEWAY_JWT_SECRET", ""),
			Issuer:    getenv("HERMES_GATEWAY_ISSUER", "hermes-dev"),
			Audience:  getenv("HERMES_GATEWAY_AUDIENCE", "llm-gateway"),
		},
		APIAuth: APIAuthConfig{
			Enabled:                getenvBool("HERMES_GO_API_AUTH_ENABLED", true),
			AllowInsecureLocalOnly: getenvBool("HERMES_GO_ALLOW_INSECURE_LOCAL_ONLY", false),
			Secret:                 getenv("HERMES_GO_API_JWT_SECRET", ""),
			Issuer:                 getenv("HERMES_GO_API_JWT_ISSUER", "messaging-core"),
			Audience:               getenv("HERMES_GO_API_JWT_AUDIENCE", "messaging-core"),
			ClockSkewSeconds:       getenvInt("HERMES_GO_API_JWT_CLOCK_SKEW_SECONDS", 30),
		},
		ToolLimits: ToolLimitsConfig{
			TimeoutSeconds: getenvInt("HERMES_GO_TOOL_TIMEOUT_SECONDS", 30),
			MaxArgsBytes:   getenvInt("HERMES_GO_TOOL_MAX_ARGS_BYTES", 64*1024),
			MaxResultBytes: getenvInt("HERMES_GO_TOOL_MAX_RESULT_BYTES", 64*1024),
			MaxConcurrent:  getenvInt("HERMES_GO_TOOL_MAX_CONCURRENT", 16),
		},
		RedisAddr:   getenv("HERMES_GO_REDIS_ADDR", ""),
		WorkerCount: getenvInt("HERMES_GO_WORKER_COUNT", 4),
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
