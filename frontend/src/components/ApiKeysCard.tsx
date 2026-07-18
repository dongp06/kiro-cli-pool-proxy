import { useCallback, useEffect, useState } from 'react'
import { api, type ApiKey } from '../api'
import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import { fmtNum, fmtTime, mask } from '../lib/format'
import * as I from '../icons'

export function ApiKeysCard({ t, privacy, toast }: { t: T; privacy: boolean; toast: ToastFn }) {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [name, setName] = useState('')
  const [limit, setLimit] = useState('')

  const load = useCallback(async () => {
    try { setKeys(await api.keys()) } catch { /* ignore */ }
  }, [])
  useEffect(() => { load() }, [load])

  const create = async () => {
    const k = await api.createKey(name.trim(), parseFloat(limit) || 0)
    setName(''); setLimit('')
    toast(t('keys.created') + k.key)
    navigator.clipboard.writeText(k.key).catch(() => {})
    load()
  }
  const toggle = async (k: ApiKey) => { await api.toggleKey(k.id, !k.enabled); toast(k.enabled ? t('toast.keyOff') : t('toast.keyOn')); load() }
  const del = async (k: ApiKey) => { if (confirm(t('common.delete') + ' ' + (k.name || k.id) + ' ?')) { await api.deleteKey(k.id); toast(t('toast.keyDeleted')); load() } }

  return (
    <div className={cls.card}>
      <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)] flex-wrap">
        <span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Key /></span> {t('keys.title')}</span>
        <span className="ml-auto inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-bold text-[var(--ok)] bg-[rgba(15,118,110,.12)]"><I.Shield /> {t('keys.required')}</span>
      </div>
      <div className="p-5">
        <p className="text-[var(--muted)] mt-0 mb-3 text-[13px]">{t('keys.desc')}</p>

        <div className="flex gap-2 flex-wrap items-end mb-4">
          <div className="flex-1 min-w-[160px]"><label className={cls.label} htmlFor="kpp-key-name">{t('keys.name')}</label>
            <input id="kpp-key-name" className={cls.input} placeholder={t('keys.namePlaceholder')} value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div className="w-40"><label className={cls.label} htmlFor="kpp-key-limit">{t('keys.limit')}</label>
            <input id="kpp-key-limit" className={cls.input} type="number" min="0" step="0.01" placeholder="0" value={limit} onChange={(e) => setLimit(e.target.value)} /></div>
          <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={create}><I.Plus /> {t('keys.create')}</button>
        </div>

        {keys.length === 0 ? (
          <div className="text-center py-6 text-[var(--muted)] text-[13px]">{t('keys.empty')}</div>
        ) : (
          <div className="flex flex-col gap-2.5">
            {keys.map((k) => {
              const pct = k.creditLimit > 0 ? Math.min(100, Math.round((k.credits / k.creditLimit) * 100)) : 0
              const hot = pct >= 85
              const keyStr = privacy ? mask(k.key) : k.key
              return (
                <div key={k.id} className={`rounded-xl border border-[var(--border)] bg-[var(--panel2)] p-3.5 ${k.enabled ? '' : 'opacity-60'}`}>
                  <div className="flex items-center gap-2.5 flex-wrap">
                    <span className="font-semibold text-[13.5px]">{k.name || k.id}</span>
                    {k.enabled
                      ? <span className="px-2 py-0.5 rounded-full text-[10.5px] font-bold text-[var(--ok)] bg-[rgba(15,118,110,.12)]">enabled</span>
                      : <span className="px-2 py-0.5 rounded-full text-[10.5px] font-bold text-[var(--muted)] bg-[var(--panel3)]">disabled</span>}
                    <div className="ml-auto flex gap-2">
                      <button className={`${cls.btn} ${cls.sm}`} onClick={() => { navigator.clipboard.writeText(k.key); toast(t('keys.copied')) }}><I.Copy /> {t('common.copy')}</button>
                      <button className={`${cls.btn} ${cls.sm}`} onClick={() => toggle(k)}><I.Power /> {k.enabled ? t('common.off') : t('common.on')}</button>
                      <button className={`${cls.btn} ${cls.btnDanger} ${cls.sm}`} onClick={() => del(k)} aria-label={t('common.delete')}><I.Trash /></button>
                    </div>
                  </div>
                  <div className="font-mono text-[11.5px] text-[var(--faint)] break-all mt-1.5">{keyStr}</div>
                  <div className="flex justify-between text-[11.5px] text-[var(--muted)] mt-2 mb-1">
                    <span>Credits: <b className="text-[var(--text)]">{fmtNum(k.credits)}</b>{k.creditLimit > 0 ? <> / {fmtNum(k.creditLimit)}</> : ' (∞)'}</span>
                    <span>{fmtNum(k.requests)} req · {fmtTime(k.lastUsedUnix)}</span>
                  </div>
                  {k.creditLimit > 0 && (
                    <div className="h-[6px] rounded bg-[var(--panel3)] overflow-hidden">
                      <div className="h-full rounded" style={{ width: pct + '%', background: hot ? 'var(--danger)' : 'var(--brand)' }} />
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
