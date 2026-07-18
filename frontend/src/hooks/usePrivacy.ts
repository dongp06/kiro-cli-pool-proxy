import { useEffect, useState } from 'react'

export function usePrivacy() {
  const [on, setOn] = useState<boolean>(() => localStorage.getItem('kpp_privacy') !== '0')
  useEffect(() => { localStorage.setItem('kpp_privacy', on ? '1' : '0') }, [on])
  return { on, toggle: () => setOn((v) => !v) }
}
