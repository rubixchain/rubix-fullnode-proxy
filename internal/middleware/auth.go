package middleware

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"rubix-fullnode-proxy/internal/constants"
	"rubix-fullnode-proxy/internal/response"
	"rubix-fullnode-proxy/internal/util"
)

func Auth(secretKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get(constants.HeaderAPIKey)
			if apiKey == "" || subtle.ConstantTimeCompare([]byte(apiKey), []byte(secretKey)) != 1 {
				slog.Warn("Unauthorized access attempt rejected",
					slog.String("path", r.URL.Path),
					slog.String("client_ip", util.GetClientIP(r)),
				)
				response.JSON(w, http.StatusUnauthorized, constants.ResponseUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
