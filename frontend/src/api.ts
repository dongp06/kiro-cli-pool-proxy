export interface Overview {
  totalAccounts: number
  enabled: number
  available: number
  totalCredits: number
  totalRequests: number
  quotaUsed: number
  quotaLimit: number
  strategy: string
}

export interface Account {
  id: string
  email: string
  authMethod: string
  region: string
  enabled: boolean
  credits: number
  requests: number
  lastUsedUnix: number
  usageLimit: number
  usageCurrent: number
  nextResetUnix: number
  hasProfileArn: boolean
  tokenExpires: number
  plan: string
}

export interface Settings {
  strategy: string
  listenAddr: string
  adminPassword: boolean
  passwordEnv: boolean
}

export interface ApiKey {
  id: string
  name: string
  key: string
  enabled: boolean
  creditLimit: number
  requests: number
  credits: number
  createdUnix: number
  lastUsedUnix: number
}

export interface LogEntry {
  timeUnix: number
  account: string
  apiKey: string
  status: number
  credits: number
  kind: string
  err?: string
}

export interface KiroModel {
  modelId: string
  modelName?: string
}

/** One server-sent event: { type, data }. */
export interface ServerEvent {
  type: 'log' | 'quota' | 'accounts' | string
  data: unknown
}

const BASE = '/admin/api/'

export class Unauthorized extends Error {}

async function req(path: string, opts?: RequestInit): Promise<Response> {
  const r = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  })
  if (r.status === 401) throw new Unauthorized('unauthorized')
  return r
}

/** Result envelope for login flows: parsed JSON body plus HTTP-ok flag. */
export interface FlowResult<D = Record<string, unknown>> {
  ok: boolean
  data: D
}

async function flow<D = Record<string, unknown>>(path: string, body: Record<string, unknown>): Promise<FlowResult<D>> {
  const r = await req(path, { method: 'POST', body: JSON.stringify(body) })
  let data: D
  try { data = (await r.json()) as D } catch { data = {} as D }
  return { ok: r.ok, data }
}

export const api = {
  auth: () => fetch(BASE + 'auth').then((r) => r.json()) as Promise<{ authRequired: boolean; authed: boolean }>,
  login: (password: string) =>
    fetch(BASE + 'login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password }),
    }),
  logout: () => req('logout', { method: 'POST' }),
  overview: () => req('overview').then((r) => r.json() as Promise<Overview>),
  accounts: () => req('accounts').then((r) => r.json() as Promise<Account[]>),
  settings: () => req('settings').then((r) => r.json() as Promise<Settings>),
  setStrategy: (strategy: string) =>
    req('settings', { method: 'PATCH', body: JSON.stringify({ strategy }) }),
  setPassword: (current: string, newPassword: string) =>
    req('settings/password', { method: 'POST', body: JSON.stringify({ current, new: newPassword }) }).then(
      async (r) => ({ ok: r.ok, data: (await r.json().catch(() => ({}))) as { error?: string } })),
  keys: () => req('keys').then((r) => r.json() as Promise<ApiKey[]>),
  createKey: (name: string, creditLimit: number) =>
    req('keys', { method: 'POST', body: JSON.stringify({ name, creditLimit }) }).then((r) => r.json() as Promise<ApiKey>),
  toggleKey: (id: string, enabled: boolean) =>
    req('keys/' + id, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
  deleteKey: (id: string) => req('keys/' + id, { method: 'DELETE' }),
  toggleAccount: (id: string, enabled: boolean) =>
    req('accounts/' + id, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
  deleteAccount: (id: string) => req('accounts/' + id, { method: 'DELETE' }),
  testAccount: (id: string) =>
    req('accounts/' + id + '/test', { method: 'POST' }).then(
      (r) => r.json() as Promise<{ ok: boolean; email?: string; hasProfileArn?: boolean; error?: string }>),
  refreshQuota: (id: string) =>
    req('accounts/' + id + '/refresh-quota', { method: 'POST' }).then(
      (r) => r.json() as Promise<{ ok: boolean; account?: Account; error?: string }>),
  addAccount: (acc: Record<string, unknown>) =>
    req('accounts', { method: 'POST', body: JSON.stringify(acc) }),
  importLocal: (body: Record<string, unknown>) =>
    req('accounts/import-local', { method: 'POST', body: JSON.stringify(body) }),
  logs: () => req('logs').then((r) => r.json() as Promise<LogEntry[]>),
  clearLogs: () => req('logs', { method: 'DELETE' }),

  // --- Login flows (backed by /admin/api/auth/*) ---
  iamSsoStart: (startUrl: string, region: string) =>
    flow<{ sessionId: string; authorizeUrl: string; expiresIn: number; error?: string }>(
      'auth/iam-sso/start', { startUrl, region }),
  iamSsoComplete: (sessionId: string, callbackUrl: string) =>
    flow<{ success: boolean; account?: { id: string; email: string }; error?: string }>(
      'auth/iam-sso/complete', { sessionId, callbackUrl }),
  builderIdStart: (region: string) =>
    flow<{ sessionId: string; userCode: string; verificationUri: string; interval: number; error?: string }>(
      'auth/builderid/start', { region }),
  builderIdPoll: (sessionId: string) =>
    flow<{ success: boolean; completed: boolean; status?: string; interval?: number; account?: { id: string; email: string }; error?: string }>(
      'auth/builderid/poll', { sessionId }),
  importSsoToken: (bearerToken: string, region: string) =>
    flow<{ success: boolean; accounts: { id: string; email: string }[]; errors: string[]; error?: string }>(
      'auth/sso-token', { bearerToken, region }),
  microsoftSsoStart: () =>
    flow<{ sessionId: string; authorizeUrl: string; expiresIn: number; stage: string; error?: string }>(
      'auth/microsoft-sso/start', {}),
  microsoftSsoComplete: (sessionId: string, callbackUrl: string) =>
    flow<{ success: boolean; stage: string; authorizeUrl?: string; account?: { id: string; email: string }; error?: string }>(
      'auth/microsoft-sso/complete', { sessionId, callbackUrl }),
  microsoftSsoCancel: (sessionId: string) =>
    flow<{ success: boolean }>('auth/microsoft-sso/cancel', { sessionId }),
  importApiKey: (apiKey: string, region: string) =>
    flow<{ success: boolean; account?: { id: string; email: string; region: string }; error?: string }>(
      'auth/api-key', { apiKey, region }),

  // --- Models ---
  models: () => req('models').then((r) => r.json() as Promise<KiroModel[]>),
  syncModels: () =>
    req('models/sync', { method: 'POST' }).then(
      (r) => r.json() as Promise<{ ok: boolean; models?: KiroModel[]; error?: string }>),
  testModel: (id: string, model: string) =>
    req('accounts/' + id + '/test-model', { method: 'POST', body: JSON.stringify({ model }) }).then(
      (r) => r.json() as Promise<{ ok: boolean; model?: string; reply?: string; error?: string; latencyMs?: number }>),

  // --- Realtime (SSE). Returns the EventSource so the caller can close it. ---
  events: (onEvent: (e: ServerEvent) => void, onError?: () => void): EventSource => {
    const es = new EventSource(BASE + 'events')
    es.onmessage = (m) => {
      try { onEvent(JSON.parse(m.data) as ServerEvent) } catch { /* ignore malformed frame */ }
    }
    if (onError) es.onerror = onError
    return es
  },
}
