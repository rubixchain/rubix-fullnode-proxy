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

	"rubix-fullnode-proxy/internal/config"
	"rubix-fullnode-proxy/internal/constants"
	"rubix-fullnode-proxy/internal/middleware"
	"rubix-fullnode-proxy/internal/proxy"
	"rubix-fullnode-proxy/internal/response"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting Rubix Fullnode Proxy service...")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", slog.Any("error", err))
		os.Exit(1)
	}

	targetURL, err := url.Parse(cfg.FullnodeURL)
	if err != nil {
		slog.Error("Failed to parse fullnode URL",
			slog.String("url", cfg.FullnodeURL),
			slog.Any("error", err),
		)
		os.Exit(1)
	}

	slog.Info("Configuration loaded successfully",
		slog.String("proxy_bind", cfg.ProxyBindAddr),
		slog.String("proxy_port", cfg.ProxyPort),
		slog.String("target_fullnode", cfg.FullnodeURL),
		slog.Int("rate_limit_per_min", cfg.RateLimitPerMin),
		slog.Int("rate_limit_burst", cfg.RateLimitBurst),
	)

	limiter := middleware.NewRateLimiter(cfg.RateLimitPerMin, cfg.RateLimitBurst)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, constants.ResponseHealthy)
	})

	proxyHandler := proxy.NewReverseProxy(targetURL)

	var protectedHandler http.Handler = proxyHandler
	protectedHandler = proxy.Whitelist(protectedHandler)
	protectedHandler = middleware.Auth(cfg.ProxySecretKey)(protectedHandler)
	protectedHandler = middleware.MaxBodySize(constants.MaxBodySize)(protectedHandler)

	mux.Handle("/", protectedHandler)

	// Order (outermost → innermost): Recovery → Logging → RateLimit → Gzip → mux
	var handler http.Handler = mux
	handler = middleware.Gzip(handler)
	handler = middleware.RateLimit(limiter)(handler)
	handler = middleware.Logging(handler)
	handler = middleware.Recovery(handler)

	serverAddr := cfg.ProxyBindAddr + ":" + cfg.ProxyPort
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      handler,
		ReadTimeout:  constants.ServerReadTimeout,
		WriteTimeout: constants.ServerWriteTimeout,
		IdleTimeout:  constants.ServerIdleTimeout,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Proxy server listening on " + serverAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server failed to start", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	sig := <-stopChan
	slog.Info("Received shutdown signal", slog.String("signal", sig.String()))

	ctx, cancel := context.WithTimeout(context.Background(), constants.ShutdownTimeout)
	defer cancel()

	slog.Info("Shutting down proxy server gracefully...")
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown failed", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("Proxy server stopped cleanly.")
}
