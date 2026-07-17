# Kiro CLI Pool Proxy

Plain reverse-proxy xoay account cho **kiro-cli gốc**. Không cần MITM, không cần
cert, không cần fork. Dùng chính cơ chế endpoint override có sẵn trong kiro-cli.

## Nguyên lý (đã verify từ binary kiro-cli 2.12.2)

kiro-cli có sẵn 3 setting để override endpoint:

| Setting | Trỏ tới | Dùng cho |
|---------|---------|----------|
| `api.krs.service` | `runtime.{region}.kiro.dev` | **Chat** (GenerateAssistantResponse) |
| `api.cps.service` | `management.{region}.kiro.dev` | Profiles, usage limits |
| `api.codewhisperer.service` | legacy `q.amazonaws.com` | Telemetry |

Set 3 setting này trỏ vào proxy → kiro-cli gửi thẳng request (plain HTTP) cho proxy:

```
kiro-cli ──HTTP──→ Pool Proxy ──HTTPS──→ runtime.{region}.kiro.dev
  (native)          │
                    ├── Swap Authorization + profileArn + tokentype
                    ├── Xoay account (round-robin / smart quota-aware)
                    ├── Tee response → đếm credit (meteringEvent)
                    └── Auto refresh token + GetUsageLimits quota poll
```

**kiro-cli chạy 100% native** — system prompt, tools, thinking, agentic, context
management đều do CLI tự xử lý. Proxy chỉ xoay account + đếm token.

> Đã test thực tế: chat qua proxy trả lời đúng, credit đếm khớp CLI (0.16 = 0.1619).

## Quick Start

### 1. Build

```bash
go build -o kiro-pool-proxy .
```

### 2. Tạo config với accounts

```bash
# Import account từ kiro-cli đã login (đọc SQLite, pure-Go)
go run ./cmd/import-local

# Hoặc chạy lần đầu để tạo template
./kiro-pool-proxy --config config.json
```

Điền accounts vào `config.json` (xem mẫu bên dưới).

### 3. Chạy proxy

```bash
./kiro-pool-proxy --config config.json
```

### 4. Trỏ kiro-cli vào proxy

```bash
./set-endpoints.sh http://127.0.0.1:9999 us-east-1
# hoặc thủ công:
kiro-cli settings api.krs.service '{"endpoint":"http://127.0.0.1:9999","region":"us-east-1"}'
kiro-cli settings api.cps.service '{"endpoint":"http://127.0.0.1:9999","region":"us-east-1"}'
```

### 5. Dùng kiro-cli như bình thường

```bash
kiro-cli chat
# Mọi request đi qua proxy: xoay account + đếm credit
```

Khôi phục về mặc định: `./set-endpoints.sh --reset`

## Chạy proxy trên máy khác (remote)

Vì đây là plain HTTP proxy (không MITM), chạy remote rất đơn giản:

```bash
# Trên SERVER:
#   config.json: "listenAddr": "0.0.0.0:9999", "poolKey": "secret" (tùy chọn)
./kiro-pool-proxy
sudo ufw allow 9999/tcp

# Trên CLIENT (máy chạy kiro-cli):
./set-endpoints.sh http://SERVER_IP:9999 us-east-1
kiro-cli chat
```

Không cần copy/trust cert gì cả. Nếu qua internet công cộng nên:
- Đặt proxy sau TLS (nginx/caddy) và set endpoint `https://...`
- Hoặc dùng SSH tunnel: `ssh -N -L 9999:127.0.0.1:9999 user@SERVER`

## Config

```json
{
  "listenAddr": "0.0.0.0:9999",
  "strategy": "smart",
  "poolKey": "",
  "accounts": [
    {
      "id": "acc-1",
      "email": "user@example.com",
      "accessToken": "eyJ...",
      "refreshToken": "eyJ...",
      "clientId": "...",
      "clientSecret": "...",
      "authMethod": "idc",
      "region": "us-east-1",
      "profileArn": "arn:aws:codewhisperer:us-east-1:...:profile/...",
      "enabled": true
    }
  ]
}
```

| Field | Mô tả |
|-------|-------|
| `listenAddr` | `127.0.0.1:9999` (local) hoặc `0.0.0.0:9999` (remote) |
| `strategy` | `round-robin` hoặc `smart` (ưu tiên account còn nhiều quota) |
| `poolKey` | Tùy chọn: shared secret, client gửi qua header `X-Pool-Key` |

## Auth Methods

| Method | Cần | Refresh endpoint |
|--------|-----|------------------|
| `idc` | refreshToken, clientId, clientSecret, region | `oidc.{region}.amazonaws.com/token` |
| `social` | refreshToken | `prod.us-east-1.auth.desktop.kiro.dev/refreshToken` |
| `external_idp` | refreshToken, clientId, tokenEndpoint | Custom IdP endpoint |
| `api_key` | accessToken (ksk_) | Không refresh |

## Token/Credit Accounting

Proxy tee response stream (forward nguyên bytes cho CLI + parse song song):

- **Credit per turn**: parse `meteringEvent` (cumulative, last-wins)
- **Per-account**: `credits`, `requests`, `lastUsedUnix` persist vào config.json (60s)
- **Context %**: parse `contextUsageEvent`
- **Quota-aware**: poll `GetUsageLimits` mỗi 5min (AGENTIC_REQUEST), skip account hết quota
- **Smart strategy**: ưu tiên account còn nhiều quota nhất

## Cấu trúc

```
kiro-cli-pool-proxy/
├── main.go                  # Entry point (plain reverse-proxy)
├── config/config.go         # Account + accounting
├── auth/
│   ├── refresh.go           # Token refresh
│   └── usage.go             # GetUsageLimits quota poller
├── pool/pool.go             # Account selection + cooldown + quota-aware
├── proxy/
│   ├── server.go            # Reverse-proxy handler + tee streaming
│   ├── rewrite.go           # profileArn swap + endpoint region table
│   └── eventstream.go       # AWS Event Stream parser (meteringEvent)
├── cmd/import-local/        # Import accounts từ kiro-cli SQLite
├── set-endpoints.sh         # Trỏ kiro-cli vào proxy
└── REVERSE_ENGINEERING.md   # Wire format spec từ binary
```

## Cập nhật theo kiro gốc

Proxy **không phụ thuộc phiên bản kiro-cli** — dùng cơ chế setting công khai
(`api.krs.service`) nên kiro-cli update lên version mới vẫn chạy. Chỉ cần chạy lại
`set-endpoints.sh` sau khi update (settings có thể bị reset).
