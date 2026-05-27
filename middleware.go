package main

import (
	"compress/gzip"
	"crypto/subtle"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// responseWriterWrapper wraps http.ResponseWriter to capture the HTTP status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// ─── Gzip Compression Middleware ────────────────────────────────────────────

// gzipResponseWriter wraps http.ResponseWriter and compresses writes through a gzip.Writer.
type gzipResponseWriter struct {
	http.ResponseWriter
	gzWriter   *gzip.Writer
	statusCode int
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	g.statusCode = code
	// Remove Content-Length because the compressed size will differ
	g.ResponseWriter.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if g.statusCode == 0 {
		g.statusCode = http.StatusOK
	}
	return g.gzWriter.Write(b)
}

// gzipWriterPool recycles gzip.Writer instances to reduce GC pressure
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// GzipMiddleware compresses responses when the client sends `Accept-Encoding: gzip`.
// It sits in the middleware chain so that ALL responses flowing back to the Explorer
// (including proxied fullnode responses) are transparently compressed.
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only compress if the client explicitly accepts gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Borrow a gzip writer from the pool, reset it to target the real ResponseWriter
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			gz.Close()
			gzipWriterPool.Put(gz)
		}()

		// Set response headers BEFORE the handler writes anything
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		// Delete Content-Length upfront — the handler may have set one
		w.Header().Del("Content-Length")

		gzw := &gzipResponseWriter{
			ResponseWriter: w,
			gzWriter:       gz,
		}

		next.ServeHTTP(gzw, r)
	})
}

// ─── Logging Middleware ─────────────────────────────────────────────────────

// LoggingMiddleware logs structured details of each request (Method, Path, Status Code, Latency, Client IP)
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapper := &responseWriterWrapper{ResponseWriter: w}

		next.ServeHTTP(wrapper, r)

		latency := time.Since(start)
		clientIP := getClientIP(r)

		slog.Info("Request handled",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status_code", wrapper.statusCode),
			slog.Duration("latency", latency),
			slog.String("client_ip", clientIP),
		)
	})
}

// ─── Auth Middleware ────────────────────────────────────────────────────────

// AuthMiddleware validates that the custom X-API-KEY header matches the configured key
func AuthMiddleware(secretKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-KEY")
			if apiKey == "" || subtle.ConstantTimeCompare([]byte(apiKey), []byte(secretKey)) != 1 {
				clientIP := getClientIP(r)
				slog.Warn("Unauthorized access attempt rejected",
					slog.String("path", r.URL.Path),
					slog.String("client_ip", clientIP),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"status":false,"message":"Unauthorized: Invalid or missing X-API-KEY"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ─── Recovery Middleware ────────────────────────────────────────────────────

// RecoveryMiddleware captures panics, logs them, and returns a 500 status
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("Recovered from panic",
					slog.Any("error", err),
					slog.String("path", r.URL.Path),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"status":false,"message":"Internal Server Error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ─── Max Body Size Middleware ───────────────────────────────────────────────

// MaxBodySizeMiddleware limits the size of the request body to prevent DOS attacks.
func MaxBodySizeMiddleware(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			next.ServeHTTP(w, r)
		})
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// getClientIP extracts the original client IP from headers or RemoteAddr
func getClientIP(r *http.Request) string {
	// 1. Check X-Real-IP (populated by Nginx edge)
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}

	// 2. Check X-Forwarded-For (can be a comma-separated list; the first client IP is the original one)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// 3. Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
