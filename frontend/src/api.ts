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
  toggleAccount: (id: string, enabled: boolean) =>
    req('accounts/' + id, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
  deleteAccount: (id: string) => req('accounts/' + id, { method: 'DELETE' }),
  addAccount: (acc: Record<string, unknown>) =>
    req('accounts', { method: 'POST', body: JSON.stringify(acc) }),
  importLocal: (body: Record<string, unknown>) =>
    req('accounts/import-local', { method: 'POST', body: JSON.stringify(body) }),
}
