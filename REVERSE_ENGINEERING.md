# Reverse Engineering kiro-cli

Findings từ binary thật `~/.local/bin/kiro-cli` (Rust, không strip, có debug_info) và
`~/.local/share/kiro-cli/data.sqlite3`. Phiên bản: **appVersion 2.12.2**.

> Mục đích: xác minh wire format để proxy transparent forward chính xác. Vì proxy là
> transparent (forward nguyên request CLI gửi), phần lớn thông tin này để tham chiếu
> và để viết tool import account từ local kiro-cli.

## Kiến trúc binary

```
kiro-cli (Rust, 114M)          ← binary chính
├── crates/q_cli               ← CLI entry / TUI cũ
├── crates/fig_api_client       ← API client (endpoints, profile, credentials, interceptors)
├── crates/fig_auth             ← auth (builder_id, external_idp, social, pkce, oauth_callback)
├── crates/fig_aws_common       ← http client, user_agent_override_interceptor
├── crates/fig_telemetry        ← telemetry
├── amzn_codewhisperer_client   ← SDK: generate_completions, get_usage_limits,
│                                  list_available_profiles, send_telemetry_event
├── amzn_consolas_client        ← SDK: generate_recommendations, list_customizations
└── amzn_toolkit_telemetry_client← SDK: post_metrics

tui.js (12M, bun/JS)           ← V3 TUI frontend (gọi Rust qua IPC)
bun (101M)                     ← JS runtime
data.sqlite3                   ← auth tokens, history, conversations
```

Chat agentic (V3) chạy qua tui.js → IPC → Rust binary → API. Các thao tác API
runtime dùng service `codewhispererruntime`.

## Endpoints (từ crates/fig_api_client/src/endpoints.rs)

### Management (control plane)

| Region | Endpoint |
|--------|----------|
| us-east-1 | `https://management.us-east-1.kiro.dev` |
| eu-central-1 | `https://management.eu-central-1.kiro.dev` |
| us-gov-east-1 | `https://management.us-gov-east-1.kiro.dev` |
| us-gov-west-1 | `https://management.us-gov-west-1.kiro.dev` |
| us-iso-east-1 | `https://kiro-management.us-iso-east-1.c2s.ic.gov` |
| us-isob-east-1 | `https://kiro-management.us-isob-east-1.sc2s.sgov.gov` |
| us-isof-south-1 | `https://kiro-management.us-isof-south-1.csp.hci.ic.gov` |
| us-isof-east-1 | `https://kiro-management.us-isof-east-1.csp.hci.ic.gov` |
| (fallback) | `https://codewhisperer.us-east-1.amazonaws.com` |

### Runtime (data plane — chat/assistant)

| Region | Endpoint |
|--------|----------|
| us-east-1 | `https://runtime.us-east-1.kiro.dev` |
| eu-central-1 | `https://runtime.eu-central-1.kiro.dev` |
| us-gov-east-1 | `https://runtime.us-gov-east-1.kiro.dev` |
| us-gov-west-1 | `https://runtime.us-gov-west-1.kiro.dev` |

### Legacy (vẫn còn dùng, sẽ deprecate)

- `https://q.{region}.amazonaws.com` (us-east-1, eu-central-1, us-gov-*)
- `https://codewhisperer.us-east-1.amazonaws.com`

### Auth endpoints

- Social: `https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken`
- IdC/BuilderID OIDC: `https://oidc.{region}.amazonaws.com` (register/authorize/token)
- Sign-in portal: `app.kiro.dev`

## Headers (wire — xác nhận từ binary + Kiro-Go captures)

| Header | Giá trị |
|--------|---------|
| `User-Agent` | `aws-sdk-rust/... app/AmazonQ-For-CLI` (appVersion **2.12.2**) |
| `x-amz-user-agent` | tương tự, với feature tag `m/F` |
| `Content-Type` | `application/x-amz-json-1.0` |
| `x-amz-target` | `AmazonCodeWhispererStreamingService.GenerateAssistantResponse` (runtime) |
| `x-amzn-codewhisperer-optout` | `false` (default) |
| `Authorization` | `Bearer {access_token}` |
| `tokentype` | theo auth_mode (xem dưới) — CLI construct động |

- App identifier: `AmazonQ-For-CLI` (crate `fig_aws_common::user_agent_override_interceptor`).
- `UserAgentOverrideInterceptor` ghi đè UA; `appVersion` = `2.12.2`.
- `OptOutInterceptor` set `x-amzn-codewhisperer-optout` (opt_out_preference OPTIN/OPTOUT).

## TokenType (từ fig_api_client::interceptor::token_type_interceptor)

`TokenTypeInterceptor` chạy tại `modify_before_signing`, đọc `auth_mode`. Enum values
xác nhận trong binary: **`Normal`**, **`ExternalIdp`**, **`ApiKey`**.

| auth_mode | tokentype header |
|-----------|------------------|
| Normal (IdC/BuilderID native) | *(không gửi / bỏ trống)* |
| ExternalIdp (social, enterprise IdP) | `EXTERNAL_IDP` |
| ApiKey (ksk_ tokens) | `API_KEY` |

## Token store (data.sqlite3, bảng `auth_kv`) — xác nhận từ DB thật

### `kirocli:odic:token` (lưu ý: "odic" — typo trong CLI, không phải "oidc")

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": "RFC3339 timestamp",
  "region": "us-east-1",
  "start_url": "https://view.awsapps.com/start",
  "scopes": ["codewhisperer:completions", "codewhisperer:analysis", "codewhisperer:conversations"],
  "oauth_flow": "..."
}
```

### `kirocli:odic:device-registration`

```json
{
  "client_id": "...",
  "client_secret": "...",
  "client_secret_expires_at": "...",
  "region": "us-east-1",
  "scopes": [...],
  "oauth_flow": "..."
}
```

### Các key khác (theo auth type)

- `kirocli:social:token` — social login token
- `kirocli:external-idp:token` — external IdP token

### Import mapping (kiro-cli → proxy account)

| Proxy field | Nguồn |
|-------------|-------|
| accessToken | `odic:token.access_token` |
| refreshToken | `odic:token.refresh_token` |
| expiresAt | `odic:token.expires_at` (parse RFC3339 → unix) |
| region | `odic:token.region` |
| clientId | `odic:device-registration.client_id` |
| clientSecret | `odic:device-registration.client_secret` |
| authMethod | `idc` (odic) / `social` / `external_idp` |

## OIDC device auth flow (crates/fig_auth/src/builder_id.rs)

- Grant type: `urn:ietf:params:oauth:grant-type:device_code`
- Scopes: `codewhisperer:completions`, `codewhisperer:analysis`, `codewhisperer:conversations`
- Register client: `POST oidc.{region}.amazonaws.com/client/register`
- Token: `POST oidc.{region}.amazonaws.com/token` (grantType `refresh_token` khi refresh)

## External IdP flow (crates/fig_auth/src/external_idp.rs)

- OIDC discovery: `{issuer}/.well-known/openid-configuration`
- Refresh: form-encoded `grant_type=refresh_token&client_id=...&scope=...`,
  `Content-Type: application/x-www-form-urlencoded`
- Callback loopback: `http://127.0.0.1:{port}/oauth/callback` (PKCE, port động)

## UserContext (telemetry / request context)

Fields: `ide_category` = **`CLI`** (enum: Cli/Eclipse/JetBrains/Jupyter/VisualStudio/VsCode),
`operating_system`, `product`, `client_id`, `ide_version`.

## Ý nghĩa cho proxy transparent

Vì proxy forward nguyên request CLI gửi, các điểm cần chú ý:

1. **Endpoint intercept**: `runtime.*.kiro.dev`, `management.*.kiro.dev`,
   legacy `q.*.amazonaws.com`, `codewhisperer.*.amazonaws.com`, + gov/iso hosts.
2. **Region derivation**: từ `profileArn` (`arn:aws:codewhisperer:{region}:...`).
3. **Swap khi xoay account**: `Authorization` header, `profileArn` trong body,
   `tokentype` header (theo auth_mode account mới).
4. **Giữ nguyên** mọi header khác (User-Agent, x-amz-*, opt-out) — CLI đã set đúng.
5. **Không cần** hardcode appVersion vì UA của CLI được forward nguyên.
