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

---

## Cập nhật RE (verified 2026-07-17, binary kiro-cli 2.12.2)

> Trích trực tiếp từ symbol/type names + serializer string literals trong binary
> (`crates/amzn-codewhisperer-client/...`). Các key **camelCase** dưới đây là literal
> serialize/deserialize thật trên wire; tên `snake_case` chỉ là field Rust nội bộ.

### GetUsageLimits — Input (`GetUsageLimitsInput`)

Field Rust: `profile_arn`, `resource_type`, `is_email_required`.
Wire keys (literal camelCase xác nhận trong binary): **`profileArn`**, **`resourceType`**,
**`isEmailRequired`**.

- `resourceType` là enum `ResourceType` (xem dưới); có thể lọc quota theo loại.
- Proxy hiện gửi `{origin: "KIRO_CLI", profileArn}` — server chấp nhận (đã verify E2E),
  `resourceType`/`isEmailRequired` optional. Có thể thêm `resourceType: AGENTIC_REQUEST`
  để truy vấn đúng loại quota chat.

### GetUsageLimits — Output (`GetUsageLimitsOutput`)

Field (Rust): `limits`, `next_date_reset`, `usage_breakdown`, `usage_breakdown_list`,
`subscription_info`, `overage_configuration`, `user_info`.

Wire keys camelCase xác nhận literal: **`usageBreakdownList`**, **`subscriptionInfo`**,
**`userInfo`**, **`currency`**, **`resourceType`**.

| Field | Type | Ý nghĩa |
|-------|------|---------|
| `usageBreakdownList[]` | `UsageBreakdown` | Breakdown theo `resourceType` (proxy đọc AGENTIC_REQUEST) |
| `limits[]` | `UsageLimitList` | Danh sách limit theo `UsageLimitType` |
| `subscriptionInfo` | `SubscriptionInfo` | Loại subscription + capability |
| `overageConfiguration` | `OverageConfiguration` | Cấu hình vượt hạn mức |
| `nextDateReset` | number | Epoch reset quota |
| `userInfo` | — | Thông tin user |

**`UsageBreakdown`** (Rust fields): `resource_type`, `current_usage`, `usage_limit`,
`current_overages`, `overage_charges`, `overage_rate`, `currency`, `free_trial_info`.

**`UsageLimitList`** (Rust fields): `r#type` (`UsageLimitType`), `current_usage`,
`total_usage_limit`, `percent_used`, `message`.

**`SubscriptionInfo`**: `r#type` (`SubscriptionType`), `upgrade_capable`, `overage_capable`.

**`FreeTrialInfo`**: `free_trial_status` (`FreeTrialStatus`: `ACTIVE`/`EXPIRED`),
`free_trial_expiry`.

**`OverageConfiguration`**: `overage_status`, `overage_cap`.

### Enums (xác nhận trong binary)

| Enum | Members / wire values |
|------|----------------------|
| `ResourceType` | `AGENTIC_REQUEST`, `CODE_COMPLETIONS`, `TRANSFORM` (Rust: AgenticRequest/CodeCompletions/Transform/Unknown) |
| `UsageLimitType` | `DailyRequestCount`, `MonthlyRequestCount`, `InsufficientModelCapacity` |
| `FreeTrialStatus` | `ACTIVE`, `EXPIRED` |
| `SubscriptionType` | `QDeveloperStandalone`, `...Free`, `...Power`, `...Pro`, `...ProPlus` |
| `GetUsageLimitsError` | `ValidationError`, `AccessDeniedError`, `InternalServerError`, `ThrottlingError` |

> **Quan trọng (đã verify E2E, commit 00cf8d7)**: quota `AGENTIC_REQUEST` đo bằng
> **INVOCATIONS** (số request), *không phải* credit. `meteringEvent` mới là credit (cost).
> Poll đọc ví dụ `5906/10000` invocations. Proxy tăng counter +1 invocation/request,
> poll 5 phút re-sync giá trị thật.

### SendTelemetryEvent — TelemetryEvent union (control-plane telemetry)

Members xác nhận: `chatAddMessageEvent`, `chatInteractWithMessageEvent`,
`chatUserModificationEvent`, `codeCoverageEvent`, `codeFixAcceptanceEvent`,
`codeFixGenerationEvent`, `codeScanEvent`, `codeScanFailedEvent`,
`codeScanRemediationsEvent`, `codeScanSucceededEvent`, `docGenerationEvent`,
`docV2AcceptanceEvent`, `docV2GenerationEvent`, `featureDevCodeAcceptanceEvent`,
`featureDevCodeGenerationEvent`, `featureDevEvent`, `inlineChatEvent`, `metricData`,
`terminalUserInteractionEvent`, `testGenerationEvent`, `transformEvent`,
`userModificationEvent`, `userTriggerDecisionEvent`.

Proxy transparent → forward nguyên, **không cần** parse các event này (chỉ tham chiếu).

### UserContext (wire enums)

- `ideCategory`: `CLI`, `ECLIPSE`, `JETBRAINS`, `JUPYTER_MD`, `JUPYTER_SM`,
  `VISUAL_STUDIO`, `VSCODE`.
- `operatingSystem`: `LINUX`, `MAC`, `WINDOWS`.
- Fields: `client_id`, `ide_version`, `product`, `operating_system`, `ide_category`.

### Streaming (runtime) — event accounting

Proxy parse AWS Event Stream, chỉ cần 2 event cho accounting:
`meteringEvent` (credit cumulative, last-wins) + `contextUsageEvent`
(`contextUsagePercentage`). Các event khác (assistantResponse/toolUse/followup...)
forward nguyên byte cho CLI, không parse.

### Live capture GetUsageLimits (2026-07-17, account KIRO POWER) — sự thật trên wire

```json
{
  "nextDateReset": 1785542400.0,
  "overageConfiguration": { "overageStatus": "DISABLED" },
  "subscriptionInfo": {
    "subscriptionTitle": "KIRO POWER",
    "type": "Q_DEVELOPER_STANDALONE_POWER",
    "overageCapability": "OVERAGE_CAPABLE",
    "upgradeCapability": "UPGRADE_INCAPABLE",
    "subscriptionManagementTarget": "MANAGE"
  },
  "usageBreakdownList": [
    { "resourceType": "CREDIT", "currentUsage": 5998, "usageLimit": 10000,
      "currency": "USD", "currentOverages": 0, "bonuses": [] }
  ]
}
```

**Chỉnh quan trọng**: breakdown thật dùng `resourceType` = **`CREDIT`** (không phải
`AGENTIC_REQUEST` trên account này) — `currentUsage/usageLimit` = 5998/10000 chính là
quota request chat. `AGENTIC_REQUEST` vẫn là enum value hợp lệ (plan khác). `usage.go`
đã sửa để match cả `CREDIT` lẫn `AGENTIC_REQUEST`, fallback breakdown[0].

Wire field bổ sung (xác nhận từ capture, không có trong strings): `overageStatus`,
`overageCapability`, `upgradeCapability`, `subscriptionManagementTarget`,
`subscriptionTitle`, `currentOverages`, `bonuses[]`. Input tối thiểu chỉ cần
`{profileArn}` (server bỏ qua `origin`; `resourceType`/`isEmailRequired` optional).

> **Lưu ý vận hành**: token của account có thể có `region` (vd eu-central-1) khác với
> region của **profile** thật (us-east-1). Phải resolve profileArn qua
> `ListAvailableProfiles` đúng region — proxy derive runtime/management host từ region
> của profileArn, không phải region của token.

### Login gate — bản chất pool: client KHÔNG cần login (verified 2026-07-17)

Reverse cổng đăng nhập local của kiro-cli (`fig_auth`, DeviceCode flow):

- Cổng "let's get you signed in" chỉ bật khi **thiếu** `kirocli:odic:token` trong bảng
  `auth_kv` HOẶC token đã hết hạn.
- Nếu tồn tại `kirocli:odic:token` với `expires_at` tương lai → kiro-cli coi như đã
  đăng nhập, vào thẳng `chat` và gửi request. **access_token/refresh_token KHÔNG cần
  hợp lệ** vì proxy ghi đè `Authorization` bằng token account pool. expiry xa nên CLI
  không gọi refresh (không đụng OIDC).
- Profile resolve qua `api.cps.service` (ListAvailableProfiles) → proxy swap sang
  account pool → trả profile pool. Client tự dùng đúng profileArn của pool.

**Kết quả**: máy khách chỉ cần (1) trỏ `api.krs.service`/`api.cps.service` vào proxy và
(2) seed 1 token giả. Không login, không account riêng, không tốn quota riêng.

Schema seed (`auth_kv`, table có sẵn sau bất kỳ lệnh kiro-cli nào):

```
key   kirocli:odic:token
value {"access_token":"POOL_PLACEHOLDER","refresh_token":"POOL_PLACEHOLDER",
       "expires_at":"2099-01-01T00:00:00Z","region":"us-east-1",
       "start_url":"https://pool.local/start",
       "scopes":["codewhisperer:completions","codewhisperer:analysis","codewhisperer:conversations"],
       "oauth_flow":"DeviceCode"}
```

Đã đóng gói trong `setup-client.sh` (đã test: HOME sạch + không login → chat OK,
credit đếm khớp qua proxy). Settings kiro-cli nằm ở `~/.kiro/settings/cli.json`,
DB ở `~/.local/share/kiro-cli/data.sqlite3`.
