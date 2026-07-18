import type { useLang } from '../i18n'

export function LangSwitch({ lang }: { lang: ReturnType<typeof useLang> }) {
  return (
    <div className="flex rounded-[var(--radius)] border border-[var(--border)] overflow-hidden text-[12px] font-semibold" role="group" aria-label={lang.t('lang.toggle')}>
      {(['vi', 'en'] as const).map((l) => (
        <button key={l} onClick={() => lang.setLang(l)} title={lang.t('lang.toggle')}
          aria-pressed={lang.lang === l}
          className={`px-2.5 py-1.5 cursor-pointer transition-colors ${lang.lang === l ? 'bg-[var(--primary)] text-[var(--primary-fg)]' : 'bg-[var(--panel)] text-[var(--muted)] hover:text-[var(--text)]'}`}>
          {l.toUpperCase()}
        </button>
      ))}
    </div>
  )
}
