import { useCallback, useEffect, useState } from 'react'
import { api, Unauthorized, type LogEntry } from '../api'
import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import { fmtNum, fmtClock } from '../lib/format'
import * as I from '../icons'

export function LogsTab({ t, toast, onUnauth }: { t: T; toast: ToastFn; onUnauth: () => void }) {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [filter, setFilter] = useState('all')
  const [auto, setAuto] = useState(false)

  const load = useCallback(async () => {
    try { setLogs(await api.logs()) }
    catch (e) { if (e instanceof Unauthorized) onUnauth() }
  }, [onUnauth])
  useEffect(() => { load() }, [load])
  useEffect(() => {
    if (!auto) return
    const iv = setInterval(load, 4000)
    return () => clearInterval(iv)
  }, [auto, load])

  const clear = async () => { await api.clearLogs(); toast(t('logs.cleared')); load() }
  const shown = logs.filter((l) => filter === 'all' || (filter === 'success' ? l.status < 400 : l.status >= 400))

  return (
    <div className={cls.card}>
      <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)] flex-wrap">
        <span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.List /></span> {t('logs.title')}</span>
        <div className="ml-auto flex gap-2.5 items-center flex-wrap">
          <select className={`${cls.input} w-auto`} value={filter} onChange={(e) => setFilter(e.target.value)}>
            <option value="all">{t('logs.filterAll')}</option>
            <option value="success">{t('logs.filterSuccess')}</option>
            <option value="error">{t('logs.filterError')}</option>
          </select>
          <button className={`${cls.btn} ${cls.sm}`} onClick={load}><I.Refresh /> {t('logs.refresh')}</button>
          <label className="flex items-center gap-1.5 text-xs text-[var(--muted)] cursor-pointer select-none">
            <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} /> {t('logs.auto')}
          </label>
          <button className={`${cls.btn} ${cls.btnDanger} ${cls.sm}`} onClick={clear}><I.Trash /> {t('logs.clear')}</button>
        </div>
      </div>
      <div className="p-5">
        {shown.length === 0 ? (
          <div className="text-center py-10 text-[var(--muted)]">
            <div className="text-4xl opacity-40 mb-2.5 flex justify-center" aria-hidden="true"><I.List /></div>
            {t('logs.empty')}
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {shown.map((l, i) => {
              const ok = l.status < 400
              return (
                <div key={i} className="flex items-center gap-3 px-3.5 py-2.5 rounded-xl border border-[var(--border)] bg-[var(--panel2)] text-[12.5px] flex-wrap">
                  <span className={`px-2 py-0.5 rounded-full text-[10.5px] font-bold ${ok ? 'text-[var(--ok)] bg-[rgba(15,118,110,.12)]' : 'text-[var(--danger)] bg-[rgba(229,75,79,.12)]'}`}>{l.status}</span>
                  <span className="font-mono text-[var(--faint)]">{fmtClock(l.timeUnix)}</span>
                  <span className="text-[var(--muted)]">{t('logs.account')} <b className="text-[var(--text)]">{l.account}</b></span>
                  <span className="text-[var(--muted)]">{t('logs.key')} <b className="text-[var(--text)]">{l.apiKey || '—'}</b></span>
                  {ok
                    ? <span className="ml-auto inline-flex items-center gap-1 text-[var(--muted)]"><I.Coin /> <b className="text-[var(--text)]">{fmtNum(l.credits)}</b> {t('logs.credits')}</span>
                    : <span className="ml-auto text-[var(--danger)] break-all">{l.err}</span>}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
