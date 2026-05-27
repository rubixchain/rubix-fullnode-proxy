package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"rubix-fullnode-proxy/internal/constants"
	"rubix-fullnode-proxy/internal/middleware"
	"rubix-fullnode-proxy/internal/proxy"
	"rubix-fullnode-proxy/internal/response"
)

func TestProxyFlow(t *testing.T) {
	mockFullnodeReceivedReq := false
	var receivedHeaders http.Header
	var receivedBody string

	mockFullnode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mockFullnodeReceivedReq = true
		receivedHeaders = r.Header
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			receivedBody = string(bodyBytes)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success","synced":true}`))
	}))
	defer mockFullnode.Close()

	targetURL, err := url.Parse(mockFullnode.URL)
	if err != nil {
		t.Fatalf("Failed to parse mock fullnode URL: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, constants.ResponseHealthy)
	})

	const secretKey = "test-auth-secret-key"
	proxyHandler := proxy.NewReverseProxy(targetURL)
	var protectedHandler http.Handler = proxyHandler
	protectedHandler = proxy.Whitelist(protectedHandler)
	protectedHandler = middleware.Auth(secretKey)(protectedHandler)

	mux.Handle("/", protectedHandler)

	var mainHandler http.Handler = mux
	mainHandler = middleware.Logging(mainHandler)
	mainHandler = middleware.Recovery(mainHandler)

	proxyServer := httptest.NewServer(mainHandler)
	defer proxyServer.Close()

	client := &http.Client{}

	t.Run("Health Check Success", func(t *testing.T) {
		resp, err := client.Get(proxyServer.URL + "/health")
		if err != nil {
			t.Fatalf("Get /health failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Valid Authorized Request Success", func(t *testing.T) {
		mockFullnodeReceivedReq = false
		payload := `{"token_ids": ["token_xyz_123"]}`
		req, err := http.NewRequest("POST", proxyServer.URL+"/rubix/v1/fullnode/sync-token-chain", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set(constants.HeaderAPIKey, secretKey)
		req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)
		req.Header.Set(constants.HeaderXRealIP, "1.2.3.4")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		if !mockFullnodeReceivedReq {
			t.Error("Mock Fullnode did not receive the request")
		}

		if receivedHeaders.Get(constants.HeaderXRealIP) != "1.2.3.4" {
			t.Errorf("Expected X-Real-IP '1.2.3.4', got '%s'", receivedHeaders.Get(constants.HeaderXRealIP))
		}

		if receivedBody != payload {
			t.Errorf("Expected payload '%s', got '%s'", payload, receivedBody)
		}
	})

	t.Run("Invalid API Key Unauthorized", func(t *testing.T) {
		req, err := http.NewRequest("POST", proxyServer.URL+"/rubix/v1/fullnode/sync-token-chain", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set(constants.HeaderAPIKey, "wrong-secret-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != constants.ResponseUnauthorized {
			t.Errorf("Expected body '%s', got '%s'", constants.ResponseUnauthorized, string(body))
		}
	})

	t.Run("Missing API Key Unauthorized", func(t *testing.T) {
		req, err := http.NewRequest("POST", proxyServer.URL+"/rubix/v1/fullnode/sync-token-chain", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Non-whitelisted Endpoint Forbidden", func(t *testing.T) {
		req, err := http.NewRequest("POST", proxyServer.URL+"/rubix/v1/fullnode/invalid-endpoint", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set(constants.HeaderAPIKey, secretKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != constants.ResponseForbidden {
			t.Errorf("Expected body '%s', got '%s'", constants.ResponseForbidden, string(body))
		}
	})

	t.Run("Wrong Method on Whitelisted Path Forbidden", func(t *testing.T) {
		req, err := http.NewRequest("GET", proxyServer.URL+"/rubix/v1/fullnode/sync-token-chain", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set(constants.HeaderAPIKey, secretKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})

	t.Run("Backend Unavailable Returns 502", func(t *testing.T) {
		deadURL, _ := url.Parse("http://127.0.0.1:19999")
		deadProxy := proxy.NewReverseProxy(deadURL)
		var deadHandler http.Handler = deadProxy
		deadHandler = proxy.Whitelist(deadHandler)
		deadHandler = middleware.Auth(secretKey)(deadHandler)

		deadMux := http.NewServeMux()
		deadMux.Handle("/", deadHandler)

		deadServer := httptest.NewServer(deadMux)
		defer deadServer.Close()

		req, err := http.NewRequest("POST", deadServer.URL+"/rubix/v1/fullnode/sync-token-chain", strings.NewReader(`{"token_ids":["t1"]}`))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set(constants.HeaderAPIKey, secretKey)
		req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != constants.ResponseBadGateway {
			t.Errorf("Expected body '%s', got '%s'", constants.ResponseBadGateway, string(body))
		}
	})
}
