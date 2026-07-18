import { useState } from 'react'
import { api } from '../api'
import type { Ctx } from '../context'
import { cls } from '../lib/styles'
import * as I from '../icons'
import { LangSwitch } from './LangSwitch'
import { ThemeBtn } from './ThemeBtn'

export function Login({ onOk, ctx }: { onOk: () => void; ctx: Ctx }) {
  const { t } = ctx.lang
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const submit = async () => {
    const r = await api.login(pw)
    if (r.ok) onOk()
    else setErr(t('login.wrong'))
  }
  return (
    <div className="h-full grid place-items-center p-6">
      <div className="w-[400px] max-w-full rounded-2xl p-9 bg-[var(--panel)] border border-[var(--border)]">
        <div className="w-14 h-14 rounded-2xl mx-auto mb-4 grid place-items-center bg-[var(--primary)] text-[var(--primary-fg)] text-3xl">
          <I.Logo />
        </div>
        <h1 className="text-center text-[22px] font-extrabold m-0 mb-1.5">KiroPool</h1>
        <p className="text-center text-[var(--muted)] text-[13.5px] m-0 mb-6">{t('login.subtitle')}</p>
        <label className={cls.label} htmlFor="kpp-password">{t('login.password')}</label>
        <div className="relative">
          <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--faint)]" aria-hidden="true"><I.Lock /></span>
          <input id="kpp-password" className={cls.input + ' pl-10'} type="password" value={pw} placeholder={t('login.placeholder')}
            onChange={(e) => setPw(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </div>
        <div className="h-4" />
        <button className={`${cls.btn} ${cls.btnPrimary} w-full justify-center py-3`} onClick={submit}>
          <I.LogIn /> {t('login.submit')}
        </button>
        <p className="text-center text-[var(--danger)] text-[13px] h-4 mt-3" role="alert">{err}</p>
      </div>
      <div className="fixed top-5 right-5 flex items-center gap-2.5">
        <LangSwitch lang={ctx.lang} />
        <ThemeBtn theme={ctx.theme} t={t} />
      </div>
    </div>
  )
}
