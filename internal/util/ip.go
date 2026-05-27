package util

import (
	"net"
	"net/http"
	"strings"

	"rubix-fullnode-proxy/internal/constants"
)

func GetClientIP(r *http.Request) string {
	if ip := r.Header.Get(constants.HeaderXRealIP); ip != "" {
		return strings.TrimSpace(ip)
	}

	if xff := r.Header.Get(constants.HeaderXForwardedFor); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
