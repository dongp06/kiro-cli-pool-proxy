import type { T } from '../i18n'
import type { ToastFn } from '../context'
import { cls } from '../lib/styles'
import { CodeBlock } from '../components/CodeBlock'
import * as I from '../icons'

export function ConnectTab({ url, region, t, toast }: { url: string; region: string; t: T; toast: ToastFn }) {
  const zero = `curl -fsSL ${url}/setup-client.sh | bash -s -- ${url} ${region} <API_KEY>\nkiro-cli chat`
  const manual = `kiro-cli settings api.krs.service '{"endpoint":"${url}","region":"${region}"}'\nkiro-cli settings api.cps.service '{"endpoint":"${url}","region":"${region}"}'`
  const eps: [React.ReactNode, string, string, string][] = [
    [<I.Chat />, 'Chat / Assistant', 'GenerateAssistantResponse', `${url} → runtime.*.kiro.dev`],
    [<I.Layers />, 'Profiles / Usage', 'ListAvailableProfiles · GetUsageLimits', `${url} → management.*.kiro.dev`],
    [<I.Rocket />, 'Client bootstrap', 'GET', `${url}/setup-client.sh`],
    [<I.Chart />, 'Health', 'GET', `${url}/health`],
  ]
  return (
    <>
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Rocket /></span> {t('connect.zeroTitle')}</span></div>
        <div className="p-5">
          <span className="inline-block px-2.5 py-0.5 rounded-md text-[11px] font-bold mb-2 bg-[rgba(15,118,110,.12)] text-[var(--ok)]">{t('connect.recommended')}</span>
          <p className="text-[var(--muted)] mt-0 mb-3">{t('connect.zeroDesc1')}</p>
          <CodeBlock text={zero} toast={toast} t={t} />
          <p className="text-[var(--muted)] mb-0 mt-3">{t('connect.zeroDesc2')}</p>
        </div>
      </div>
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Plug /></span> {t('connect.manualTitle')}</span></div>
        <div className="p-5">
          <span className="inline-block px-2.5 py-0.5 rounded-md text-[11px] font-bold mb-2 bg-[rgba(45,98,239,.12)] text-[var(--accent)]">{t('connect.manualBadge')}</span>
          <p className="text-[var(--muted)] mt-0 mb-3">{t('connect.manualDesc1')}</p>
          <CodeBlock text={manual} toast={toast} t={t} />
          <p className="text-[var(--muted)] mb-0 mt-3">{t('connect.manualDesc2')}</p>
        </div>
      </div>
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Layers /></span> {t('connect.endpoints')}</span></div>
        <div className="p-5">
          {eps.map(([ic, ti, m, u], i) => (
            <div key={i} className="flex items-center gap-3.5 px-4 py-3.5 border border-[var(--border)] rounded-xl bg-[var(--panel2)] mb-2.5 flex-wrap">
              <div className="w-[38px] h-[38px] flex-none rounded-[10px] grid place-items-center bg-[var(--panel3)] text-[var(--accent)] text-base" aria-hidden="true">{ic}</div>
              <div><div className="font-semibold text-[13.5px]">{ti}</div><div className="text-[var(--faint)] text-[11.5px]">{m}</div></div>
              <div className="ml-auto font-mono text-xs text-[var(--muted)] break-all text-right">{u}</div>
            </div>
          ))}
        </div>
      </div>
    </>
  )
}
