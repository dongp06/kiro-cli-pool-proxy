import { useCallback, useEffect, useState } from 'react'
import { api, Unauthorized, type Account, type Overview } from '../api'
import type { Ctx, ToastFn } from '../context'
import { usePrivacy } from '../hooks/usePrivacy'
import { cls } from '../lib/styles'
import { fmtNum } from '../lib/format'
import * as I from '../icons'
import { LangSwitch } from './LangSwitch'
import { ThemeBtn } from './ThemeBtn'
import { Stat } from './Stat'
import { AccountCard } from './AccountCard'
import { AddModal } from './AddModal'
import { ImportModal } from './ImportModal'
import { ConnectTab } from '../tabs/ConnectTab'
import { SettingsTab } from '../tabs/SettingsTab'
import { LogsTab } from '../tabs/LogsTab'

type Tab = 'accounts' | 'connect' | 'settings' | 'logs'

export function Dashboard(props: {
  authRequired: boolean
  ctx: Ctx
  onLogout: () => void
  onUnauth: () => void
  toast: ToastFn
}) {
  const { theme, lang } = props.ctx
  const { t } = lang
  const privacy = usePrivacy()
  const [tab, setTab] = useState<Tab>('accounts')
  const [ov, setOv] = useState<Overview | null>(null)
  const [accs, setAccs] = useState<Account[]>([])
  const [listenAddr, setListenAddr] = useState('')
  const [refreshMs, setRefreshMs] = useState<number>(() => {
    const v = localStorage.getItem('kpp_refresh')
    return v !== null ? +v * 1000 : 8000
  })
  const [addOpen, setAddOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const region = accs[0]?.region || 'us-east-1'

  const refresh = useCallback(async () => {
    try {
      const [o, a, s] = await Promise.all([api.overview(), api.accounts(), api.settings()])
      setOv(o); setAccs(a); setListenAddr(s.listenAddr)
    } catch (e) {
      if (e instanceof Unauthorized) props.onUnauth()
    }
  }, [props])

  useEffect(() => { refresh() }, [refresh])
  useEffect(() => {
    if (refreshMs <= 0) return
    const iv = setInterval(refresh, refreshMs)
    return () => clearInterval(iv)
  }, [refresh, refreshMs])

  const logout = async () => { await api.logout().catch(() => {}); props.onLogout() }
  const setStrategy = async (s: string) => { await api.setStrategy(s); props.toast(t('set.saved')); refresh() }

  const qpct = ov && ov.quotaLimit > 0 ? Math.round((ov.quotaUsed / ov.quotaLimit) * 100) : 0
  const url = location.protocol + '//' + location.host

  const filtered = accs.filter((a) => {
    const q = search.trim().toLowerCase()
    if (q && !(a.id.toLowerCase().includes(q) || (a.email || '').toLowerCase().includes(q))) return false
    const exhausted = a.usageLimit > 0 && a.usageCurrent >= a.usageLimit
    if (statusFilter === 'enabled' && !a.enabled) return false
    if (statusFilter === 'disabled' && a.enabled) return false
    if (statusFilter === 'exhausted' && !exhausted) return false
    return true
  })

  return (
    <div className="min-h-full flex flex-col">
      <header className="sticky top-0 z-20 border-b border-[var(--border)] bg-[var(--bg)]">
        <div className="max-w-[1180px] mx-auto px-6 py-3.5 flex items-center gap-5 flex-wrap">
          <div className="flex items-center gap-3 font-extrabold text-[17px]">
            <span className="w-8 h-8 rounded-[9px] grid place-items-center bg-[var(--primary)] text-[var(--primary-fg)] text-lg"><I.Logo /></span>
            KiroPool
          </div>
          <nav className="flex gap-1 rounded-xl p-1 bg-[var(--panel3)] border border-[var(--border)]">
            {([['accounts', t('nav.accounts'), I.Users], ['connect', t('nav.connect'), I.Plug], ['settings', t('nav.settings'), I.Gear], ['logs', t('nav.logs'), I.List]] as const).map(([id, label, Ico]) => (
              <button key={id} onClick={() => setTab(id as Tab)}
                aria-current={tab === id ? 'page' : undefined}
                className={`inline-flex items-center gap-2 rounded-[9px] px-3.5 py-2 text-[13px] font-semibold cursor-pointer transition-colors ${tab === id ? 'bg-[var(--panel)] text-[var(--text)]' : 'text-[var(--muted)] hover:text-[var(--text)]'}`}>
                <span className="text-[15px]" aria-hidden="true"><Ico /></span><span>{label}</span>
              </button>
            ))}
          </nav>
          <div className="flex items-center gap-2.5 ml-auto">
            <LangSwitch lang={lang} />
            <ThemeBtn theme={theme} t={t} />
            {props.authRequired ? (
              <button className={`${cls.btn} ${cls.btnGhost} ${cls.sm}`} onClick={logout}><I.Power /> {t('common.logout')}</button>
            ) : (
              <span className="text-xs text-[var(--muted)]">{t('common.authOff')}</span>
            )}
          </div>
        </div>
      </header>

      <div className="max-w-[1180px] w-full mx-auto px-6 pt-6 pb-14 flex-1">
        <div className="grid gap-4 mb-6" style={{ gridTemplateColumns: 'repeat(auto-fit,minmax(210px,1fr))' }}>
          <Stat kind="a" icon={<I.Users />} title={t('stat.accounts')} value={ov?.totalAccounts ?? 0}
            sub={t('stat.accountsSub', { a: ov?.enabled ?? 0, b: ov?.available ?? 0 })} />
          <Stat kind="b" icon={<I.Route />} title={t('stat.requests')} value={fmtNum(ov?.totalRequests ?? 0)} sub={t('stat.requestsSub')} />
          <Stat kind="c" icon={<I.Coin />} title={t('stat.credits')} value={fmtNum(ov?.totalCredits ?? 0)} sub={t('stat.creditsSub')} />
          <Stat kind="d" icon={<I.Gauge />} title={t('stat.quota')} value={ov && ov.quotaLimit > 0 ? qpct + '%' : '—'}
            sub={ov && ov.quotaLimit > 0 ? t('stat.quotaSub', { used: fmtNum(ov.quotaUsed), limit: fmtNum(ov.quotaLimit) }) : t('stat.noPoll')} />
        </div>

        {tab === 'accounts' && (
          <div className={cls.card}>
            <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)] flex-wrap">
              <span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Users /></span> {t('acc.title')}</span>
              <div className="ml-auto flex gap-2.5 items-center flex-wrap">
                <button className="flex items-center gap-1.5 text-xs text-[var(--muted)] cursor-pointer select-none" title={t('privacy.tooltip')}
                  onClick={privacy.toggle} aria-pressed={privacy.on}>
                  {privacy.on ? <I.EyeOff /> : <I.Eye />} {t('privacy.label')}
                </button>
                <select className={`${cls.input} w-auto`} value={ov?.strategy || 'smart'} onChange={(e) => setStrategy(e.target.value)}>
                  <option value="round-robin">{t('strategy.rr')}</option>
                  <option value="smart">{t('strategy.smart')}</option>
                </select>
                <button className={`${cls.btn} ${cls.sm}`} onClick={() => setImportOpen(true)}><I.Download /> {t('acc.import')}</button>
                <button className={`${cls.btn} ${cls.btnPrimary} ${cls.sm}`} onClick={() => setAddOpen(true)}><I.Plus /> {t('acc.add')}</button>
              </div>
            </div>
            <div className="px-5 pt-4 flex gap-2.5 flex-wrap items-center">
              <div className="relative flex-1 min-w-[200px]">
                <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--faint)]" aria-hidden="true"><I.Search /></span>
                <input className={`${cls.input} pl-9`} placeholder={t('acc.search')} aria-label={t('acc.search')} value={search} onChange={(e) => setSearch(e.target.value)} />
              </div>
              <select className={`${cls.input} w-auto`} value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
                <option value="all">{t('acc.filterAll')}</option>
                <option value="enabled">{t('acc.filterEnabled')}</option>
                <option value="disabled">{t('acc.filterDisabled')}</option>
                <option value="exhausted">{t('acc.filterExhausted')}</option>
              </select>
            </div>
            <div className="p-5">
              {accs.length === 0 ? (
                <div className="text-center py-10 text-[var(--muted)]">
                  <div className="text-4xl opacity-40 mb-2.5 flex justify-center" aria-hidden="true"><I.Users /></div>
                  {t('acc.empty')}<br />{t('acc.emptyHint')}
                </div>
              ) : filtered.length === 0 ? (
                <div className="text-center py-10 text-[var(--muted)]">{t('acc.noMatch')}</div>
              ) : (
                <div className="grid gap-3.5" style={{ gridTemplateColumns: 'repeat(auto-fill,minmax(340px,1fr))' }}>
                  {filtered.map((a) => (
                    <AccountCard key={a.id} a={a} t={t} privacy={privacy.on}
                      onToggle={async () => { await api.toggleAccount(a.id, !a.enabled); props.toast(a.enabled ? t('toast.accOff') : t('toast.accOn')); refresh() }}
                      onDelete={async () => { if (confirm(t('acc.confirmDelete', { id: a.id }))) { await api.deleteAccount(a.id); props.toast(t('toast.accDeleted')); refresh() } }} />
                  ))}
                </div>
              )}
            </div>
          </div>
        )}

        {tab === 'connect' && <ConnectTab url={url} region={region} t={t} toast={props.toast} />}

        {tab === 'settings' && (
          <SettingsTab
            strategy={ov?.strategy || 'smart'} onStrategy={setStrategy}
            listenAddr={listenAddr} theme={theme} t={t} privacy={privacy.on} toast={props.toast}
            refreshSec={refreshMs / 1000}
            onRefresh={(v) => { setRefreshMs(v * 1000); localStorage.setItem('kpp_refresh', String(v)) }} />
        )}

        {tab === 'logs' && <LogsTab t={t} toast={props.toast} onUnauth={props.onUnauth} />}
      </div>

      <footer className="border-t border-[var(--border)] mt-5">
        <div className="max-w-[1180px] mx-auto px-6 py-5 flex items-center gap-3.5 text-[12.5px] text-[var(--muted)] flex-wrap">
          <span className="flex items-center gap-2 font-extrabold text-sm text-[var(--text)]">
            <span className="w-6 h-6 rounded-md grid place-items-center bg-[var(--primary)] text-[var(--primary-fg)]"><I.Logo /></span> KiroPool
          </span>
          <span className="w-px h-5 bg-[var(--border)]" />
          <span><span className="inline-block w-2 h-2 rounded-full bg-[var(--ok)] mr-1.5" style={{ animation: 'pulse 2s infinite' }} />{t('footer.running')}</span>
          <span className="ml-auto">{t('footer.tagline')}</span>
        </div>
      </footer>

      {addOpen && <AddModal t={t} onClose={() => setAddOpen(false)} onDone={() => { setAddOpen(false); refresh() }} toast={props.toast} />}
      {importOpen && <ImportModal t={t} region={region} onClose={() => setImportOpen(false)} onDone={() => { setImportOpen(false); refresh() }} toast={props.toast} />}
    </div>
  )
}
