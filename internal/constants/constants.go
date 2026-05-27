package constants

import "time"

// Default configuration values
const (
	DefaultFullnodeURL     = "http://localhost:20000"
	DefaultProxyPort       = "8080"
	DefaultBindAddr        = "127.0.0.1"
	DefaultRateLimitPerMin = 60
	DefaultRateLimitBurst  = 10
	EnvFileName            = ".env"
)

// Environment variable keys
const (
	EnvFullnodeURL     = "FULLNODE_URL"
	EnvProxyPort       = "PROXY_PORT"
	EnvProxyBindAddr   = "PROXY_BIND_ADDR"
	EnvProxySecretKey  = "PROXY_SECRET_KEY"
	EnvRateLimitPerMin = "RATE_LIMIT_PER_MIN"
	EnvRateLimitBurst  = "RATE_LIMIT_BURST"
)

// HTTP headers
const (
	HeaderAPIKey          = "X-API-KEY"
	HeaderContentType     = "Content-Type"
	HeaderContentEncoding = "Content-Encoding"
	HeaderAcceptEncoding  = "Accept-Encoding"
	HeaderContentLength   = "Content-Length"
	HeaderVary            = "Vary"
	HeaderRetryAfter      = "Retry-After"
	HeaderXRealIP         = "X-Real-IP"
	HeaderXForwardedFor   = "X-Forwarded-For"
	HeaderServer          = "Server"
	HeaderXPoweredBy      = "X-Powered-By"
)

// Content types
const (
	ContentTypeJSON = "application/json"
)

// Server timeouts
const (
	ServerReadTimeout  = 15 * time.Second
	ServerWriteTimeout = 120 * time.Second
	ServerIdleTimeout  = 60 * time.Second
	ShutdownTimeout    = 10 * time.Second
)

// Rate limiter
const (
	RateLimitCleanupInterval = 5 * time.Minute
	RateLimitStaleThreshold  = 5 * time.Minute
	RetryAfterSeconds        = "60"
)

// Request limits
const (
	MaxBodySize int64 = 1 << 20 // 1MB
)

// JSON response bodies
const (
	ResponseHealthy         = `{"status":"healthy"}`
	ResponseUnauthorized    = `{"status":false,"message":"Unauthorized: Invalid or missing X-API-KEY"}`
	ResponseForbidden       = `{"status":false,"message":"Forbidden"}`
	ResponseTooManyRequests = `{"status":false,"message":"Too Many Requests"}`
	ResponseInternalError   = `{"status":false,"message":"Internal Server Error"}`
	ResponseBadGateway      = `{"status":false,"message":"Backend fullnode unavailable"}`
)

// Whitelisted API endpoints (path → allowed methods)
var WhitelistedEndpoints = map[string][]string{
	"/rubix/v1/fullnode/sync-token-chain": {"POST"},
}
