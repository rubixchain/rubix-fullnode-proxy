# Rubix Fullnode Proxy

A Go reverse-proxy that protects a Rubix Fullnode from direct external access. The Explorer never talks to the Fullnode directly — it talks to this proxy over HTTPS, and the proxy forwards only approved requests to the Fullnode on localhost.

## Why this exists

The Fullnode exposes many internal APIs. We don't want any of them reachable from the internet. This proxy:

- **Hides the Fullnode** — it binds to `127.0.0.1`, invisible from outside the VM
- **Allows only one endpoint** — `POST /rubix/v1/fullnode/sync-token-chain`, everything else is blocked
- **Authenticates requests** — Explorer must send a secret API key
- **Prevents abuse** — rate limiting (60 req/min per IP) and 1MB body size cap
- **Handles SSL** — Nginx terminates HTTPS in front of the proxy

Zero external dependencies. Built entirely with the Go standard library.

---

## How it works

```
  Explorer (remote VM)
        |
        | HTTPS (port 443)
        v
  +-----------+
  |   Nginx   |  (only public-facing thing on the VM)
  +-----------+
        |
        | http://127.0.0.1:8080
        v
  +-----------+
  | Go Proxy  |  auth, rate limit, whitelist, gzip, logging
  +-----------+
        |
        | http://127.0.0.1:20000
        v
  +-----------+
  | Fullnode  |  (never exposed to the internet)
  +-----------+
```

All three run on the **same VM**. Only Nginx listens on a public port. The Go Proxy and Fullnode are bound to `127.0.0.1` — unreachable from outside.

### Request lifecycle

1. Explorer sends `POST https://<vm-ip>/rubix/v1/fullnode/sync-token-chain` with `X-API-KEY` header
2. **Nginx** terminates SSL, forwards to Go Proxy on localhost:8080
3. **Recovery middleware** — catches panics so the server never crashes
4. **Logging middleware** — records method, path, status, latency, client IP (JSON format)
5. **Rate limiter** — if this IP exceeded 60 req/min, reject with `429`
6. **Gzip middleware** — if client accepts gzip, compress the response
7. **Body size check** — if request body > 1MB, reject
8. **Auth middleware** — if `X-API-KEY` is missing or wrong, reject with `401`
9. **Whitelist** — if path/method isn't `POST /rubix/v1/fullnode/sync-token-chain`, reject with `403`
10. **Reverse proxy** — strips the API key, forwards to Fullnode on localhost:20000
11. Response flows back through the same chain to the Explorer

If the Fullnode is down, the proxy returns `502 Backend fullnode unavailable`.

---

## Project structure

```
rubix-fullnode-proxy/
├── cmd/proxy/main.go              # Entrypoint — wires everything together
├── internal/
│   ├── constants/constants.go     # All config defaults, headers, timeouts, responses
│   ├── config/
│   │   ├── config.go              # Config struct, Load(), validation
│   │   └── env.go                 # .env file parser
│   ├── middleware/
│   │   ├── auth.go                # API key validation (timing-safe)
│   │   ├── bodysize.go            # Request body size limit
│   │   ├── gzip.go                # Response compression
│   │   ├── logging.go             # Structured JSON request logging
│   │   ├── ratelimit.go           # Per-IP token bucket rate limiter
│   │   └── recovery.go            # Panic recovery
│   ├── proxy/
│   │   ├── handler.go             # Reverse proxy to Fullnode
│   │   └── whitelist.go           # Path + method whitelist
│   ├── response/json.go           # Shared JSON response helper
│   └── util/ip.go                 # Client IP extraction
├── tests/proxy_test.go            # Integration tests (7 test cases)
├── deployment/nginx.conf          # Production Nginx config
├── .env.example                   # Environment variable template
├── go.mod
└── README.md
```

---

## Setup

### Prerequisites

- Go 1.22+
- Nginx (on the deployment VM)
- A Rubix Fullnode running on the same VM

### 1. Configure

```bash
cp .env.example .env
```

Edit `.env`:

```env
FULLNODE_URL=http://localhost:20000    # Fullnode address (same VM, localhost)
PROXY_PORT=8080                       # Proxy listening port
PROXY_BIND_ADDR=127.0.0.1            # 127.0.0.1 for production, 0.0.0.0 for local dev
PROXY_SECRET_KEY=your-strong-secret   # Shared with the Explorer (REQUIRED)
RATE_LIMIT_PER_MIN=60                 # Max requests per minute per client IP
RATE_LIMIT_BURST=10                   # Burst allowance before throttling
```

### 2. Build

```bash
go build -o rubix-proxy ./cmd/proxy
```

### 3. Run

```bash
# Linux/macOS
./rubix-proxy

# Windows
.\rubix-proxy.exe
```

### 4. Run as a systemd service (Linux)

Create `/etc/systemd/system/rubix-proxy.service`:

```ini
[Unit]
Description=Rubix Fullnode Proxy
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

## Nginx setup (SSL)

Nginx sits in front of the Go proxy to handle HTTPS. The Go proxy and Fullnode stay on localhost.

### 1. Install

```bash
sudo apt update && sudo apt install nginx -y
```

### 2. Generate a self-signed certificate

Use this if you don't have a domain yet. Clients will see a browser warning, but encryption works.

```bash
sudo mkdir -p /etc/nginx/ssl
sudo openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/nginx/ssl/rubix-proxy.key \
  -out /etc/nginx/ssl/rubix-proxy.crt \
  -subj "/CN=rubix-proxy"
```

### 3. Deploy the config

```bash
sudo cp deployment/nginx.conf /etc/nginx/sites-available/rubix-proxy
sudo ln -s /etc/nginx/sites-available/rubix-proxy /etc/nginx/sites-enabled/
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t && sudo systemctl reload nginx
```

### 4. Switch to Let's Encrypt (when you have a domain)

```bash
# First, edit deployment/nginx.conf: change "server_name _" to your domain
sudo apt install certbot python3-certbot-nginx -y
sudo certbot --nginx -d your-domain.com
```

Certbot auto-renews certificates.

---

## Explorer configuration

The Explorer needs two environment variables to connect to this proxy:

```env
TOKEN_SYNC_PROXY_URL=https://<vm-public-ip>
TOKEN_SYNC_API_KEY=your-strong-secret    # same value as PROXY_SECRET_KEY
```

The Explorer will send requests like:

```
POST https://<vm-public-ip>/rubix/v1/fullnode/sync-token-chain
Headers:
  X-API-KEY: your-strong-secret
  Content-Type: application/json
  Accept-Encoding: gzip
Body:
  {"token_ids": ["token_abc_123", "token_def_456"]}
```

---

## API responses

| Scenario | HTTP Status | Response body |
| :--- | :--- | :--- |
| Healthy proxy | `200` | `{"status":"healthy"}` |
| Successful sync | `200` | Fullnode's JSON response (may be gzip compressed) |
| Missing or wrong API key | `401` | `{"status":false,"message":"Unauthorized: Invalid or missing X-API-KEY"}` |
| Non-whitelisted path or method | `403` | `{"status":false,"message":"Forbidden"}` |
| Rate limit exceeded | `429` | `{"status":false,"message":"Too Many Requests"}` |
| Fullnode is down | `502` | `{"status":false,"message":"Backend fullnode unavailable"}` |

---

## Testing

```bash
# Run all tests
go test -v ./...

# Quick curl checks (for local dev with PROXY_BIND_ADDR=0.0.0.0)

# Health check (no auth needed)
curl -i http://localhost:8080/health

# Valid sync request
curl -i -X POST http://localhost:8080/rubix/v1/fullnode/sync-token-chain \
  -H "X-API-KEY: your-strong-secret" \
  -H "Content-Type: application/json" \
  -d '{"token_ids": ["token_xyz_123"]}'

# Should return 403 (wrong endpoint)
curl -i -X POST http://localhost:8080/some/other/path \
  -H "X-API-KEY: your-strong-secret"

# Should return 401 (no API key)
curl -i -X POST http://localhost:8080/rubix/v1/fullnode/sync-token-chain
```

---

## Security summary

| What | How |
| :--- | :--- |
| Fullnode not exposed | Bound to `127.0.0.1:20000`, unreachable from outside |
| Proxy not exposed | Bound to `127.0.0.1:8080`, only Nginx forwards to it |
| Only HTTPS externally | Nginx redirects HTTP to HTTPS |
| Only one endpoint allowed | Whitelist blocks everything except the sync endpoint |
| API key required | Timing-safe comparison prevents brute-force timing attacks |
| Abuse prevention | 60 req/min per IP, 1MB body limit |
| No internal info leaked | `Server`, `X-Powered-By` headers stripped; API key not forwarded to Fullnode |
