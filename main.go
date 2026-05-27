package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Configure JSON structured logging to stdout
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting Rubix Fullnode Proxy service...")

	// Load configuration
	cfg, err := LoadConfig()
	if err != nil {
		slog.Error("Failed to load configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// Parse target fullnode URL
	targetURL, err := url.Parse(cfg.FullnodeURL)
	if err != nil {
		slog.Error("Failed to parse fullnode URL", slog.String("url", cfg.FullnodeURL), slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("Configuration loaded successfully",
		slog.String("proxy_bind", cfg.ProxyBindAddr),
		slog.String("proxy_port", cfg.ProxyPort),
		slog.String("target_fullnode", cfg.FullnodeURL),
		slog.Int("rate_limit_per_min", cfg.RateLimitPerMin),
		slog.Int("rate_limit_burst", cfg.RateLimitBurst),
	)

	// Initialize per-IP rate limiter
	limiter := NewRateLimiter(cfg.RateLimitPerMin, cfg.RateLimitBurst)

	// Initialize mux router
	mux := http.NewServeMux()

	// Public Health Check Endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Setup Reverse Proxy handler
	proxyHandler := NewReverseProxy(targetURL)

	// Apply protection middlewares only to proxied requests
	var protectedHandler http.Handler = proxyHandler
	protectedHandler = WhitelistMiddleware(protectedHandler)
	protectedHandler = AuthMiddleware(cfg.ProxySecretKey)(protectedHandler)
	protectedHandler = MaxBodySizeMiddleware(1024 * 1024)(protectedHandler) // 1MB limit

	// Register protected handler to all other routes
	mux.Handle("/", protectedHandler)

	// Apply global middlewares to all endpoints.
	// Order (outermost → innermost): Recovery → Logging → RateLimit → Gzip → [mux → auth → whitelist → proxy]
	var mainHandler http.Handler = mux
	mainHandler = GzipMiddleware(mainHandler)
	mainHandler = RateLimitMiddleware(limiter)(mainHandler)
	mainHandler = LoggingMiddleware(mainHandler)
	mainHandler = RecoveryMiddleware(mainHandler)

	// Bind to localhost only — nginx handles external traffic
	serverAddr := cfg.ProxyBindAddr + ":" + cfg.ProxyPort
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      mainHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second, // Explorer's HTTP timeout is 2m
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for shutdown signals
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		slog.Info("Proxy server listening on " + serverAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server failed to start", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Wait for termination signal
	sig := <-stopChan
	slog.Info("Received shutdown signal", slog.String("signal", sig.String()))

	// Graceful shutdown context with 10-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	slog.Info("Shutting down proxy server gracefully...")
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown failed", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("Proxy server stopped cleanly.")
}
