import type { useTheme } from './hooks/useTheme'
import type { useLang } from './i18n'

/** App-wide context bundle passed to Login / Dashboard. */
export type Ctx = {
  theme: ReturnType<typeof useTheme>
  lang: ReturnType<typeof useLang>
}

/** Toast pusher signature shared across components. */
export type ToastFn = (msg: string, err?: boolean) => void
