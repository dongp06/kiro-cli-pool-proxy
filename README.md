<div align="center">

<img src="frontend/public/kiro.svg" alt="KiroPool logo" width="88" height="88" />

# KiroPool

**A zero-MITM reverse proxy that pools & rotates Kiro accounts — and exposes them through Anthropic, OpenAI, and native `kiro-cli` APIs.**

Point any Claude Code, Codex, opencode, or `kiro-cli` client at one endpoint. KiroPool rotates a pool of Kiro accounts behind it, swaps credentials transparently, meters credits per API key, and tracks each account's subscription quota — all without certificates, forks, or client-side login.

<br/>

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black)](https://react.dev/)
[![Vite](https://img.shields.io/badge/Vite-6-646CFF?logo=vite&logoColor=white)](https://vite.dev/)
[![Tailwind CSS](https://img.shields.io/badge/Tailwind-4-06B6D4?logo=tailwindcss&logoColor=white)](https://tailwindcss.com/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](#-docker-setup)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](#-license)

</div>

---

## Table of Contents

- [Why KiroPool](#-why-kiropool)
- [Features](#-features)
- [How it works](#-how-it-works)
- [Quick start](#-quick-start)
- [Docker setup](#-docker-setup)
- [Connecting clients](#-connecting-clients)
- [Pooling accounts](#-pooling-accounts)
- [Subscription tiers & quota](#-subscription-tiers--quota)
- [Admin panel](#-admin-panel)
- [Configuration reference](#-configuration-reference)
- [Running remotely](#-running-remotely)
- [Project layout](#-project-layout)
- [Star history](#-star-history)
- [License](#-license)

---

## ✨ Why KiroPool

Kiro subscriptions are per-account, and every account has a finite credit quota that resets on a schedule. If you drive Kiro hard through a coding agent, a single account runs dry fast.

KiroPool puts **many accounts behind one endpoint**. Clients authenticate with a lightweight pool API key — they never see or manage Kiro credentials. The proxy picks a healthy account for every request, swaps in that account's real token, streams the response straight back, and meters the credits it cost. When an account is exhausted, KiroPool routes around it.

Because it uses `kiro-cli`'s **public endpoint-override settings** rather than intercepting TLS, there is no cert to install, no MITM, and nothing that breaks when Kiro ships a new CLI version.

---

## 🚀 Features

| | Feature |
|---|---|
| 🔁 | **Account pooling & rotation** — `round-robin` or `smart` (quota-aware) strategy that skips exhausted accounts. |
| 🧩 | **Three API surfaces** — native `kiro-cli`, Anthropic Messages (`/v1/messages`), and OpenAI Chat Completions (`/v1/chat/completions`). |
| 🔑 | **Per-key credit metering** — issue `kpp_…` API keys with individual credit limits; hit the limit → `HTTP 402`. |
| 📊 | **Subscription quota tracking** — polls `GetUsageLimits` and surfaces remaining vs. total credit per account (Pro, Pro+, Pro Max, Power). |
| 🧠 | **Thinking / reasoning passthrough** — streams `reasoningContentEvent` so Claude Code, opencode, and Codex show model thinking. |
| 🛠️ | **Tool calling** — bidirectional mapping between Anthropic/OpenAI tool calls and Kiro `toolUses`/`toolResults`. |
| 🎯 | **Model mapping** — valid Kiro `modelId`s pass through verbatim; Anthropic/OpenAI names are mapped; unknowns fall back to `auto`. |
| 🔐 | **Multiple auth methods** — IAM Identity Center (SSO), AWS Builder ID, external IdP (Microsoft Entra, etc.), and Kiro API keys. |
| 📥 | **One-click import** — read credentials directly from a local `kiro-cli` SQLite store (pure-Go, no CGO). |
| 🖥️ | **Embedded admin panel** — React + Tailwind dashboard baked into the binary at `/admin`, with a live SSE event stream. |
| 📦 | **Single static binary** — the frontend is `//go:embed`-ed; ship one file, or one small Docker image. |
| 🌍 | **Multi-region** — `us-east-1`, `eu-central-1`, `us-gov-east-1`, `us-gov-west-1`. |

---

## 🧠 How it works

`kiro-cli` exposes three settings that override its service endpoints. Point them at KiroPool and the CLI sends plain HTTP straight to the proxy — no certificate trust required.

| Setting | Upstream it targets | Used for |
|---|---|---|
| `api.krs.service` | `runtime.{region}.kiro.dev` | **Chat** (`GenerateAssistantResponse`) |
| `api.cps.service` | `management.{region}.kiro.dev` | Profiles, usage limits |
| `api.codewhisperer.service` | legacy `q.amazonaws.com` | Telemetry (optional) |

```
kiro-cli / Claude Code / Codex ──HTTP──▶  KiroPool  ──HTTPS──▶  runtime.{region}.kiro.dev
        (native client)                     │
                                            ├─ Swap Authorization + profileArn + tokentype
                                            ├─ Rotate account (round-robin / smart quota-aware)
                                            ├─ Tee the response stream → meter credits (meteringEvent)
                                            └─ Refresh tokens + poll GetUsageLimits for quota
```

The client stays **100% native** — system prompts, tools, thinking, agentic loops, and context management are all handled by the client. KiroPool only rotates accounts and counts credits.

> Verified against `kiro-cli` 2.12.2: chat through the proxy returns correctly and metered credits match the CLI's own count (0.16 ≈ 0.1619).

---

## ⚡ Quick start

**Prerequisites:** Go 1.25+ (to build from source), or Docker (see below). At least one Kiro account.

### 1. Build

```bash
git clone https://github.com/dongp06/kiro-cli-pool-proxy.git
cd kiro-cli-pool-proxy
go build -o kiro-pool-proxy .
```

The admin UI is prebuilt and embedded, so this produces a single self-contained binary. To rebuild the UI after frontend changes:

```bash
cd frontend
npm install
npm run build      # emits to ../proxy/webdist (embedded on next `go build`)
npx tsc --noEmit   # Vite does not typecheck — run this separately
```

### 2. Create a config

Running once with no config writes a template you can edit:

```bash
./kiro-pool-proxy --config config.json
# → "Created template config … edit it with your accounts, then re-run."
```

Or import an already-logged-in `kiro-cli` account (reads its SQLite store):

```bash
go run ./cmd/import-local
```

### 3. Run

```bash
./kiro-pool-proxy --config config.json
```

```
╔══════════════════════════════════════════════════════════════╗
║   KiroPool  ·  Kiro account pool + Anthropic/OpenAI gateway  ║
╠══════════════════════════════════════════════════════════════╣
║   Listen:   0.0.0.0:5000                                      ║
║   Accounts: 3                                                 ║
║   Strategy: smart                                             ║
╠══════════════════════════════════════════════════════════════╣
║   Admin panel: http://<SERVER_IP>:5000/admin                 ║
╚══════════════════════════════════════════════════════════════╝
```

### 4. Point a client at it

```bash
./set-endpoints.sh http://127.0.0.1:5000 us-east-1
kiro-cli chat        # every request now flows through the pool
```

Restore defaults any time with `./set-endpoints.sh --reset`.

---

## 🐳 Docker setup

The image is a pure Go build (the admin SPA is already embedded), so it's small and fast to build. Nothing but a Go toolchain is needed at build time.

### Docker Compose (recommended)

```bash
git clone https://github.com/dongp06/kiro-cli-pool-proxy.git
cd kiro-cli-pool-proxy

mkdir -p data
./kiro-pool-proxy --config data/config.json    # or copy an existing config.json into ./data
# edit data/config.json with your accounts

docker compose up -d --build
```

`docker-compose.yml` maps port `5000` and mounts `./data` so your `config.json` and usage counters persist across restarts.

```yaml
services:
  kiro-pool-proxy:
    build: .
    image: kiro-pool-proxy:latest
    container_name: kiro-pool-proxy
    restart: unless-stopped
    ports:
      - "5000:5000"
    volumes:
      - ./data:/app/data
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:5000/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 5s
```

### Plain Docker

```bash
docker build -t kiro-pool-proxy .
docker run -d --name kiro-pool-proxy \
  -p 5000:5000 \
  -v "$(pwd)/data:/app/data" \
  kiro-pool-proxy
```

The container reads its config from `/app/data/config.json`. Put your `config.json` in the mounted `./data` directory before starting.

> **Security:** if you expose the container beyond localhost, set both `adminPassword` (protects `/admin`) and a `poolKey` / API keys (protect the proxy endpoints). See [Running remotely](#-running-remotely).

---

## 🔌 Connecting clients

KiroPool serves three API surfaces on the same port. All of them accept a pool API key (`kpp_…`) created in the admin panel — clients never handle real Kiro credentials.

### `kiro-cli` (native)

```bash
./set-endpoints.sh http://SERVER_IP:5000 us-east-1
# or manually:
kiro-cli settings api.krs.service '{"endpoint":"http://SERVER_IP:5000","region":"us-east-1"}'
kiro-cli settings api.cps.service '{"endpoint":"http://SERVER_IP:5000","region":"us-east-1"}'
```

Zero-login clients (no Kiro account of their own) can use `setup-client.sh SERVER_URL REGION KEY`, which points the endpoints **and** seeds a placeholder token so the CLI's local login gate passes. The proxy swaps in a real pooled account.

### Claude Code CLI / opencode (Anthropic API → `/v1/messages`)

```bash
export ANTHROPIC_BASE_URL=http://SERVER_IP:5000
export ANTHROPIC_API_KEY=kpp_xxxxxxxxxxxxxxxx   # created in the admin panel
claude
```

### OpenAI Codex CLI (OpenAI API → `/v1/chat/completions`)

```bash
export OPENAI_BASE_URL=http://SERVER_IP:5000/v1
export OPENAI_API_KEY=kpp_xxxxxxxxxxxxxxxx
codex
```

Both surfaces support streaming (`stream: true`) and non-streaming, tool calling, and thinking/reasoning passthrough. Every request's credit cost is charged to the API key; when a key exceeds its `creditLimit` the proxy returns `HTTP 402`.

### Endpoint summary

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health` | Liveness check |
| `POST` | `/v1/messages` (also `/anthropic/v1/messages`) | Anthropic Messages |
| `POST` | `/v1/messages/count_tokens` | Token counting |
| `POST` | `/v1/chat/completions` (also `/openai/v1/chat/completions`) | OpenAI Chat Completions |
| `GET` | `/v1/models`, `/openai/v1/models` | Model list |
| `GET` | `/setup-client.sh` | Serves the client setup script |
| — | `/admin`, `/admin/api/*` | Admin panel + API (SSE at `/admin/api/events`) |

---

## 👥 Pooling accounts

An account is one Kiro identity plus its credentials. Add accounts three ways:

1. **Admin panel** → *Accounts* → *Add account* (pick the auth method, paste tokens).
2. **Import from local `kiro-cli`** → `go run ./cmd/import-local`, or the *Import kiro-cli* button.
3. **Edit `config.json`** directly and restart.

### Auth methods

| `authMethod` | Requires | Token refresh endpoint |
|---|---|---|
| `idc` | `refreshToken`, `clientId`, `clientSecret`, `region` | `oidc.{region}.amazonaws.com/token` |
| `social` | `refreshToken` (AWS Builder ID) | `prod.us-east-1.auth.desktop.kiro.dev/refreshToken` |
| `external_idp` | `refreshToken`, `clientId`, `tokenEndpoint` (Microsoft Entra, etc.) | custom IdP endpoint |
| `api_key` | `accessToken` (`ksk_…`) | not refreshed |

> A `profileArn` is required for chat and for `GetUsageLimits` on `idc`/`social`/`external_idp` accounts. `api_key` accounts use the region-bound GET form instead.

### Rotation strategies

- **`round-robin`** — cycle through enabled accounts in order.
- **`smart`** — prefer the account with the most remaining quota, and skip any that are exhausted.

Switch strategies live in *Settings* (applies to the next request) or via `"strategy"` in the config.

---

## 💳 Subscription tiers & quota

Each Kiro account carries a subscription tier. KiroPool reads the tier and its credit quota from the `GetUsageLimits` control-plane call and shows **remaining vs. total credit** per account in the admin UI.

| Tier | `subscriptionType` |
|---|---|
| Free | `FREE` |
| Pro | `PRO` |
| Pro+ | `PRO_PLUS` |
| Pro Max / Power | `POWER` |

> KiroPool tracks whatever `subscriptionType` the account reports and does not hard-code the tier list. The values above are the ones confirmed from the Kiro control plane; higher-tier plans (marketed as "Pro Max" / "Power") report `POWER` with a larger credit `usageLimit`.

**How quota is read.** `GetUsageLimits` returns a `usageBreakdownList`. KiroPool picks the `CREDIT` breakdown first, then `AGENTIC_REQUEST`, then falls back to the first entry:

- `usageLimit` — total credit the tier grants this period.
- `usageCurrent` — credit consumed so far.
- **remaining** = `usageLimit − usageCurrent` (computed for the UI meter).
- `nextResetUnix` — when the quota rolls over.

Example: a Power plan may report `resourceType="CREDIT"` with `5998 / 10000`. The `smart` strategy uses these numbers to prefer accounts with the most headroom and to skip exhausted ones. The account card's quota meter turns amber at 70% and red at 85% usage.

---

## 🖥️ Admin panel

Open `http://SERVER_IP:5000/admin` for the dashboard (React 18 + Tailwind 4, dark/light, EN/VI):

- **KPI cards** — total accounts, requests, credits, and aggregate quota.
- **Accounts** — per-account quota meter (remaining/total credit), credits, requests, status, and enable/disable/delete. Search and filter by state.
- **Add / import accounts** — auth-method-aware form, plus one-click `kiro-cli` SQLite import.
- **API keys** — create `kpp_…` keys with optional per-key credit limits.
- **Connection helper** — copy-paste commands to point any client at the proxy.
- **Strategy selector** — toggle `round-robin` ↔ `smart`.
- **Live logs** — realtime SSE stream (`log`, `quota`, `accounts` events).

Protect it with a password:

```json
{ "adminPassword": "your-secure-password" }
```

Leaving it empty means no auth — only acceptable when bound to `127.0.0.1`. Account tokens are **never** returned to the UI; they are sanitized server-side.

---

## ⚙️ Configuration reference

```json
{
  "listenAddr": "0.0.0.0:5000",
  "strategy": "smart",
  "poolKey": "",
  "adminPassword": "",
  "apiKeys": [
    { "id": "key-1", "name": "laptop", "key": "kpp_…", "creditLimit": 0, "enabled": true }
  ],
  "accounts": [
    {
      "id": "account-1",
      "email": "user@example.com",
      "accessToken": "eyJ…",
      "refreshToken": "eyJ…",
      "clientId": "…",
      "clientSecret": "…",
      "authMethod": "idc",
      "region": "us-east-1",
      "profileArn": "arn:aws:codewhisperer:us-east-1:123456789:profile/xxxxxxxx",
      "enabled": true
    }
  ]
}
```

| Field | Description |
|---|---|
| `listenAddr` | `127.0.0.1:5000` (local) or `0.0.0.0:5000` (remote). |
| `strategy` | `round-robin` or `smart` (quota-aware). |
| `poolKey` | Optional shared secret; clients send it as the `X-Pool-Key` header. |
| `adminPassword` | Protects `/admin`. Empty = no auth (localhost only). |
| `apiKeys[]` | Pool API keys clients present as the bearer token. `creditLimit: 0` = unlimited. |
| `accounts[]` | Pooled Kiro accounts. See [Pooling accounts](#-pooling-accounts). |

> **Never commit real tokens or `config.json` with credentials.** `config.json` and `data/` are git-ignored and Docker-ignored for this reason. Rotate any secret that leaks.

---

## 🌐 Running remotely

Because this is a plain HTTP proxy (no MITM), remote deployment is straightforward:

```bash
# On the SERVER — config.json: "listenAddr": "0.0.0.0:5000", set a poolKey / API keys
./kiro-pool-proxy
sudo ufw allow 5000/tcp

# On the CLIENT
./set-endpoints.sh http://SERVER_IP:5000 us-east-1
kiro-cli chat
```

No certificate to copy or trust. For exposure over the public internet, either:

- put KiroPool behind TLS (nginx/Caddy) and use an `https://…` endpoint, **or**
- tunnel over SSH: `ssh -N -L 5000:127.0.0.1:5000 user@SERVER`.

Always set `adminPassword` **and** a `poolKey` (or API keys) when the proxy is reachable from anything other than localhost.

---

## 📁 Project layout

```
kiro-cli-pool-proxy/
├── main.go                  # Entry point (plain reverse-proxy)
├── Dockerfile               # Pure-Go multi-stage build
├── docker-compose.yml       # Compose service (port 5000, ./data volume)
├── config/config.go         # Account model + usage accounting
├── auth/
│   ├── refresh.go           # Token refresh (idc / social / external_idp)
│   └── usage.go             # GetUsageLimits quota reader
├── pool/pool.go             # Account selection + cooldown + quota-aware
├── proxy/
│   ├── server.go            # HTTP router + tee streaming
│   ├── rewrite.go           # profileArn swap + region endpoint table
│   ├── eventstream.go       # AWS Event Stream parser (meteringEvent)
│   └── webdist/             # Embedded admin SPA (//go:embed)
├── frontend/                # React 18 + TS + Vite 6 + Tailwind 4 admin UI
├── cmd/import-local/        # Import accounts from kiro-cli SQLite
├── set-endpoints.sh         # Point kiro-cli at the proxy
└── setup-client.sh          # Zero-login client setup
```

---

## ⭐ Star history

If KiroPool saves you some credits, a star helps.

<div align="center">

<a href="https://star-history.com/#dongp06/kiro-cli-pool-proxy&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=dongp06/kiro-cli-pool-proxy&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=dongp06/kiro-cli-pool-proxy&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=dongp06/kiro-cli-pool-proxy&type=Date" width="640" />
  </picture>
</a>

</div>

---

## 📄 License

Released under the [MIT License](LICENSE).

<div align="center">
<sub>Built for developers who'd rather design systems than babysit quotas.</sub>
</div>
