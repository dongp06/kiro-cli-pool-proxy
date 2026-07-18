import { useCallback, useEffect, useState } from 'react'
import type { Lang, T } from './types'
import { vi } from './locales/vi'
import { en } from './locales/en'

const DICT: Record<Lang, Record<string, string>> = { vi, en }

export function useLang() {
  const [lang, setLang] = useState<Lang>(() => (localStorage.getItem('kpp_lang') as Lang) || 'vi')
  useEffect(() => { localStorage.setItem('kpp_lang', lang) }, [lang])
  const t: T = useCallback((key, vars) => {
    let s = DICT[lang][key] ?? DICT.vi[key] ?? key
    if (vars) for (const k in vars) s = s.replace('{' + k + '}', String(vars[k]))
    return s
  }, [lang])
  return { lang, setLang, t }
}

export type { Lang, T }
