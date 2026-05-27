package middleware

import (
	"log/slog"
	"net/http"

	"rubix-fullnode-proxy/internal/constants"
	"rubix-fullnode-proxy/internal/response"
)

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("Recovered from panic",
					slog.Any("error", err),
					slog.String("path", r.URL.Path),
				)
				response.JSON(w, http.StatusInternalServerError, constants.ResponseInternalError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
