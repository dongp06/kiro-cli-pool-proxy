import type { useTheme } from '../hooks/useTheme'
import type { T } from '../i18n'
import * as I from '../icons'

export function ThemeBtn({ theme, t }: { theme: ReturnType<typeof useTheme>; t: T }) {
  return (
    <button className="w-[38px] h-[38px] grid place-items-center rounded-[var(--radius)] bg-[var(--panel)] border border-[var(--border)] text-[var(--text)] cursor-pointer hover:bg-[var(--panel3)] text-base"
      onClick={theme.toggle} title={t('theme.toggle')} aria-label={t('theme.toggle')}>
      {theme.theme === 'dark' ? <I.Moon /> : <I.Sun />}
    </button>
  )
}
