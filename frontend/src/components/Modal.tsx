import { useEffect } from 'react'
import * as I from '../icons'

export function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-5" style={{ background: 'rgba(0,0,0,.42)', animation: 'fade .18s' }} onClick={onClose}>
      <div className="w-[540px] max-w-full max-h-[88vh] overflow-auto rounded-2xl bg-[var(--panel)] border border-[var(--border)]"
        style={{ animation: 'pop .2s' }} onClick={(e) => e.stopPropagation()}
        role="dialog" aria-modal="true" aria-label={title}>
        <div className="flex items-center px-[22px] py-[18px] border-b border-[var(--border)]">
          <h3 className="m-0 text-base font-bold">{title}</h3>
          <button className="ml-auto bg-transparent border-0 text-[var(--muted)] text-2xl cursor-pointer leading-none hover:text-[var(--text)]"
            onClick={onClose} aria-label="Close"><I.X /></button>
        </div>
        <div className="px-[22px] py-5">{children}</div>
      </div>
    </div>
  )
}
