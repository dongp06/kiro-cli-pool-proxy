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
}

export interface Settings {
  strategy: string
  listenAddr: string
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
  keys: () => req('keys').then((r) => r.json() as Promise<ApiKey[]>),
  createKey: (name: string, creditLimit: number) =>
    req('keys', { method: 'POST', body: JSON.stringify({ name, creditLimit }) }).then((r) => r.json() as Promise<ApiKey>),
  toggleKey: (id: string, enabled: boolean) =>
    req('keys/' + id, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
  deleteKey: (id: string) => req('keys/' + id, { method: 'DELETE' }),
  toggleAccount: (id: string, enabled: boolean) =>
    req('accounts/' + id, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
  deleteAccount: (id: string) => req('accounts/' + id, { method: 'DELETE' }),
  addAccount: (acc: Record<string, unknown>) =>
    req('accounts', { method: 'POST', body: JSON.stringify(acc) }),
  importLocal: (body: Record<string, unknown>) =>
    req('accounts/import-local', { method: 'POST', body: JSON.stringify(body) }),
  logs: () => req('logs').then((r) => r.json() as Promise<LogEntry[]>),
  clearLogs: () => req('logs', { method: 'DELETE' }),
}
