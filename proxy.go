package main

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// whitelistedEndpoints maps allowed paths to their allowed HTTP methods
var whitelistedEndpoints = map[string][]string{
	"/rubix/v1/fullnode/sync-token-chain": {"POST"},
}

// WhitelistMiddleware restricts access to only whitelisted path+method combinations.
// Per spec, any request that does not match a whitelisted path AND method returns 403 Forbidden.
func WhitelistMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedMethods, pathExists := whitelistedEndpoints[r.URL.Path]

		allowed := false
		if pathExists {
			for _, m := range allowedMethods {
				if r.Method == m {
					allowed = true
					break
				}
			}
		}

		if !allowed {
			slog.Warn("Access denied: path or method not whitelisted",
				slog.String("path", r.URL.Path),
				slog.String("method", r.Method),
				slog.String("client_ip", getClientIP(r)),
			)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"status":false,"message":"Forbidden"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// NewReverseProxy initializes the httputil.ReverseProxy and customizes headers and errors
func NewReverseProxy(targetURL *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Capture the original director and wrap it
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Rewrite Host header so the upstream sees its own host
		req.Host = targetURL.Host

		// Extract remote IP
		clientIP, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			clientIP = req.RemoteAddr
		}

		// Ensure X-Real-IP is propagated correctly
		if req.Header.Get("X-Real-IP") == "" {
			req.Header.Set("X-Real-IP", clientIP)
		}

		if req.Header.Get("X-Forwarded-For") == "" {
			req.Header.Set("X-Forwarded-For", clientIP)
		}

		// Don't forward auth credentials to the upstream fullnode
		req.Header.Del("X-API-KEY")
	}

	// Strip headers that could leak internal topology from upstream responses
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("Server")
		resp.Header.Del("X-Powered-By")
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("Reverse proxy error forwarding request to fullnode",
			slog.Any("error", err),
			slog.String("path", r.URL.Path),
			slog.String("target", targetURL.String()),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"status":false,"message":"Backend fullnode unavailable"}`))
	}

	return proxy
}
