import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import * as I from '../icons'

export function CodeBlock({ text, toast, t }: { text: string; toast: ToastFn; t: T }) {
  return (
    <div className="relative rounded-xl p-3.5 pr-11 font-mono text-[12.5px] leading-7 whitespace-pre-wrap break-all bg-[var(--bg2)] border border-[var(--border)] text-[var(--brand)]">
      <button className={`${cls.btn} ${cls.sm} absolute top-2 right-2`} aria-label={t('common.copy')}
        onClick={() => { navigator.clipboard.writeText(text); toast(t('connect.copied')) }}><I.Copy /></button>
      {text}
    </div>
  )
}
