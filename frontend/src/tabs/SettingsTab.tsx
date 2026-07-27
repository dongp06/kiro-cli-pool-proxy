import { useState } from 'react'
import type { useTheme } from '../hooks/useTheme'
import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import { ApiKeysCard } from '../components/ApiKeysCard'
import { api } from '../api'
import * as I from '../icons'

export function SettingsTab({ strategy, onStrategy, listenAddr, theme, t, privacy, refreshSec, onRefresh, toast, hasPassword, passwordEnv, onPasswordSaved }: {
  strategy: string
  onStrategy: (s: string) => void
  listenAddr: string
  theme: ReturnType<typeof useTheme>
  t: T
  privacy: boolean
  refreshSec: number
  onRefresh: (v: number) => void
  toast: ToastFn
  hasPassword: boolean
  passwordEnv: boolean
  onPasswordSaved: () => void
}) {
  return (
    <>
      <ApiKeysCard t={t} privacy={privacy} toast={toast} />
      <SecurityCard t={t} toast={toast} hasPassword={hasPassword} passwordEnv={passwordEnv} onSaved={onPasswordSaved} />
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Gear /></span> {t('set.strategyTitle')}</span></div>
        <div className="p-5">
          <label className={cls.label}>{t('set.strategy')}</label>
          <select className={cls.input} value={strategy} onChange={(e) => onStrategy(e.target.value)}>
            <option value="round-robin">{t('set.strategyRR')}</option>
            <option value="smart">{t('set.strategySmart')}</option>
          </select>
          <small className="text-[var(--faint)] text-[11.5px] block mt-1.5">{t('set.strategyHint')}</small>
        </div>
      </div>
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Info /></span> {t('set.proxyInfo')}</span></div>
        <div className="p-5">
          <div className="grid grid-cols-2 gap-x-3.5">
            <div><label className={cls.label}>{t('set.listenAddr')}</label><input className={`${cls.input} font-mono`} value={listenAddr} readOnly /></div>
            <div><label className={cls.label}>{t('set.theme')}</label>
              <select className={cls.input} value={theme.theme} onChange={(e) => theme.setTheme(e.target.value)}>
                <option value="dark">{t('set.themeDark')}</option><option value="light">{t('set.themeLight')}</option>
              </select>
            </div>
          </div>
          <label className={cls.label}>{t('set.autoRefresh')}</label>
          <select className={cls.input} value={refreshSec} onChange={(e) => onRefresh(+e.target.value)}>
            <option value={5}>5s</option><option value={8}>8s</option><option value={15}>15s</option><option value={0}>{t('set.off')}</option>
          </select>
        </div>
      </div>
    </>
  )
}

function SecurityCard({ t, toast, hasPassword, passwordEnv, onSaved }: {
  t: T
  toast: ToastFn
  hasPassword: boolean
  passwordEnv: boolean
  onSaved: () => void
}) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(false)

  const status = passwordEnv ? 'env' : hasPassword ? 'set' : 'unset'
  const statusText = status === 'env' ? t('set.pwStatusEnv') : status === 'set' ? t('set.pwStatusSet') : t('set.pwStatusUnset')
  const statusColor = status === 'unset' ? 'text-[var(--danger,#ef4444)]' : 'text-[var(--muted)]'

  const save = async () => {
    if (next && next.length < 8) { toast(t('set.pwTooShort')); return }
    setBusy(true)
    try {
      const r = await api.setPassword(current, next)
      if (r.ok) {
        toast(next ? t('set.pwSaved') : t('set.pwCleared'))
        setCurrent(''); setNext('')
        onSaved()
      } else {
        const err = r.data?.error || ''
        if (err.includes('current password')) toast(t('set.pwWrongCurrent'))
        else if (err.includes('8 characters')) toast(t('set.pwTooShort'))
        else toast(t('set.pwError'))
      }
    } catch {
      toast(t('set.pwError'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={cls.card}>
      <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]">
        <span className="font-bold text-[15px] flex items-center gap-2.5">
          <span className="text-[var(--brand)] text-[17px]"><I.Shield /></span> {t('set.security')}
        </span>
      </div>
      <div className="p-5">
        <p className={`text-[12.5px] mb-4 flex items-center gap-2 ${statusColor}`} role="status" aria-live="polite">
          <span className="text-[15px]"><I.Lock /></span> {statusText}
        </p>
        {!passwordEnv && (
          <>
            {hasPassword && (
              <div className="mb-3">
                <label className={cls.label} htmlFor="pw-current">{t('set.pwCurrent')}</label>
                <input id="pw-current" className={cls.input} type="password" autoComplete="current-password"
                  value={current} onChange={(e) => setCurrent(e.target.value)} />
              </div>
            )}
            <label className={cls.label} htmlFor="pw-new">{t('set.pwNew')}</label>
            <input id="pw-new" className={cls.input} type="password" autoComplete="new-password"
              placeholder={t('set.pwNewPlaceholder')} value={next} onChange={(e) => setNext(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && !busy) save() }} />
            <button className={`${cls.btn} ${cls.btnPrimary} mt-3.5`} onClick={save} disabled={busy}>{t('set.pwSave')}</button>
          </>
        )}
      </div>
    </div>
  )
}
