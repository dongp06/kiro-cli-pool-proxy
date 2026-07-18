import { useState } from 'react'
import { api } from '../api'
import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import { Modal } from './Modal'

const AUTH_FIELDS: Record<string, string[]> = {
  idc: ['accessToken', 'refreshToken', 'clientId', 'clientSecret', 'region', 'profileArn'],
  social: ['accessToken', 'refreshToken', 'region', 'profileArn'],
  external_idp: ['accessToken', 'refreshToken', 'clientId', 'tokenEndpoint', 'scopes', 'region', 'profileArn'],
  api_key: ['accessToken', 'region', 'profileArn'],
}
const PH: Record<string, string> = {
  accessToken: 'eyJ... / ksk_...', refreshToken: 'eyJ...',
  profileArn: 'arn:aws:codewhisperer:us-east-1:...:profile/...', region: 'us-east-1',
}

export function AddModal({ t, onClose, onDone, toast }: { t: T; onClose: () => void; onDone: () => void; toast: ToastFn }) {
  const [auth, setAuth] = useState('idc')
  const [f, setF] = useState<Record<string, string>>({})
  const set = (k: string, v: string) => setF((s) => ({ ...s, [k]: v }))
  const submit = async () => {
    if (!f.accessToken) { toast(t('add.tokenRequired'), true); return }
    const acc: Record<string, unknown> = { authMethod: auth, email: f.email || '' }
    AUTH_FIELDS[auth].forEach((k) => { if (f[k]) acc[k] = f[k] })
    const r = await api.addAccount(acc)
    if (r.ok) { toast(t('add.added')); onDone() } else toast(t('add.error'), true)
  }
  return (
    <Modal title={t('add.title')} onClose={onClose}>
      <label className={cls.label}>{t('add.authMethod')}</label>
      <select className={cls.input} value={auth} onChange={(e) => { setAuth(e.target.value); setF({}) }}>
        <option value="idc">idc (IAM Identity Center / Builder ID)</option>
        <option value="social">social</option>
        <option value="external_idp">external_idp</option>
        <option value="api_key">api_key</option>
      </select>
      <label className={cls.label}>{t('add.emailOpt')}</label>
      <input className={cls.input} placeholder="user@example.com" value={f.email || ''} onChange={(e) => set('email', e.target.value)} />
      {AUTH_FIELDS[auth].map((k) => (
        <div key={k}><label className={cls.label}>{k}</label>
          <input className={cls.input} placeholder={PH[k] || ''} value={f[k] || ''} onChange={(e) => set(k, e.target.value)} /></div>
      ))}
      <div className="flex gap-2.5 justify-end mt-[22px]">
        <button className={`${cls.btn} ${cls.btnGhost}`} onClick={onClose}>{t('common.cancel')}</button>
        <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={submit}>{t('acc.add')}</button>
      </div>
    </Modal>
  )
}
