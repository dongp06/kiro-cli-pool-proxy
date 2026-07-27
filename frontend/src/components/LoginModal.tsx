import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import * as I from '../icons'

/** Live status line announced to screen readers. */
function Status({ msg, err }: { msg: string; err?: boolean }) {
  if (!msg) return null
  return (
    <p role="status" aria-live="polite"
      className={`mt-3 mb-0 text-[13px] ${err ? 'text-[var(--danger)]' : 'text-[var(--muted)]'}`}>
      {msg}
    </p>
  )
}

/** Reusable "open the authorization page + paste the callback URL" step. */
function AuthStep({ t, toast, url, placeholder, hint, value, onChange }: {
  t: T; toast: ToastFn; url: string; placeholder: string; hint: string
  value: string; onChange: (v: string) => void
}) {
  return (
    <div className="mt-4">
      <a className={`${cls.btn} ${cls.btnPrimary}`} href={url} target="_blank" rel="noopener noreferrer">
        <I.LogIn /> {t('login2.openAuth')}
      </a>
      <button type="button" className={`${cls.btn} ${cls.sm} ml-2`} aria-label={t('login2.copyLink')}
        onClick={() => { navigator.clipboard.writeText(url); toast(t('connect.copied')) }}>
        <I.Copy /> {t('login2.copyLink')}
      </button>
      <p className="text-[12.5px] text-[var(--muted)] mt-2 mb-0">{t('login2.authHint')}</p>
      <label className={cls.label}>{t('login2.callbackUrl')}</label>
      <input className={`${cls.input} font-mono`} placeholder={placeholder}
        value={value} onChange={(e) => onChange(e.target.value)} autoComplete="off" spellCheck={false} />
      <p className="text-[12.5px] text-[var(--muted)] mt-1.5 mb-0">{hint}</p>
    </div>
  )
}

// --- IAM Identity Center ---

export function IamFlow({ t, toast, onDone }: { t: T; toast: ToastFn; onDone: () => void }) {
  const [startUrl, setStartUrl] = useState('')
  const [region, setRegion] = useState('us-east-1')
  const [sessionId, setSessionId] = useState('')
  const [authorizeUrl, setAuthorizeUrl] = useState('')
  const [cb, setCb] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState(''); const [err, setErr] = useState(false)

  const start = async () => {
    if (!startUrl.trim()) { setErr(true); setStatus(t('login2.startUrlRequired')); return }
    setBusy(true); setErr(false); setStatus(t('login2.starting'))
    const { ok, data } = await api.iamSsoStart(startUrl.trim(), region.trim())
    setBusy(false)
    if (!ok) { setErr(true); setStatus(data.error || t('login2.failed')); return }
    setSessionId(data.sessionId); setAuthorizeUrl(data.authorizeUrl); setStatus('')
  }
  const complete = async () => {
    if (!cb.trim()) { setErr(true); setStatus(t('login2.callbackRequired')); return }
    setBusy(true); setErr(false); setStatus(t('login2.completing'))
    const { ok, data } = await api.iamSsoComplete(sessionId, cb.trim())
    setBusy(false)
    if (!ok || !data.success) { setErr(true); setStatus(data.error || t('login2.failed')); return }
    toast(t('login2.added', { email: data.account?.email || data.account?.id || '' }))
    onDone()
  }

  return (
    <div>
      {!authorizeUrl ? (
        <>
          <label className={cls.label}>{t('login2.startUrl')}</label>
          <input className={cls.input} placeholder="https://my-org.awsapps.com/start"
            value={startUrl} onChange={(e) => setStartUrl(e.target.value)} />
          <label className={cls.label}>{t('login2.region')}</label>
          <input className={cls.input} placeholder="us-east-1" value={region} onChange={(e) => setRegion(e.target.value)} />
          <div className="flex justify-end mt-[22px]">
            <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={start} disabled={busy}>{t('login2.start')}</button>
          </div>
        </>
      ) : (
        <>
          <AuthStep t={t} toast={toast} url={authorizeUrl} value={cb} onChange={setCb}
            placeholder="http://127.0.0.1/oauth/callback?code=..." hint={t('login2.callbackHint')} />
          <div className="flex justify-end mt-[22px]">
            <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={complete} disabled={busy}>{t('login2.complete')}</button>
          </div>
        </>
      )}
      <Status msg={status} err={err} />
    </div>
  )
}

// --- AWS Builder ID (device code) ---

export function BuilderIdFlow({ t, toast, onDone }: { t: T; toast: ToastFn; onDone: () => void }) {
  const [region, setRegion] = useState('us-east-1')
  const [session, setSession] = useState<{ id: string; userCode: string; uri: string; interval: number } | null>(null)
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState(''); const [err, setErr] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const stopped = useRef(false)

  useEffect(() => () => { stopped.current = true; if (timer.current) clearTimeout(timer.current) }, [])

  const poll = useCallback(async (id: string, interval: number) => {
    if (stopped.current) return
    const { ok, data } = await api.builderIdPoll(id)
    if (stopped.current) return
    if (!ok) { setErr(true); setStatus(data.error || t('login2.failed')); return }
    if (data.completed) {
      toast(t('login2.added', { email: data.account?.email || data.account?.id || '' }))
      onDone(); return
    }
    const next = (data.interval || interval) * 1000
    timer.current = setTimeout(() => poll(id, data.interval || interval), next)
  }, [t, toast, onDone])

  const start = async () => {
    setBusy(true); setErr(false); setStatus(t('login2.starting'))
    const { ok, data } = await api.builderIdStart(region.trim())
    setBusy(false)
    if (!ok) { setErr(true); setStatus(data.error || t('login2.failed')); return }
    setSession({ id: data.sessionId, userCode: data.userCode, uri: data.verificationUri, interval: data.interval || 5 })
    setStatus(t('login2.waiting'))
    timer.current = setTimeout(() => poll(data.sessionId, data.interval || 5), (data.interval || 5) * 1000)
  }

  return (
    <div>
      {!session ? (
        <>
          <label className={cls.label}>{t('login2.region')}</label>
          <input className={cls.input} placeholder="us-east-1" value={region} onChange={(e) => setRegion(e.target.value)} />
          <div className="flex justify-end mt-[22px]">
            <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={start} disabled={busy}>{t('login2.start')}</button>
          </div>
        </>
      ) : (
        <div className="mt-4">
          <label className={cls.label}>{t('login2.deviceCode')}</label>
          <div className="flex items-center gap-2">
            <code className="rounded-[var(--radius)] px-3 py-2 text-base font-mono font-bold tracking-widest bg-[var(--bg2)] border border-[var(--border)] text-[var(--brand)]">{session.userCode}</code>
            <button type="button" className={`${cls.btn} ${cls.sm}`} aria-label={t('common.copy')}
              onClick={() => { navigator.clipboard.writeText(session.userCode); toast(t('connect.copied')) }}><I.Copy /></button>
          </div>
          <a className={`${cls.btn} ${cls.btnPrimary} mt-3`} href={session.uri} target="_blank" rel="noopener noreferrer">
            <I.LogIn /> {t('login2.openAuth')}
          </a>
          <p className="text-[12.5px] text-[var(--muted)] mt-2 mb-0">{t('login2.deviceHint')}</p>
        </div>
      )}
      <Status msg={status} err={err} />
    </div>
  )
}

// --- SSO Token import ---

export function TokenFlow({ t, toast, onDone }: { t: T; toast: ToastFn; onDone: () => void }) {
  const [tokens, setTokens] = useState('')
  const [region, setRegion] = useState('us-east-1')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState(''); const [err, setErr] = useState(false)

  const submit = async () => {
    if (!tokens.trim()) { setErr(true); setStatus(t('login2.tokensRequired')); return }
    setBusy(true); setErr(false); setStatus(t('login2.importing'))
    const { ok, data } = await api.importSsoToken(tokens, region.trim())
    setBusy(false)
    if (!ok) { setErr(true); setStatus(data.error || t('login2.failed')); return }
    const n = data.accounts?.length || 0
    const fails = data.errors?.length || 0
    if (n > 0) toast(t('login2.importedN', { n }))
    if (fails > 0) { setErr(true); setStatus(t('login2.errorsN', { n: fails }) + ': ' + data.errors.join('; ')) }
    if (n > 0) { onDone(); return }
    if (fails === 0) { setErr(true); setStatus(t('login2.failed')) }
  }

  return (
    <div>
      <label className={cls.label}>{t('login2.tokens')}</label>
      <textarea className={`${cls.input} font-mono min-h-[120px] resize-y`} placeholder="eyJ..."
        value={tokens} onChange={(e) => setTokens(e.target.value)} spellCheck={false} />
      <p className="text-[12.5px] text-[var(--muted)] mt-1.5 mb-0">{t('login2.tokensHint')}</p>
      <label className={cls.label}>{t('login2.region')}</label>
      <input className={cls.input} placeholder="us-east-1" value={region} onChange={(e) => setRegion(e.target.value)} />
      <div className="flex justify-end mt-[22px]">
        <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={submit} disabled={busy}>{t('login2.import')}</button>
      </div>
      <Status msg={status} err={err} />
    </div>
  )
}

// --- Kiro API key (ksk_) import ---

export function ApiKeyFlow({ t, toast, onDone }: { t: T; toast: ToastFn; onDone: () => void }) {
  const [apiKey, setApiKey] = useState('')
  const [region, setRegion] = useState('auto')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState(''); const [err, setErr] = useState(false)

  const submit = async () => {
    if (!apiKey.trim()) { setErr(true); setStatus(t('login2.apiKeyRequired')); return }
    setBusy(true); setErr(false); setStatus(t('login2.probing'))
    const { ok, data } = await api.importApiKey(apiKey.trim(), region.trim())
    setBusy(false)
    if (!ok || !data.success) { setErr(true); setStatus(data.error || t('login2.failed')); return }
    toast(t('login2.added', { email: data.account?.email || data.account?.id || '' }))
    onDone()
  }

  return (
    <div>
      <label className={cls.label}>{t('login2.apiKey')}</label>
      <textarea className={`${cls.input} font-mono min-h-[80px] resize-y`} placeholder="ksk_..."
        value={apiKey} onChange={(e) => setApiKey(e.target.value)} spellCheck={false} autoComplete="off" />
      <p className="text-[12.5px] text-[var(--muted)] mt-1.5 mb-0">{t('login2.apiKeyHint')}</p>
      <label className={cls.label}>{t('login2.region')}</label>
      <input className={cls.input} placeholder="auto" value={region} onChange={(e) => setRegion(e.target.value)} />
      <p className="text-[12.5px] text-[var(--muted)] mt-1.5 mb-0">{t('login2.apiKeyRegionHint')}</p>
      <div className="flex justify-end mt-[22px]">
        <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={submit} disabled={busy}>{t('login2.import')}</button>
      </div>
      <Status msg={status} err={err} />
    </div>
  )
}

// --- Microsoft Enterprise SSO (two-leg callback) ---
export function MicrosoftFlow({ t, toast, onDone }: { t: T; toast: ToastFn; onDone: () => void }) {
  const [sessionId, setSessionId] = useState('')
  const [authorizeUrl, setAuthorizeUrl] = useState('')
  const [leg, setLeg] = useState<1 | 2>(1)
  const [cb, setCb] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState(''); const [err, setErr] = useState(false)
  const active = useRef('')

  useEffect(() => () => { if (active.current) api.microsoftSsoCancel(active.current) }, [])

  const start = async () => {
    setBusy(true); setErr(false); setStatus(t('login2.starting'))
    const { ok, data } = await api.microsoftSsoStart()
    setBusy(false)
    if (!ok) { setErr(true); setStatus(data.error || t('login2.failed')); return }
    setSessionId(data.sessionId); active.current = data.sessionId
    setAuthorizeUrl(data.authorizeUrl); setStatus('')
  }
  const complete = async () => {
    if (!cb.trim()) { setErr(true); setStatus(t('login2.callbackRequired')); return }
    setBusy(true); setErr(false); setStatus(t('login2.completing'))
    const { ok, data } = await api.microsoftSsoComplete(sessionId, cb.trim())
    setBusy(false)
    if (!ok || !data.success) { setErr(true); setStatus(data.error || t('login2.failed')); return }
    if (data.stage === 'microsoft' && data.authorizeUrl) {
      setAuthorizeUrl(data.authorizeUrl); setLeg(2); setCb(''); setStatus('')
      return
    }
    active.current = ''
    toast(t('login2.added', { email: data.account?.email || data.account?.id || '' }))
    onDone()
  }

  return (
    <div>
      {!authorizeUrl ? (
        <>
          <p className="text-[var(--muted)] text-[13px] mt-3 mb-0">{t('login2.msIntro')}</p>
          <div className="flex justify-end mt-[22px]">
            <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={start} disabled={busy}>{t('login2.start')}</button>
          </div>
        </>
      ) : (
        <>
          <p className="text-[13px] font-semibold mt-3 mb-0">{leg === 1 ? t('login2.msStep1') : t('login2.msStep2')}</p>
          <AuthStep t={t} toast={toast} url={authorizeUrl} value={cb} onChange={setCb}
            placeholder="http://localhost:3128/..."
            hint={leg === 1 ? t('login2.msHint1') : t('login2.msHint2')} />
          <div className="flex justify-end mt-[22px]">
            <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={complete} disabled={busy}>
              {leg === 1 ? t('login2.next') : t('login2.complete')}
            </button>
          </div>
        </>
      )}
      <Status msg={status} err={err} />
    </div>
  )
}
