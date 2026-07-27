import type { T } from '../i18n'
import { fmtNum, fmtUntil } from '../lib/format'
import * as I from '../icons'

/**
 * QuotaMeter renders an account's subscription credit usage: how much of the
 * plan's credit pool is left vs the total, the used percentage, and when the
 * quota next resets. Shared by AccountCard and AccountDetailModal so both stay
 * visually and behaviourally in sync.
 *
 * `unit` is the upstream resource unit (e.g. "CREDIT"); omitted when unknown.
 */
export function QuotaMeter({ limit, current, nextResetUnix, t, unit, size = 'sm' }: {
  limit: number
  current: number
  nextResetUnix?: number
  t: T
  unit?: string
  size?: 'sm' | 'md'
}) {
  const known = limit > 0
  const used = Math.max(0, current)
  const remaining = Math.max(0, limit - used)
  const pct = known ? Math.min(100, Math.round((used / limit) * 100)) : 0

  // Colour escalates as the pool depletes: brand → amber → red.
  const tier = pct >= 85 ? 'hot' : pct >= 70 ? 'warm' : 'ok'
  const fill = tier === 'hot' ? 'var(--danger)' : tier === 'warm' ? 'var(--warn)' : 'var(--brand)'
  const remainingTone = tier === 'hot' ? 'text-[var(--danger)]' : 'text-[var(--text)]'

  const bigNum = size === 'md' ? 'text-[22px]' : 'text-[17px]'
  const unitLabel = unit ? unit.toLowerCase() : t('quota.creditUnit')

  if (!known) {
    return (
      <div className="my-3" role="group" aria-label={t('quota.aria.group')}>
        <div className="flex items-baseline justify-between mb-1.5">
          <span className="text-[11px] font-semibold uppercase tracking-wide text-[var(--muted)]">{t('quota.label')}</span>
          <span className="text-[12px] text-[var(--faint)]">{t('stat.noPoll')}</span>
        </div>
        <div className="h-[7px] rounded-md bg-[var(--panel3)]" aria-hidden="true" />
      </div>
    )
  }

  const valueText = t('quota.aria.value', { remaining: fmtNum(remaining), total: fmtNum(limit), pct: String(pct) })

  return (
    <div className="my-3" role="group" aria-label={t('quota.aria.group')}>
      <div className="flex items-baseline justify-between mb-1">
        <span className="text-[11px] font-semibold uppercase tracking-wide text-[var(--muted)]">{t('quota.label')}</span>
        <span className={`text-[11px] font-semibold tabular-nums ${tier === 'hot' ? 'text-[var(--danger)]' : tier === 'warm' ? 'text-[var(--warn)]' : 'text-[var(--muted)]'}`}>
          {t('quota.usedPct', { pct: String(pct) })}
        </span>
      </div>

      <div className="flex items-baseline gap-1.5 mb-2">
        <span className={`${bigNum} font-bold tabular-nums leading-none ${remainingTone}`}>{fmtNum(remaining)}</span>
        <span className="text-[12px] text-[var(--faint)] tabular-nums">/ {fmtNum(limit)} {unitLabel}</span>
        <span className="text-[11px] text-[var(--muted)] ml-0.5">{t('quota.left')}</span>
      </div>

      <div className="h-[7px] rounded-md bg-[var(--panel3)] overflow-hidden"
        role="progressbar" aria-valuemin={0} aria-valuemax={Math.round(limit)} aria-valuenow={Math.round(used)}
        aria-valuetext={valueText} aria-label={t('quota.aria.group')}>
        <div className="h-full rounded-md transition-[width] duration-300 ease-out" style={{ width: pct + '%', background: fill }} />
      </div>

      <div className="flex items-center justify-between mt-1.5 text-[11.5px] text-[var(--muted)] tabular-nums">
        <span>{t('quota.usedOf', { used: fmtNum(used) })}</span>
        {nextResetUnix ? (
          <span className="inline-flex items-center gap-1" title={t('detail.nextReset')}>
            <I.Refresh /> {t('quota.resetsIn', { in: fmtUntil(nextResetUnix) })}
          </span>
        ) : null}
      </div>
    </div>
  )
}
