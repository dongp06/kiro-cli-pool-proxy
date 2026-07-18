# KiroPool

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
./set-endpoints.sh http://127.0.0.1:5000 us-east-1
# hoặc thủ công:
kiro-cli settings api.krs.service '{"endpoint":"http://127.0.0.1:5000","region":"us-east-1"}'
kiro-cli settings api.cps.service '{"endpoint":"http://127.0.0.1:5000","region":"us-east-1"}'
```

### 5. Dùng kiro-cli như bình thường

```bash
kiro-cli chat
# Mọi request đi qua proxy: xoay account + đếm credit
```

Khôi phục về mặc định: `./set-endpoints.sh --reset`

## Dùng Claude Code CLI / opencode (Anthropic API)

Ngoài kiro-cli gốc, proxy còn expose endpoint **Anthropic Messages API** tương thích
tại `POST /v1/messages`. Nó dịch Anthropic ↔ Kiro `GenerateAssistantResponse` hai chiều
(request: messages/system/tools → conversationState; response: event-stream → Anthropic
SSE), swap account pool và **đếm credit per-key** như kiro-cli.

> Client KHÔNG cần login Kiro, KHÔNG cần account riêng. Chỉ cần 1 API key của pool
> (`kpp_...`) làm khóa xác thực. Proxy tự swap sang account thật.

### Claude Code CLI

```bash
export ANTHROPIC_BASE_URL=http://SERVER_IP:5000
export ANTHROPIC_API_KEY=kpp_xxxxxxxxxxxxxxxx   # API key tạo trong admin panel
claude
```

Claude Code sẽ gọi `http://SERVER_IP:5000/v1/messages` (gửi key qua header `x-api-key`).

### opencode

opencode hỗ trợ provider Anthropic-compatible. Trỏ baseURL vào proxy trong config
(`~/.config/opencode/opencode.json` hoặc `opencode.json` trong project):

```json
{
  "provider": {
    "kiro-pool": {
      "npm": "@ai-sdk/anthropic",
      "options": {
        "baseURL": "http://SERVER_IP:5000/v1",
        "apiKey": "kpp_xxxxxxxxxxxxxxxx"
      },
      "models": { "claude-3-5-sonnet": { "name": "Kiro Pool (Sonnet)" } }
    }
  }
}
```

Rồi chọn model `kiro-pool/claude-3-5-sonnet` trong opencode.

### Ghi chú

- **Model mapping**: mặc định mọi model Anthropic → Kiro `modelId: "auto"` (server tự
  chọn model mạnh nhất account có). Ép model khác qua env `KPP_KIRO_MODEL`.
- **Stream + non-stream** đều hỗ trợ (`"stream": true` phát SSE chuẩn Anthropic).
- **Tool calling**: đã cài đặt (tool_use / tool_result ↔ Kiro toolUses/toolResults) —
  xác minh với client thực tế đang tiến hành.
- Credit mỗi request được cộng vào API key tương ứng; hết `creditLimit` → HTTP 402.

## Dùng Codex / client OpenAI (OpenAI Chat Completions API)

Proxy cũng expose endpoint **OpenAI Chat Completions** tại `POST /v1/chat/completions`
(+ `/v1/models` stub). Dịch OpenAI ↔ Kiro, swap account pool, đếm credit per-key.

### Codex CLI / client OpenAI

```bash
export OPENAI_BASE_URL=http://SERVER_IP:5000/v1
export OPENAI_API_KEY=kpp_xxxxxxxxxxxxxxxx
codex
```

Client gọi `http://SERVER_IP:5000/v1/chat/completions` (key qua `Authorization: Bearer`).

### opencode (provider OpenAI-compatible)

```json
{
  "provider": {
    "kiro-pool-oai": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "http://SERVER_IP:5000/v1",
        "apiKey": "kpp_xxxxxxxxxxxxxxxx"
      },
      "models": { "kiro-auto": { "name": "Kiro Pool (auto)" } }
    }
  }
}
```

Hỗ trợ cả `stream: true` (phát `chat.completion.chunk` + `data: [DONE]`) lẫn non-stream.
Tool calling (`tools`/`tool_calls`/`role:tool`) đã map sang Kiro toolUses/toolResults.

## Web Admin Panel

Mở `http://localhost:5000/admin` để quản lý qua giao diện (dark dashboard):

- **KPI cards**: tổng accounts, requests, credits, quota
- **Bảng accounts**: quota bar (invocations), credits, requests, status, bật/tắt/xóa
- **Thêm account**: form theo auth method (idc/social/external_idp/api_key)
- **Import từ kiro-cli**: đọc SQLite local 1 click
- **Connection helper**: copy lệnh `settings` để trỏ kiro-cli vào proxy
- **Strategy selector**: đổi round-robin ↔ smart

Bảo vệ admin bằng mật khẩu (config `adminPassword`):

```json
{ "adminPassword": "your-secure-password" }
```

Để trống = không auth (chỉ nên dùng khi bind 127.0.0.1). Account tokens **không bao giờ**
được trả về UI (đã sanitize).

## Chạy proxy trên máy khác (remote)

Vì đây là plain HTTP proxy (không MITM), chạy remote rất đơn giản:

```bash
# Trên SERVER:
#   config.json: "listenAddr": "0.0.0.0:5000", "poolKey": "secret" (tùy chọn)
./kiro-pool-proxy
sudo ufw allow 5000/tcp

# Trên CLIENT (máy chạy kiro-cli):
./set-endpoints.sh http://SERVER_IP:5000 us-east-1
kiro-cli chat
```

Không cần copy/trust cert gì cả. Nếu qua internet công cộng nên:
- Đặt proxy sau TLS (nginx/caddy) và set endpoint `https://...`
- Hoặc dùng SSH tunnel: `ssh -N -L 5000:127.0.0.1:5000 user@SERVER`

## Config

```json
{
  "listenAddr": "0.0.0.0:5000",
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
| `listenAddr` | `127.0.0.1:5000` (local) hoặc `0.0.0.0:5000` (remote) |
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
