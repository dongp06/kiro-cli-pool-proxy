import { useState } from 'react'
import { api, type Account, type KiroModel } from '../api'
import type { T } from '../i18n'
import { cls } from '../lib/styles'
import { fmtNum, fmtTime, fmtClock, mask } from '../lib/format'
import * as I from '../icons'
import { Modal } from './Modal'
import { QuotaMeter } from './QuotaMeter'

export function AccountDetailModal({ a, t, privacy, models, onClose, onToggle, onTest, onRefreshQuota, onDelete }: {
  a: Account
  t: T
  privacy: boolean
  models: KiroModel[]
  onClose: () => void
  onToggle: () => void
  onTest: () => Promise<boolean>
  onRefreshQuota: () => Promise<void>
  onDelete: () => void
}) {
  const [model, setModel] = useState('auto')
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; reply?: string; error?: string; latencyMs?: number } | null>(null)
  const [busy, setBusy] = useState<'' | 'toggle' | 'quota' | 'test'>('')

  const rawName = a.email || a.id
  const name = privacy && a.email ? mask(rawName) : rawName

  const runTestModel = async () => {
    setTesting(true); setResult(null)
    try {
      const r = await api.testModel(a.id, model)
      setResult(r)
    } catch {
      setResult({ ok: false, error: t('detail.testModelErr') })
    } finally { setTesting(false) }
  }
  const handleTest = async () => {
    setBusy('test')
    try {
      await onTest()
    } finally { setBusy('') }
  }
  const handleQuota = async () => {
    setBusy('quota')
    try { await onRefreshQuota() } finally { setBusy('') }
  }

  const rows: [string, React.ReactNode][] = [
    [t('acc.auth'), <b className="font-mono">{a.authMethod}</b>],
    [t('acc.region'), <b className="font-mono">{a.region || '—'}</b>],
    [t('detail.profileArn'), a.hasProfileArn ? <span className="text-[var(--ok)]">{t('detail.present')}</span> : <span className="text-[var(--warn)]">{t('detail.missing')}</span>],
    [t('detail.tokenExpires'), <span className="font-mono">{a.tokenExpires ? fmtClock(a.tokenExpires) : '—'}</span>],
    [t('stat.credits'), <b>{fmtNum(a.credits)}</b>],
    [t('stat.requests'), <b>{fmtNum(a.requests)}</b>],
    [t('detail.lastUsed'), <span>{fmtTime(a.lastUsedUnix)}</span>],
    [t('detail.nextReset'), <span className="font-mono">{a.nextResetUnix ? fmtClock(a.nextResetUnix) : '—'}</span>],
  ]

  return (
    <Modal title={name} onClose={onClose}>
      <div className="text-[var(--faint)] text-[11.5px] font-mono break-all -mt-1 mb-3">{a.id}</div>

      <div className="my-2">
        <QuotaMeter limit={a.usageLimit} current={a.usageCurrent} nextResetUnix={a.nextResetUnix} t={t} size="md" />
      </div>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 my-4 text-[12.5px]">
        {rows.map(([k, v], i) => (
          <div key={i} className="flex justify-between gap-2 border-b border-[var(--border)] pb-1.5">
            <dt className="text-[var(--muted)]">{k}</dt>
            <dd className="m-0 text-[var(--text)] text-right break-all">{v}</dd>
          </div>
        ))}
      </dl>

      <div className="rounded-xl border border-[var(--border)] bg-[var(--panel2)] p-3.5 mt-4">
        <div className="font-semibold text-[13.5px] flex items-center gap-2 mb-2"><span className="text-[var(--accent)]"><I.Chat /></span> {t('detail.testModelTitle')}</div>
        <p className="text-[var(--faint)] text-[11.5px] mt-0 mb-2.5">{t('detail.testModelDesc')}</p>
        <div className="flex gap-2.5 flex-wrap items-end">
          <div className="flex-1 min-w-[180px]">
            <label className={cls.label} id={`tm-${a.id}`}>{t('detail.model')}</label>
            <select className={cls.input} aria-labelledby={`tm-${a.id}`} value={model} onChange={(e) => setModel(e.target.value)}>
              <option value="auto">auto</option>
              {models.map((m) => <option key={m.modelId} value={m.modelId}>{m.modelName || m.modelId}</option>)}
            </select>
          </div>
          <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={runTestModel} disabled={testing} aria-busy={testing}>
            <I.Check /> {testing ? t('detail.testingModel') : t('detail.runTest')}
          </button>
        </div>
        <p className="sr-only" aria-live="polite">{testing ? t('detail.testingModel') : result ? (result.ok ? t('detail.testModelOk') : t('detail.testModelFail')) : ''}</p>
        {result && (
          <div className={`mt-3 rounded-lg px-3 py-2.5 text-[12.5px] border ${result.ok ? 'border-[var(--border)] bg-[var(--bg2)]' : 'border-transparent bg-[rgba(229,75,79,.1)]'}`}>
            {result.ok ? (
              <>
                <div className="flex items-center gap-2 text-[var(--ok)] font-semibold"><I.Check /> {t('detail.testModelOk')} {result.latencyMs != null && <span className="text-[var(--faint)] font-normal">· {result.latencyMs}ms</span>}</div>
                {result.reply && <div className="mt-1.5 font-mono break-all text-[var(--text)]">{result.reply}</div>}
              </>
            ) : (
              <div className="text-[var(--danger)] break-all">{result.error || t('detail.testModelFail')}</div>
            )}
          </div>
        )}
      </div>

      <div className="flex gap-2 mt-5 pt-3.5 border-t border-[var(--border)] flex-wrap">
        <button className={`${cls.btn} ${cls.sm}`} onClick={() => { setBusy('toggle'); onToggle(); setBusy('') }}><I.Power /> {a.enabled ? t('common.off') : t('common.on')}</button>
        <button className={`${cls.btn} ${cls.sm}`} onClick={handleTest} disabled={busy !== ''} aria-busy={busy === 'test'}><I.Check /> {busy === 'test' ? t('acc.testing') : t('acc.test')}</button>
        <button className={`${cls.btn} ${cls.sm}`} onClick={handleQuota} disabled={busy !== ''} aria-busy={busy === 'quota'}><I.Refresh /> {busy === 'quota' ? t('acc.refreshingQuota') : t('acc.refreshQuota')}</button>
        <button className={`${cls.btn} ${cls.btnDanger} ${cls.sm} ml-auto`} onClick={onDelete}><I.Trash /> {t('common.delete')}</button>
      </div>
    </Modal>
  )
}
