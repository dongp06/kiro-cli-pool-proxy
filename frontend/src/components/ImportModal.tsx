import { useState } from 'react'
import { api } from '../api'
import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import { Modal } from './Modal'

export function ImportModal({ t, region, onClose, onDone, toast }: {
  t: T
  region: string
  onClose: () => void
  onDone: () => void
  toast: ToastFn
}) {
  const [path, setPath] = useState('')
  const [arn, setArn] = useState('')
  const [reg, setReg] = useState(region)
  const submit = async () => {
    const r = await api.importLocal({ path, profileArn: arn, region: reg })
    const j = await r.json()
    if (r.ok) { toast(t('import.ok') + (j.authMethod || '')); onDone() }
    else toast(t('import.err') + (j.error || ''), true)
  }
  return (
    <Modal title={t('import.title')} onClose={onClose}>
      <p className="text-[var(--muted)] mt-0">{t('import.desc')}</p>
      <label className={cls.label}>{t('import.dbPath')}</label>
      <input className={cls.input} placeholder="~/.local/share/kiro-cli/data.sqlite3" value={path} onChange={(e) => setPath(e.target.value)} />
      <label className={cls.label}>{t('import.arn')}</label>
      <input className={`${cls.input} font-mono`} placeholder="arn:aws:codewhisperer:us-east-1:...:profile/..." value={arn} onChange={(e) => setArn(e.target.value)} />
      <label className={cls.label}>{t('import.region')}</label>
      <input className={cls.input} placeholder="us-east-1" value={reg} onChange={(e) => setReg(e.target.value)} />
      <div className="flex gap-2.5 justify-end mt-[22px]">
        <button className={`${cls.btn} ${cls.btnGhost}`} onClick={onClose}>{t('common.cancel')}</button>
        <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={submit}>{t('import.submit')}</button>
      </div>
    </Modal>
  )
}
