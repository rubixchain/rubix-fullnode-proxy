# Rubix Fullnode Proxy — Secure Go Middleware (Phase 1)

A lightweight, production-ready Go reverse-proxy that sits between the Rubix Explorer and a private Rubix Fullnode. It enforces API-key authentication, per-IP rate limiting, strict path+method whitelisting, payload size limits, GZIP compression, and client-IP forwarding — all using only the Go standard library (zero external dependencies). Nginx handles SSL termination in front of the proxy.

---

## Architecture

```
              [ Explorer / Client ]
                      │
                 HTTPS request
                      │
                      ▼
         [ Nginx ] (0.0.0.0:443)
      • SSL/TLS termination
      • HTTP → HTTPS redirect
      • Hides internal IPs/ports
      • Security headers (HSTS, X-Frame-Options …)
                      │
              http://127.0.0.1:8080
                      │
                      ▼
         [ Go Middleware Proxy ] (127.0.0.1:8080)
      • RecoveryMiddleware  (panic guard)
      • LoggingMiddleware   (method, path, IP, status, latency)
      • RateLimitMiddleware (per-IP token bucket, 60 req/min)
      • GzipMiddleware      (transparent compression)
      • AuthMiddleware      (X-API-KEY ↔ PROXY_SECRET_KEY)
      • MaxBodySize         (1MB payload cap)
      • WhitelistMiddleware (POST /rubix/v1/fullnode/sync-token-chain only)
                      │
              http://127.0.0.1:20000
                      │
                      ▼
         Rubix Fullnode API (127.0.0.1:20000)
```

> **Nothing is exposed externally except Nginx on ports 80/443.** The Go proxy and Fullnode bind to `127.0.0.1` only.

---

## Features

| Feature | Detail |
| :--- | :--- |
| **SSL via Nginx** | Nginx terminates TLS in front of the proxy. Supports self-signed certs now, Let's Encrypt when you have a domain. |
| **Rate Limiting** | Per-IP token-bucket rate limiter (default 60 req/min, burst of 10). Returns `429 Too Many Requests` with `Retry-After` header. |
| **Strict Whitelisting** | Only `POST /rubix/v1/fullnode/sync-token-chain` is forwarded. All other path/method combinations return `403 Forbidden`. |
| **API-Key Auth** | Validates the `X-API-KEY` header against `PROXY_SECRET_KEY` using timing-safe comparison. Invalid or missing key → `401 Unauthorized`. |
| **Compression** | Transparent `GZIP` support for responses when the client sends `Accept-Encoding: gzip`. Drastically reduces sync time for large token chains. |
| **Security Hardening** | 1MB request body limit, localhost-only binding, API key stripped before forwarding, internal headers scrubbed from responses. |
| **IP Forwarding** | Correctly propagates `X-Real-IP` and `X-Forwarded-For` so the Fullnode sees the original client IP. |
| **Structured Logging** | JSON logs via `log/slog` — method, path, status code, latency, client IP on every request. |
| **Backend Error Handling** | If the Fullnode is down, returns `502` with `{"status": false, "message": "Backend fullnode unavailable"}`. |
| **Graceful Shutdown** | Handles `SIGINT` / `SIGTERM` and drains in-flight connections with a 10 s deadline. |
| **Zero Dependencies** | Pure Go standard library (`net/http`, `httputil`, `crypto/subtle`, `log/slog`). |

---

## Configuration

Copy the example env file and customise it:

```bash
cp .env.example .env
```

| Variable | Default | Description |
| :--- | :--- | :--- |
| `FULLNODE_URL` | `http://localhost:2000` | Upstream Rubix Fullnode address. |
| `PROXY_PORT` | `8080` | Port the proxy listens on. |
| `PROXY_BIND_ADDR` | `127.0.0.1` | Bind address. `127.0.0.1` = localhost only (production). `0.0.0.0` = all interfaces (dev). |
| `PROXY_SECRET_KEY` | *(required)* | Shared secret clients must supply in the `X-API-KEY` header. |
| `RATE_LIMIT_PER_MIN` | `60` | Max requests per minute per client IP. |
| `RATE_LIMIT_BURST` | `10` | Burst allowance — extra requests allowed in a short window before throttling kicks in. |

---

## Build & Run

### Natively (Standalone)

```bash
# 1. Build
go build -o rubix-proxy .

# 2. Run (env vars can also come from .env)
# On Linux/macOS:
./rubix-proxy
# On Windows:
.\rubix-proxy.exe
```

### Systemd Service (Linux)

Create `/etc/systemd/system/rubix-proxy.service`:

```ini
[Unit]
Description=Rubix Fullnode Proxy Service
After=network.target

[Service]
Type=simple
User=nobody
WorkingDirectory=/opt/rubix-fullnode-proxy
EnvironmentFile=/opt/rubix-fullnode-proxy/.env
ExecStart=/opt/rubix-fullnode-proxy/rubix-proxy
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now rubix-proxy
```

---

## Nginx Setup (SSL Termination)

### 1. Install Nginx

```bash
sudo apt update && sudo apt install nginx -y
```

### 2. Generate a Self-Signed Certificate (no domain required)

```bash
sudo mkdir -p /etc/nginx/ssl
sudo openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/nginx/ssl/rubix-proxy.key \
  -out /etc/nginx/ssl/rubix-proxy.crt \
  -subj "/CN=rubix-proxy"
```

### 3. Deploy the Config

```bash
sudo cp nginx.conf /etc/nginx/sites-available/rubix-proxy
sudo ln -s /etc/nginx/sites-available/rubix-proxy /etc/nginx/sites-enabled/
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t && sudo systemctl reload nginx
```

### 4. Switch to Let's Encrypt (when you have a domain)

```bash
# Edit nginx.conf: change server_name _ to your domain
sudo apt install certbot python3-certbot-nginx -y
sudo certbot --nginx -d your-domain.com
```

Certbot will auto-update the certificate paths and configure auto-renewal.

---

## Running Tests

```bash
go test -v ./...
```

---

## Verify with curl

Assuming `PROXY_SECRET_KEY=test-secret-key` and the proxy is on port `8080`:

### 1. Health Check (no auth required)

```bash
curl -i http://localhost:8080/health
```

### 2. Valid Sync Request (with compression)

```bash
curl -i -X POST http://localhost:8080/rubix/v1/fullnode/sync-token-chain \
  -H "X-API-KEY: test-secret-key" \
  -H "Content-Type: application/json" \
  -H "Accept-Encoding: gzip" \
  -d '{"token_ids": ["token_xyz_123"]}'
```

### 3. Payload Too Large (>1MB)

```bash
# This will be rejected by MaxBodySizeMiddleware
curl -i -X POST http://localhost:8080/rubix/v1/fullnode/sync-token-chain \
  -H "X-API-KEY: test-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"large_payload": "..."}' 
```
