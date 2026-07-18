import { useCallback, useState } from 'react'

type Toast = { id: number; msg: string; err?: boolean }

export function useToasts() {
  const [items, setItems] = useState<Toast[]>([])
  const push = useCallback((msg: string, err?: boolean) => {
    const id = Date.now() + Math.random()
    setItems((s) => [...s, { id, msg, err }])
    setTimeout(() => setItems((s) => s.filter((t) => t.id !== id)), 2800)
  }, [])
  const node = (
    <div className="fixed bottom-6 right-6 z-[200] flex flex-col gap-2" role="status" aria-live="polite">
      {items.map((t) => (
        <div
          key={t.id}
          className="flex items-center gap-2.5 rounded-xl px-4 py-3 text-[13.5px] max-w-sm bg-[var(--panel)] border border-[var(--border)]"
          style={{ borderLeft: `3px solid ${t.err ? 'var(--danger)' : 'var(--brand)'}`, animation: 'toastin .22s' }}
        >
          <span aria-hidden="true">{t.err ? '⚠' : '✓'}</span>
          <span>{t.msg}</span>
        </div>
      ))}
    </div>
  )
  return { push, node }
}
