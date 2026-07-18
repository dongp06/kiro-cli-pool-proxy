import type { useTheme } from '../hooks/useTheme'
import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import { ApiKeysCard } from '../components/ApiKeysCard'
import * as I from '../icons'

export function SettingsTab({ strategy, onStrategy, listenAddr, theme, t, privacy, refreshSec, onRefresh, toast }: {
  strategy: string
  onStrategy: (s: string) => void
  listenAddr: string
  theme: ReturnType<typeof useTheme>
  t: T
  privacy: boolean
  refreshSec: number
  onRefresh: (v: number) => void
  toast: ToastFn
}) {
  return (
    <>
      <ApiKeysCard t={t} privacy={privacy} toast={toast} />
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
