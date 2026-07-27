import { useState, type SVGProps } from 'react'
import { api } from '../api'
import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import * as I from '../icons'
import { Modal } from './Modal'
import { IamFlow, BuilderIdFlow, TokenFlow, ApiKeyFlow, MicrosoftFlow } from './LoginModal'

type Mode =
  | 'apikey' | 'iam' | 'builderid' | 'microsoft' | 'token'
  | 'importlocal' | 'manual'

const MODES: { id: Mode; labelKey: string; descKey: string; Icon: (p: SVGProps<SVGSVGElement>) => JSX.Element }[] = [
  { id: 'apikey', labelKey: 'add2.m.apikey', descKey: 'add2.d.apikey', Icon: I.Key },
  { id: 'iam', labelKey: 'add2.m.iam', descKey: 'add2.d.iam', Icon: I.LogIn },
  { id: 'builderid', labelKey: 'add2.m.builderid', descKey: 'add2.d.builderid', Icon: I.Shield },
  { id: 'microsoft', labelKey: 'add2.m.microsoft', descKey: 'add2.d.microsoft', Icon: I.Globe },
  { id: 'token', labelKey: 'add2.m.token', descKey: 'add2.d.token', Icon: I.Download },
  { id: 'importlocal', labelKey: 'add2.m.importlocal', descKey: 'add2.d.importlocal', Icon: I.Download },
  { id: 'manual', labelKey: 'add2.m.manual', descKey: 'add2.d.manual', Icon: I.Layers },
]

export function AddAccountModal({ t, region, onClose, onDone, toast }: {
  t: T
  region: string
  onClose: () => void
  onDone: () => void
  toast: ToastFn
}) {
  const [mode, setMode] = useState<Mode | ''>('')
  const common = { t, toast, onDone }

  return (
    <Modal title={t('add2.title')} onClose={onClose}>
      {mode === '' ? (
        <>
          <p className="text-[var(--muted)] mt-0 mb-3 text-[13px]">{t('add2.pick')}</p>
          <div className="grid gap-2.5" style={{ gridTemplateColumns: 'repeat(auto-fill,minmax(220px,1fr))' }}>
            {MODES.map(({ id, labelKey, descKey, Icon }) => (
              <button key={id} type="button" onClick={() => setMode(id)}
                className="text-left flex items-start gap-3 px-3.5 py-3 rounded-xl border border-[var(--border)] bg-[var(--panel2)] hover:border-[var(--text)] transition-colors cursor-pointer">
                <span className="w-9 h-9 flex-none rounded-[10px] grid place-items-center bg-[var(--panel3)] text-[var(--accent)] text-base" aria-hidden="true"><Icon /></span>
                <span className="min-w-0">
                  <span className="block font-semibold text-[13.5px]">{t(labelKey)}</span>
                  <span className="block text-[var(--faint)] text-[11.5px] leading-snug">{t(descKey)}</span>
                </span>
              </button>
            ))}
          </div>
        </>
      ) : (
        <>
          <button type="button" onClick={() => setMode('')}
            className="inline-flex items-center gap-1.5 text-[13px] text-[var(--muted)] hover:text-[var(--text)] mb-1 cursor-pointer bg-transparent border-0 p-0">
            <span className="text-base leading-none">‹</span> {t('add2.back')}
          </button>
          <h4 className="m-0 mt-1 mb-1 text-[14px] font-bold flex items-center gap-2">
            {t(MODES.find((m) => m.id === mode)!.labelKey)}
          </h4>
          {mode === 'apikey' && <ApiKeyFlow {...common} />}
          {mode === 'iam' && <IamFlow {...common} />}
          {mode === 'builderid' && <BuilderIdFlow {...common} />}
          {mode === 'microsoft' && <MicrosoftFlow {...common} />}
          {mode === 'token' && <TokenFlow {...common} />}
          {mode === 'importlocal' && <ImportLocalForm {...common} region={region} />}
          {mode === 'manual' && <ManualForm {...common} onClose={() => setMode('')} />}
        </>
      )}
    </Modal>
  )
}

// --- Import from kiro-cli local SQLite ---

function ImportLocalForm({ t, region, toast, onDone }: {
  t: T; region: string; toast: ToastFn; onDone: () => void
}) {
  const [path, setPath] = useState('')
  const [arn, setArn] = useState('')
  const [reg, setReg] = useState(region || 'us-east-1')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    try {
      const r = await api.importLocal({ path, profileArn: arn, region: reg })
      const j = await r.json().catch(() => ({}))
      if (r.ok) { toast(t('import.ok') + (j.authMethod || '')); onDone() }
      else toast(t('import.err') + (j.error || ''), true)
    } finally { setBusy(false) }
  }
  return (
    <div>
      <p className="text-[var(--muted)] mt-2 mb-0 text-[13px]">{t('import.desc')}</p>
      <label className={cls.label}>{t('import.dbPath')}</label>
      <input className={cls.input} placeholder="~/.local/share/kiro-cli/data.sqlite3" value={path} onChange={(e) => setPath(e.target.value)} />
      <label className={cls.label}>{t('import.arn')}</label>
      <input className={`${cls.input} font-mono`} placeholder="arn:aws:codewhisperer:us-east-1:...:profile/..." value={arn} onChange={(e) => setArn(e.target.value)} />
      <label className={cls.label}>{t('import.region')}</label>
      <input className={cls.input} placeholder="us-east-1" value={reg} onChange={(e) => setReg(e.target.value)} />
      <div className="flex justify-end mt-[22px]">
        <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={submit} disabled={busy}>{t('import.submit')}</button>
      </div>
    </div>
  )
}

// --- Manual token entry (advanced) ---

const AUTH_FIELDS: Record<string, string[]> = {
  idc: ['accessToken', 'refreshToken', 'clientId', 'clientSecret', 'region', 'profileArn'],
  social: ['accessToken', 'refreshToken', 'region', 'profileArn'],
  external_idp: ['accessToken', 'refreshToken', 'clientId', 'tokenEndpoint', 'scopes', 'region', 'profileArn'],
  api_key: ['accessToken', 'region', 'profileArn'],
}
const PH: Record<string, string> = {
  accessToken: 'eyJ... / ksk_...', refreshToken: 'eyJ...',
  profileArn: 'arn:aws:codewhisperer:us-east-1:...:profile/...', region: 'us-east-1',
}

function ManualForm({ t, toast, onDone }: { t: T; toast: ToastFn; onDone: () => void; onClose: () => void }) {
  const [auth, setAuth] = useState('idc')
  const [f, setF] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const set = (k: string, v: string) => setF((s) => ({ ...s, [k]: v }))
  const submit = async () => {
    if (!f.accessToken) { toast(t('add.tokenRequired'), true); return }
    setBusy(true)
    try {
      const acc: Record<string, unknown> = { authMethod: auth, email: f.email || '' }
      AUTH_FIELDS[auth].forEach((k) => { if (f[k]) acc[k] = f[k] })
      const r = await api.addAccount(acc)
      if (r.ok) { toast(t('add.added')); onDone() } else toast(t('add.error'), true)
    } finally { setBusy(false) }
  }
  return (
    <div>
      <label className={cls.label}>{t('add.authMethod')}</label>
      <select className={cls.input} value={auth} onChange={(e) => { setAuth(e.target.value); setF({}) }}>
        <option value="idc">idc (IAM Identity Center / Builder ID)</option>
        <option value="social">social</option>
        <option value="external_idp">external_idp</option>
        <option value="api_key">api_key</option>
      </select>
      <label className={cls.label}>{t('add.emailOpt')}</label>
      <input className={cls.input} placeholder="user@example.com" value={f.email || ''} onChange={(e) => set('email', e.target.value)} />
      {AUTH_FIELDS[auth].map((k) => (
        <div key={k}><label className={cls.label}>{k}</label>
          <input className={cls.input} placeholder={PH[k] || ''} value={f[k] || ''} onChange={(e) => set(k, e.target.value)} /></div>
      ))}
      <div className="flex justify-end mt-[22px]">
        <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={submit} disabled={busy}>{t('acc.add')}</button>
      </div>
    </div>
  )
}
