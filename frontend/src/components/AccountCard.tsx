import { useState } from 'react'
import type { Account } from '../api'
import type { T } from '../i18n'
import { cls } from '../lib/styles'
import { fmtCompact, fmtTime, mask } from '../lib/format'
import * as I from '../icons'
import { QuotaMeter } from './QuotaMeter'

export function AccountCard({ a, t, privacy, onOpen, onToggle, onTest, onRefreshQuota, onDelete }: {
  a: Account
  t: T
  privacy: boolean
  onOpen: () => void
  onToggle: () => void
  onTest: () => Promise<boolean>
  onRefreshQuota: () => Promise<void>
  onDelete: () => void
}) {
  const [busy, setBusy] = useState<'' | 'test' | 'quota'>('')
  const [status, setStatus] = useState('')
  const rawName = a.email || a.id
  const name = privacy && a.email ? mask(rawName) : rawName
  const initial = (rawName.replace(/[^A-Za-z0-9]/g, '').charAt(0) || '#').toUpperCase()
  const pillBase = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-bold'
  let pill = <span className={`${pillBase} text-[var(--ok)] bg-[rgba(15,118,110,.12)]`}>{t('acc.statusActive')}</span>
  if (!a.enabled) pill = <span className={`${pillBase} text-[var(--muted)] bg-[var(--panel3)]`}>{t('acc.statusDisabled')}</span>
  else if (!a.hasProfileArn) pill = <span className={`${pillBase} text-[var(--warn)] bg-[rgba(255,174,4,.14)]`}>{t('acc.statusNoArn')}</span>
  else if (a.usageLimit > 0 && a.usageCurrent >= a.usageLimit) pill = <span className={`${pillBase} text-[var(--warn)] bg-[rgba(255,174,4,.14)]`}>{t('acc.statusExhausted')}</span>

  const handleTest = async () => {
    setBusy('test'); setStatus(t('acc.testing'))
    try {
      const ok = await onTest()
      setStatus(ok ? t('acc.testOk') : t('acc.testFail'))
    } catch { setStatus(t('acc.testFail')) } finally { setBusy('') }
  }
  const handleRefreshQuota = async () => {
    setBusy('quota'); setStatus(t('acc.refreshingQuota'))
    try { await onRefreshQuota(); setStatus(t('acc.quotaRefreshed')) }
    catch { setStatus(t('acc.quotaFail')) } finally { setBusy('') }
  }

  return (
    <div className={`bg-[var(--panel2)] border border-[var(--border)] rounded-xl p-4 transition-colors hover:border-[var(--border2)] ${a.enabled ? '' : 'opacity-60'}`}>
      <div className="flex items-start gap-3 mb-3.5">
        <div className="w-10 h-10 rounded-[11px] flex-none grid place-items-center font-extrabold bg-[var(--primary)] text-[var(--primary-fg)] text-[15px]" aria-hidden="true">{initial}</div>
        <button type="button" onClick={onOpen}
          className="min-w-0 text-left bg-transparent border-0 p-0 cursor-pointer group" aria-label={t('detail.openFor', { name })}>
          <div className="font-bold text-sm break-all group-hover:text-[var(--brand)] transition-colors">{name}</div>
          <div className="text-[var(--faint)] text-[11px] font-mono break-all">{a.id}</div>
        </button>
        <div className="ml-auto flex items-center gap-2">
          {pill}
          <button type="button" onClick={onOpen} aria-label={t('detail.open')} title={t('detail.open')}
            className={`${cls.btn} ${cls.sm} !px-2`}><I.Info /></button>
        </div>
      </div>
      <div className="flex gap-3.5 flex-wrap text-[12.5px] text-[var(--muted)] mb-1.5">
        <span>{t('acc.auth')} <b className="text-[var(--text)] font-semibold">{a.authMethod}</b></span>
        <span>{t('acc.region')} <b className="text-[var(--text)] font-semibold font-mono">{a.region || '—'}</b></span>
        {a.plan ? <span>{t('acc.plan')} <b className="text-[var(--brand)] font-semibold">{a.plan}</b></span> : null}
      </div>
      <div className="my-3">
        <QuotaMeter limit={a.usageLimit} current={a.usageCurrent} nextResetUnix={a.nextResetUnix} t={t} />
      </div>
      <div className="flex gap-3.5 flex-wrap text-[12.5px] text-[var(--muted)]">
        <span className="inline-flex items-center gap-1" title={t('stat.credits')}><I.Coin /> <b className="text-[var(--text)] font-semibold tabular-nums">{fmtCompact(a.credits)}</b></span>
        <span className="inline-flex items-center gap-1" title={t('stat.requests')}><I.Route /> <b className="text-[var(--text)] font-semibold tabular-nums">{fmtCompact(a.requests)}</b></span>
        <span className="ml-auto" title={t('detail.lastUsed')}>{fmtTime(a.lastUsedUnix)}</span>
      </div>
      <p className="sr-only" aria-live="polite">{status}</p>
      <div className="flex gap-2 mt-3.5 pt-3 border-t border-[var(--border)] flex-wrap">
        <button className={`${cls.btn} ${cls.sm}`} onClick={onToggle}><I.Power /> {a.enabled ? t('common.off') : t('common.on')}</button>
        <button className={`${cls.btn} ${cls.sm}`} onClick={handleTest} disabled={busy !== ''} aria-busy={busy === 'test'}><I.Check /> {busy === 'test' ? t('acc.testing') : t('acc.test')}</button>
        <button className={`${cls.btn} ${cls.sm}`} onClick={handleRefreshQuota} disabled={busy !== ''} aria-busy={busy === 'quota'}><I.Refresh /> {busy === 'quota' ? t('acc.refreshingQuota') : t('acc.refreshQuota')}</button>
        <button className={`${cls.btn} ${cls.btnDanger} ${cls.sm} ml-auto`} onClick={onDelete}><I.Trash /> {t('common.delete')}</button>
      </div>
    </div>
  )
}
