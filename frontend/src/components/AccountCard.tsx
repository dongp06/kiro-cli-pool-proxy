import type { Account } from '../api'
import type { T } from '../i18n'
import { cls } from '../lib/styles'
import { fmtNum, fmtTime, mask } from '../lib/format'
import * as I from '../icons'

export function AccountCard({ a, t, privacy, onToggle, onDelete }: {
  a: Account
  t: T
  privacy: boolean
  onToggle: () => void
  onDelete: () => void
}) {
  const pct = a.usageLimit > 0 ? Math.min(100, Math.round((a.usageCurrent / a.usageLimit) * 100)) : 0
  const hot = pct >= 85
  const rawName = a.email || a.id
  const name = privacy && a.email ? mask(rawName) : rawName
  const initial = (rawName.replace(/[^A-Za-z0-9]/g, '').charAt(0) || '#').toUpperCase()
  const pillBase = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-bold'
  let pill = <span className={`${pillBase} text-[var(--ok)] bg-[rgba(15,118,110,.12)]`}>{t('acc.statusActive')}</span>
  if (!a.enabled) pill = <span className={`${pillBase} text-[var(--muted)] bg-[var(--panel3)]`}>{t('acc.statusDisabled')}</span>
  else if (!a.hasProfileArn) pill = <span className={`${pillBase} text-[var(--warn)] bg-[rgba(255,174,4,.14)]`}>{t('acc.statusNoArn')}</span>
  else if (a.usageLimit > 0 && a.usageCurrent >= a.usageLimit) pill = <span className={`${pillBase} text-[var(--warn)] bg-[rgba(255,174,4,.14)]`}>{t('acc.statusExhausted')}</span>

  return (
    <div className={`bg-[var(--panel2)] border border-[var(--border)] rounded-xl p-4 transition-colors hover:border-[var(--border2)] ${a.enabled ? '' : 'opacity-60'}`}>
      <div className="flex items-start gap-3 mb-3.5">
        <div className="w-10 h-10 rounded-[11px] flex-none grid place-items-center font-extrabold bg-[var(--primary)] text-[var(--primary-fg)] text-[15px]" aria-hidden="true">{initial}</div>
        <div className="min-w-0">
          <div className="font-bold text-sm break-all">{name}</div>
          <div className="text-[var(--faint)] text-[11px] font-mono break-all">{a.id}</div>
        </div>
        <div className="ml-auto">{pill}</div>
      </div>
      <div className="flex gap-3.5 flex-wrap text-[12.5px] text-[var(--muted)] mb-1.5">
        <span>{t('acc.auth')} <b className="text-[var(--text)] font-semibold">{a.authMethod}</b></span>
        <span>{t('acc.region')} <b className="text-[var(--text)] font-semibold font-mono">{a.region || '—'}</b></span>
      </div>
      <div className="my-3">
        <div className="flex justify-between text-xs text-[var(--muted)] mb-1.5">
          <span>{t('acc.quotaLabel')}</span>
          <span>{a.usageLimit > 0 ? `${fmtNum(a.usageCurrent)} / ${fmtNum(a.usageLimit)} · ${pct}%` : t('stat.noPoll')}</span>
        </div>
        <div className="h-[7px] rounded-md bg-[var(--panel3)] overflow-hidden">
          <div className="h-full rounded-md transition-all" style={{ width: pct + '%', background: hot ? 'var(--danger)' : 'var(--brand)' }} />
        </div>
      </div>
      <div className="flex gap-3.5 flex-wrap text-[12.5px] text-[var(--muted)]">
        <span className="inline-flex items-center gap-1"><I.Coin /> <b className="text-[var(--text)] font-semibold">{fmtNum(a.credits)}</b></span>
        <span className="inline-flex items-center gap-1"><I.Route /> <b className="text-[var(--text)] font-semibold">{fmtNum(a.requests)}</b></span>
        <span>{fmtTime(a.lastUsedUnix)}</span>
      </div>
      <div className="flex gap-2 mt-3.5 pt-3 border-t border-[var(--border)]">
        <button className={`${cls.btn} ${cls.sm}`} onClick={onToggle}><I.Power /> {a.enabled ? t('common.off') : t('common.on')}</button>
        <button className={`${cls.btn} ${cls.btnDanger} ${cls.sm} ml-auto`} onClick={onDelete}><I.Trash /> {t('common.delete')}</button>
      </div>
    </div>
  )
}
