# Windows Deployment Guide

Production-like setup on a Windows desktop: HTTPS via nginx → Go proxy → Rubix fullnode.

```
External client (HTTPS, port 443)
        |
        v
nginx for Windows           [TLS termination, security headers, IP hiding]
        |
        | http://127.0.0.1:8080
        v
rubix-fullnode-proxy.exe    [auth, rate limit, whitelist, gzip]
        |
        | http://127.0.0.1:<fullnode-port>
        v
Rubix fullnode
```

All three run on the **same Windows machine**. Only nginx is reachable from outside.

---

## 0. Pre-flight checks

Open **PowerShell** and run:

```powershell
# Check existing nginx installations
Test-Path C:\nginx\nginx.exe
Test-Path "C:\Program Files\nginx\nginx.exe"
Test-Path C:\tools\nginx\nginx.exe

# Check OpenSSL (likely bundled with Git for Windows)
openssl version

# Check Go (need 1.22+)
go version

# CRITICAL: confirm the fullnode is actually listening on its expected port
# Replace 20001 with your fullnode's actual port
Get-NetTCPConnection -LocalPort 20001 -State Listen -ErrorAction SilentlyContinue

# Confirm ports 80, 443, 8080 are free
Get-NetTCPConnection -LocalPort 80,443,8080 -State Listen -ErrorAction SilentlyContinue
```

**STOP and resolve before continuing if:**
- The fullnode is NOT listening on its expected port (start it first)
- Port 443 is in use by IIS, Skype, or Hyper-V — stop the service (e.g. `Stop-Service W3SVC` for IIS)
- Port 80 or 8080 is occupied

---

## 1. Install nginx for Windows

Skip if `Test-Path C:\nginx\nginx.exe` returned `True`.

```powershell
$nginxVersion = "1.26.2"  # check https://nginx.org/en/download.html for latest stable
Invoke-WebRequest -Uri "https://nginx.org/download/nginx-$nginxVersion.zip" -OutFile "$env:TEMP\nginx.zip"
Expand-Archive -Path "$env:TEMP\nginx.zip" -DestinationPath "C:\" -Force
Rename-Item "C:\nginx-$nginxVersion" "C:\nginx"
Remove-Item "$env:TEMP\nginx.zip"

# Verify
C:\nginx\nginx.exe -v
```

Expected output: `nginx version: nginx/1.26.2` (or similar).

---

## 2. Install OpenSSL (if missing)

If `openssl version` failed in step 0:

- **Option A (recommended):** Install [Git for Windows](https://git-scm.com/download/win) — bundles OpenSSL. Restart PowerShell after install.
- **Option B:** Standalone [Win64 OpenSSL](https://slproweb.com/products/Win32OpenSSL.html) Light.

---

## 3. Generate self-signed certificate

```powershell
cd C:\nginx
New-Item -ItemType Directory -Force ssl | Out-Null

& openssl req -x509 -nodes -days 365 -newkey rsa:2048 `
  -keyout ssl\rubix-proxy.key `
  -out ssl\rubix-proxy.crt `
  -subj "/CN=rubix-proxy"

# Verify
Get-ChildItem C:\nginx\ssl
```

Expected: `rubix-proxy.crt` and `rubix-proxy.key`, both > 0 bytes.

---

## 4. Configure nginx

Copy the Windows-adapted config into nginx's conf directory:

```powershell
Copy-Item "<repo-path>\deployment\nginx-windows.conf" C:\nginx\conf\rubix-proxy.conf
```

Then edit `C:\nginx\conf\nginx.conf` and do **two things** inside the `http { }` block:

1. **Add this line** near the end of `http { }`:
   ```nginx
   include rubix-proxy.conf;
   ```
2. **Comment out** the default `server { listen 80; ... }` block (the one with `server_name localhost`).

Validate:

```powershell
C:\nginx\nginx.exe -t -p C:\nginx\
```

Must print `syntax is ok` and `test is successful` before continuing.

---

## 5. Generate the API secret key

```powershell
$secret = [Convert]::ToBase64String((1..24 | ForEach-Object {Get-Random -Maximum 256}))
Write-Host "PROXY_SECRET_KEY: $secret"
```

**Copy this value somewhere safe** — the Explorer team needs the exact same string.

---

## 6. Build the Go proxy

```powershell
cd "<repo-path>"
go build -o rubix-fullnode-proxy.exe ./cmd/proxy
Get-Item .\rubix-fullnode-proxy.exe
```

---

## 7. Create the `.env` file

```powershell
@"
FULLNODE_URL=http://localhost:20001
PROXY_PORT=8080
PROXY_BIND_ADDR=127.0.0.1
PROXY_SECRET_KEY=$secret
RATE_LIMIT_PER_MIN=60
RATE_LIMIT_BURST=10
"@ | Out-File -FilePath .env -Encoding utf8 -NoNewline
```

> Adjust `FULLNODE_URL` if your fullnode runs on a different port.

> `PROXY_BIND_ADDR=127.0.0.1` is **non-negotiable** for production — it ensures only nginx (running on the same VM) can reach the proxy.

---

## 8. Start the Go proxy

```powershell
cd "<repo-path>"
Start-Process -FilePath ".\rubix-fullnode-proxy.exe" `
  -WorkingDirectory (Get-Location) -WindowStyle Hidden `
  -RedirectStandardOutput proxy.log -RedirectStandardError proxy.err.log

Start-Sleep -Seconds 2
Get-NetTCPConnection -LocalPort 8080 -State Listen
```

Expected: `LocalAddress` is `127.0.0.1`. If you see `0.0.0.0`, the `.env` wasn't loaded — check `proxy.err.log`.

---

## 9. Start nginx

```powershell
Start-Process -FilePath "C:\nginx\nginx.exe" -WorkingDirectory C:\nginx -WindowStyle Hidden

Start-Sleep -Seconds 2
Get-NetTCPConnection -LocalPort 80,443 -State Listen
```

If nginx fails, check its log:

```powershell
Get-Content C:\nginx\logs\error.log -Tail 20
```

---

## 10. End-to-end verification

### a. HTTP → HTTPS redirect

```powershell
$r = Invoke-WebRequest -Uri "http://localhost/health" -MaximumRedirection 0 -SkipHttpErrorCheck
$r.StatusCode        # Expect: 301
$r.Headers.Location  # Expect: https://localhost/health
```

### b. Health endpoint (no auth)

```powershell
Invoke-RestMethod -Uri "https://localhost/health" -SkipCertificateCheck
# Expect: status = healthy
```

### c. Auth gate (no key → 401)

```powershell
try {
    Invoke-RestMethod -Uri "https://localhost/rubix/v1/fullnode/sync-token-chain" `
        -Method POST -ContentType "application/json" `
        -Body '{"token_ids":["50001_111131"]}' -SkipCertificateCheck
} catch { $_.Exception.Response.StatusCode }  # Expect: Unauthorized
```

### d. Whitelist gate (wrong path → 403)

```powershell
$headers = @{ "X-API-KEY" = $secret }
try {
    Invoke-RestMethod -Uri "https://localhost/some/other/path" -Method POST `
        -Headers $headers -SkipCertificateCheck
} catch { $_.Exception.Response.StatusCode }  # Expect: Forbidden
```

### e. Real sync call

```powershell
$headers = @{ "X-API-KEY" = $secret }
$body = '{"token_ids":["50001_111131"]}'
$resp = Invoke-RestMethod -Uri "https://localhost/rubix/v1/fullnode/sync-token-chain" `
    -Method POST -Headers $headers -ContentType "application/json" `
    -Body $body -SkipCertificateCheck

$resp.status                                          # Expect: True
$resp.result."50001_111131".Count                     # Expect: > 0
$resp.result."50001_111131"[0].previous_transaction_id # Expect: "" (empty)
```

### f. Header hygiene

```powershell
$r = Invoke-WebRequest -Uri "https://localhost/health" -SkipCertificateCheck
$r.Headers["Server"]        # Expect: empty or just "nginx" (NOT "nginx/1.26.2")
$r.Headers["X-Powered-By"]  # Expect: nothing
```

### g. Rate limit smoke test

```powershell
1..12 | ForEach-Object {
    try {
        $r = Invoke-WebRequest -Uri "https://localhost/health" `
            -SkipCertificateCheck -SkipHttpErrorCheck
        "Request $_`: $($r.StatusCode)"
    } catch {
        "Request $_`: $($_.Exception.Response.StatusCode)"
    }
}
# Expect: first ~10 return 200, then 429s
```

---

## 11. Stop / restart commands

```powershell
# Stop nginx (graceful)
C:\nginx\nginx.exe -s stop -p C:\nginx\

# Reload nginx config without restart
C:\nginx\nginx.exe -s reload -p C:\nginx\

# Stop the Go proxy
Stop-Process -Name rubix-fullnode-proxy

# Restart everything (run from repo root)
Start-Process -FilePath ".\rubix-fullnode-proxy.exe" -WorkingDirectory (Get-Location) -WindowStyle Hidden
Start-Process -FilePath "C:\nginx\nginx.exe" -WorkingDirectory C:\nginx -WindowStyle Hidden
```

---

## 12. Hand off to the Explorer team

Give them these two values:

```
TOKEN_SYNC_PROXY_URL=https://<this-machine's-public-IP>
TOKEN_SYNC_API_KEY=<the-PROXY_SECRET_KEY-you-generated>
```

For self-signed certs the Explorer's HTTP client needs to skip TLS verification, or install `C:\nginx\ssl\rubix-proxy.crt` into the Explorer machine's trust store. When you switch to a real domain + Let's Encrypt (via [win-acme](https://www.win-acme.com/)) this isn't needed.

---

## 13. Known footguns

| Issue | Symptom | Fix |
|:--|:--|:--|
| No persistence across reboots | Both processes die on logout/reboot | Wrap with [NSSM](https://nssm.cc/) to run as Windows Services |
| Port 443 conflict | `nginx: bind() to 0.0.0.0:443 failed (10048)` | IIS / Skype / Hyper-V is using it. `Stop-Service W3SVC` for IIS |
| `openssl` not found | Command unrecognised after Git install | Restart PowerShell; check `$env:Path` includes `C:\Program Files\Git\usr\bin` |
| Self-signed cert warning | Browser shows "Not Secure" | Expected. Use `-k` / `-SkipCertificateCheck` for clients |
| `.env` not loaded | Proxy binds `0.0.0.0` instead of `127.0.0.1` | `.env` is in wrong directory or `Start-Process` missed `-WorkingDirectory` |
| Fullnode port wrong | All sync requests → `502 Backend fullnode unavailable` | Edit `FULLNODE_URL` in `.env`, restart proxy |
| Default nginx page shows | Health check returns nginx welcome HTML | The default `server { listen 80; }` block in `nginx.conf` wasn't commented out, OR `include rubix-proxy.conf;` wasn't added |
