import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import { useTheme } from './hooks/useTheme'
import { useLang } from './i18n'
import { useToasts } from './hooks/useToasts'
import { Login } from './components/Login'
import { Dashboard } from './components/Dashboard'

export default function App() {
  const [phase, setPhase] = useState<'loading' | 'login' | 'app'>('loading')
  const [authRequired, setAuthRequired] = useState(false)
  const toast = useToasts()
  const theme = useTheme()
  const lang = useLang()

  const boot = useCallback(async () => {
    try {
      const a = await api.auth()
      setAuthRequired(a.authRequired)
      if (a.authed) setPhase('app')
      else if (a.authRequired) setPhase('login')
      else {
        await api.login('')
        setPhase('app')
      }
    } catch {
      setPhase('login')
    }
  }, [])
  useEffect(() => { boot() }, [boot])

  if (phase === 'loading')
    return (
      <div className="h-full grid place-items-center">
        <span className="inline-block w-6 h-6 rounded-full border-2 border-[var(--border)] border-t-[var(--brand)]" style={{ animation: 'spin .7s linear infinite' }} />
      </div>
    )
  if (phase === 'login')
    return (<><Login onOk={() => setPhase('app')} ctx={{ theme, lang }} />{toast.node}</>)

  return (
    <>
      <Dashboard
        authRequired={authRequired}
        ctx={{ theme, lang }}
        onLogout={() => setPhase('login')}
        onUnauth={() => setPhase('login')}
        toast={toast.push}
      />
      {toast.node}
    </>
  )
}
