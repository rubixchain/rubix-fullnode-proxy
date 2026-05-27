package proxy

import (
	"log/slog"
	"net/http"

	"rubix-fullnode-proxy/internal/constants"
	"rubix-fullnode-proxy/internal/response"
	"rubix-fullnode-proxy/internal/util"
)

func Whitelist(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedMethods, pathExists := constants.WhitelistedEndpoints[r.URL.Path]

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
				slog.String("client_ip", util.GetClientIP(r)),
			)
			response.JSON(w, http.StatusForbidden, constants.ResponseForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
