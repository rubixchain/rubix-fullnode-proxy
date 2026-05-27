package proxy

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"rubix-fullnode-proxy/internal/constants"
	"rubix-fullnode-proxy/internal/response"
)

func NewReverseProxy(targetURL *url.URL) *httputil.ReverseProxy {
	p := httputil.NewSingleHostReverseProxy(targetURL)

	originalDirector := p.Director
	p.Director = func(req *http.Request) {
		originalDirector(req)

		req.Host = targetURL.Host

		clientIP, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			clientIP = req.RemoteAddr
		}

		if req.Header.Get(constants.HeaderXRealIP) == "" {
			req.Header.Set(constants.HeaderXRealIP, clientIP)
		}

		if req.Header.Get(constants.HeaderXForwardedFor) == "" {
			req.Header.Set(constants.HeaderXForwardedFor, clientIP)
		}

		req.Header.Del(constants.HeaderAPIKey)
	}

	p.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del(constants.HeaderServer)
		resp.Header.Del(constants.HeaderXPoweredBy)
		return nil
	}

	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("Reverse proxy error forwarding request to fullnode",
			slog.Any("error", err),
			slog.String("path", r.URL.Path),
			slog.String("target", targetURL.String()),
		)
		response.JSON(w, http.StatusBadGateway, constants.ResponseBadGateway)
	}

	return p
}
