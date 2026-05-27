package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"rubix-fullnode-proxy/internal/util"
)

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

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapper := &responseWriterWrapper{ResponseWriter: w}

		next.ServeHTTP(wrapper, r)

		slog.Info("Request handled",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status_code", wrapper.statusCode),
			slog.Duration("latency", time.Since(start)),
			slog.String("client_ip", util.GetClientIP(r)),
		)
	})
}
