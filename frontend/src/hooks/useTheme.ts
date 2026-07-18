import { useEffect, useState } from 'react'

export function useTheme() {
  const [theme, setTheme] = useState<string>(() => localStorage.getItem('kpp_theme') || 'light')
  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
    localStorage.setItem('kpp_theme', theme)
  }, [theme])
  return { theme, setTheme, toggle: () => setTheme((t) => (t === 'dark' ? 'light' : 'dark')) }
}
