package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyFlow(t *testing.T) {
	// 1. Start a mock Rubix Fullnode upstream server
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

	// 2. Parse mock fullnode URL
	targetURL, err := url.Parse(mockFullnode.URL)
	if err != nil {
		t.Fatalf("Failed to parse mock fullnode URL: %v", err)
	}

	// 3. Set up the Proxy Mux/Handler
	mux := http.NewServeMux()

	// Public health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	const secretKey = "test-auth-secret-key"
	proxyHandler := NewReverseProxy(targetURL)
	var protectedHandler http.Handler = proxyHandler
	protectedHandler = WhitelistMiddleware(protectedHandler)
	protectedHandler = AuthMiddleware(secretKey)(protectedHandler)

	mux.Handle("/", protectedHandler)

	// Apply global logging and recovery
	var mainHandler http.Handler = mux
	mainHandler = LoggingMiddleware(mainHandler)
	mainHandler = RecoveryMiddleware(mainHandler)

	// Start proxy server locally using httptest
	proxyServer := httptest.NewServer(mainHandler)
	defer proxyServer.Close()

	client := &http.Client{}

	// Test Case 1: Public Health Check (No Auth needed)
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

	// Test Case 2: Whitelisted Path with Valid Auth Key & POST method (Success case)
	t.Run("Valid Authorized Request Success", func(t *testing.T) {
		mockFullnodeReceivedReq = false
		payload := `{"token_ids": ["token_xyz_123"]}`
		req, err := http.NewRequest("POST", proxyServer.URL+"/rubix/v1/fullnode/sync-token-chain", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("X-API-KEY", secretKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Real-IP", "1.2.3.4")

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

		// Verify headers passed to upstream
		if receivedHeaders.Get("X-Real-IP") != "1.2.3.4" {
			t.Errorf("Expected X-Real-IP '1.2.3.4', got '%s'", receivedHeaders.Get("X-Real-IP"))
		}

		// Verify payload was received unchanged
		if receivedBody != payload {
			t.Errorf("Expected payload '%s', got '%s'", payload, receivedBody)
		}
	})

	// Test Case 3: Invalid Auth Key → 401 Unauthorized
	t.Run("Invalid API Key Unauthorized", func(t *testing.T) {
		req, err := http.NewRequest("POST", proxyServer.URL+"/rubix/v1/fullnode/sync-token-chain", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("X-API-KEY", "wrong-secret-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		expected := `{"status":false,"message":"Unauthorized: Invalid or missing X-API-KEY"}`
		if string(body) != expected {
			t.Errorf("Expected body '%s', got '%s'", expected, string(body))
		}
	})

	// Test Case 4: Missing API Key → 401 Unauthorized
	t.Run("Missing API Key Unauthorized", func(t *testing.T) {
		req, err := http.NewRequest("POST", proxyServer.URL+"/rubix/v1/fullnode/sync-token-chain", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		// No X-API-KEY header set

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	// Test Case 5: Non-whitelisted Endpoint → 403 Forbidden
	t.Run("Non-whitelisted Endpoint Forbidden", func(t *testing.T) {
		req, err := http.NewRequest("POST", proxyServer.URL+"/rubix/v1/fullnode/invalid-endpoint", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("X-API-KEY", secretKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		expected := `{"status":false,"message":"Forbidden"}`
		if string(body) != expected {
			t.Errorf("Expected body '%s', got '%s'", expected, string(body))
		}
	})

	// Test Case 6: Wrong HTTP method on whitelisted path → 403 Forbidden (spec: all non-matching = 403)
	t.Run("Wrong Method on Whitelisted Path Forbidden", func(t *testing.T) {
		req, err := http.NewRequest("GET", proxyServer.URL+"/rubix/v1/fullnode/sync-token-chain", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("X-API-KEY", secretKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})

	// Test Case 7: Backend down → 502 with spec-mandated JSON
	t.Run("Backend Unavailable Returns 502", func(t *testing.T) {
		// Create a proxy pointing to a dead backend
		deadURL, _ := url.Parse("http://127.0.0.1:19999") // nothing listening here
		deadProxy := NewReverseProxy(deadURL)
		var deadHandler http.Handler = deadProxy
		deadHandler = WhitelistMiddleware(deadHandler)
		deadHandler = AuthMiddleware(secretKey)(deadHandler)

		deadMux := http.NewServeMux()
		deadMux.Handle("/", deadHandler)

		deadServer := httptest.NewServer(deadMux)
		defer deadServer.Close()

		req, err := http.NewRequest("POST", deadServer.URL+"/rubix/v1/fullnode/sync-token-chain", strings.NewReader(`{"token_ids":["t1"]}`))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("X-API-KEY", secretKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		expected := `{"status":false,"message":"Backend fullnode unavailable"}`
		if string(body) != expected {
			t.Errorf("Expected body '%s', got '%s'", expected, string(body))
		}
	})
}
