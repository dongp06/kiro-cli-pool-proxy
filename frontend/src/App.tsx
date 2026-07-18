import { useCallback, useEffect, useRef, useState } from 'react'
import { api, Unauthorized, type Account, type ApiKey, type Overview } from './api'
import * as I from './icons'

/* ---------- helpers ---------- */
const fmtNum = (n: number) => (n || 0).toLocaleString('en-US', { maximumFractionDigits: 2 })
function fmtTime(u: number): string {
  if (!u) return '—'
  const d = Date.now() / 1000 - u
  if (d < 60) return Math.floor(d) + 's trước'
  if (d < 3600) return Math.floor(d / 60) + 'm trước'
  if (d < 86400) return Math.floor(d / 3600) + 'h trước'
  return new Date(u * 1000).toLocaleDateString()
}

const cls = {
  btn: 'inline-flex items-center gap-2 rounded-[10px] px-3.5 py-2 text-[13px] font-medium cursor-pointer transition-all border bg-[var(--panel2)] border-[var(--border2)] text-[var(--text)] hover:border-[var(--accent)] hover:-translate-y-px whitespace-nowrap',
  btnPrimary: 'border-transparent! text-[#04121f]! font-bold bg-gradient-to-br from-[var(--brand)] to-[var(--brand2)] hover:shadow-[0_0_20px_rgba(110,231,183,.3)]',
  btnDanger: 'border-transparent! text-[var(--danger)]! bg-[rgba(248,113,113,.08)] hover:bg-[rgba(248,113,113,.16)]',
  btnGhost: 'bg-transparent! border-[var(--border)]',
  sm: 'px-2.5 py-1.5 text-xs',
  input: 'w-full rounded-[10px] px-3 py-2.5 text-[13px] bg-[var(--panel2)] border border-[var(--border2)] text-[var(--text)] outline-none focus:border-[var(--accent)] focus:ring-3 focus:ring-[rgba(96,165,250,.15)]',
  card: 'bg-[var(--panel)] border border-[var(--border)] rounded-2xl shadow-[0_10px_40px_rgba(0,0,0,.4)] mb-[18px]',
  label: 'block text-xs font-semibold text-[var(--muted)] mt-3 mb-1.5',
}

/* ---------- toast ---------- */
type Toast = { id: number; msg: string; err?: boolean }
function useToasts() {
  const [items, setItems] = useState<Toast[]>([])
  const push = useCallback((msg: string, err?: boolean) => {
    const id = Date.now() + Math.random()
    setItems((s) => [...s, { id, msg, err }])
    setTimeout(() => setItems((s) => s.filter((t) => t.id !== id)), 2800)
  }, [])
  const node = (
    <div className="fixed bottom-6 right-6 z-[200] flex flex-col gap-2">
      {items.map((t) => (
        <div
          key={t.id}
          className="flex items-center gap-2.5 rounded-xl px-4 py-3 text-[13.5px] max-w-sm bg-[var(--panel2)] border border-[var(--border2)] shadow-[0_10px_40px_rgba(0,0,0,.4)]"
          style={{ borderLeft: `3px solid ${t.err ? 'var(--danger)' : 'var(--brand)'}`, animation: 'toastin .22s' }}
        >
          <span>{t.err ? '⚠️' : '✅'}</span>
          <span>{t.msg}</span>
        </div>
      ))}
    </div>
  )
  return { push, node }
}

/* ---------- theme ---------- */
function useTheme() {
  const [theme, setTheme] = useState<string>(() => localStorage.getItem('kpp_theme') || 'dark')
  useEffect(() => {
    document.documentElement.classList.toggle('light', theme === 'light')
    localStorage.setItem('kpp_theme', theme)
  }, [theme])
  return { theme, setTheme, toggle: () => setTheme((t) => (t === 'dark' ? 'light' : 'dark')) }
}

/* ================= App ================= */
export default function App() {
  const [phase, setPhase] = useState<'loading' | 'login' | 'app'>('loading')
  const [authRequired, setAuthRequired] = useState(false)
  const toast = useToasts()
  const theme = useTheme()

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
        <span className="inline-block w-6 h-6 rounded-full border-2 border-[var(--border2)] border-t-[var(--brand)]" style={{ animation: 'spin .7s linear infinite' }} />
      </div>
    )
  if (phase === 'login')
    return (<><Login onOk={() => setPhase('app')} theme={theme} />{toast.node}</>)

  return (
    <>
      <Dashboard
        authRequired={authRequired}
        theme={theme}
        onLogout={() => setPhase('login')}
        onUnauth={() => setPhase('login')}
        toast={toast.push}
      />
      {toast.node}
    </>
  )
}

/* ---------- Login ---------- */
function Login({ onOk, theme }: { onOk: () => void; theme: ReturnType<typeof useTheme> }) {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const submit = async () => {
    const r = await api.login(pw)
    if (r.ok) onOk()
    else setErr('Sai mật khẩu')
  }
  return (
    <div className="h-full grid place-items-center p-6">
      <div className="w-[400px] max-w-full rounded-[22px] p-9 bg-[var(--panel)] border border-[var(--border2)] shadow-[0_10px_40px_rgba(0,0,0,.4)]">
        <div className="w-14 h-14 rounded-2xl mx-auto mb-4 grid place-items-center text-[#04121f] shadow-[0_0_20px_rgba(110,231,183,.25)] bg-gradient-to-br from-[var(--brand)] to-[var(--accent)] text-3xl">
          <I.Logo />
        </div>
        <h1 className="text-center text-[22px] font-extrabold m-0 mb-1.5">KiroPool</h1>
        <p className="text-center text-[var(--muted)] text-[13.5px] m-0 mb-6">Đăng nhập để quản lý pool accounts</p>
        <label className={cls.label}>Mật khẩu</label>
        <div className="relative">
          <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--faint)]"><I.Lock /></span>
          <input className={cls.input + ' pl-10'} type="password" value={pw} placeholder="Admin password"
            onChange={(e) => setPw(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </div>
        <div className="h-4" />
        <button className={`${cls.btn} ${cls.btnPrimary} w-full justify-center py-3`} onClick={submit}>
          <I.LogIn /> Đăng nhập
        </button>
        <p className="text-center text-[var(--danger)] text-[13px] h-4 mt-3">{err}</p>
      </div>
      <button className="fixed top-5 right-5 icon-btn w-[38px] h-[38px] grid place-items-center rounded-[10px] bg-[var(--panel2)] border border-[var(--border)] text-[var(--text)] cursor-pointer hover:text-[var(--accent)]" onClick={theme.toggle} title="Đổi giao diện">
        {theme.theme === 'dark' ? <I.Moon /> : <I.Sun />}
      </button>
    </div>
  )
}

/* ---------- Dashboard ---------- */
type Tab = 'accounts' | 'connect' | 'settings'
function Dashboard(props: {
  authRequired: boolean
  theme: ReturnType<typeof useTheme>
  onLogout: () => void
  onUnauth: () => void
  toast: (m: string, e?: boolean) => void
}) {
  const { theme, toast } = props
  const [tab, setTab] = useState<Tab>('accounts')
  const [ov, setOv] = useState<Overview | null>(null)
  const [accs, setAccs] = useState<Account[]>([])
  const [listenAddr, setListenAddr] = useState('')
  const [refreshMs, setRefreshMs] = useState<number>(() => {
    const v = localStorage.getItem('kpp_refresh')
    return v !== null ? +v * 1000 : 8000
  })
  const [addOpen, setAddOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const region = accs[0]?.region || 'us-east-1'

  const refresh = useCallback(async () => {
    try {
      const [o, a, s] = await Promise.all([api.overview(), api.accounts(), api.settings()])
      setOv(o); setAccs(a); setListenAddr(s.listenAddr)
    } catch (e) {
      if (e instanceof Unauthorized) props.onUnauth()
    }
  }, [props])

  useEffect(() => { refresh() }, [refresh])
  useEffect(() => {
    if (refreshMs <= 0) return
    const t = setInterval(refresh, refreshMs)
    return () => clearInterval(t)
  }, [refresh, refreshMs])

  const logout = async () => { await api.logout().catch(() => {}); props.onLogout() }
  const setStrategy = async (s: string) => { await api.setStrategy(s); toast('Đã lưu strategy'); refresh() }

  const qpct = ov && ov.quotaLimit > 0 ? Math.round((ov.quotaUsed / ov.quotaLimit) * 100) : 0
  const url = location.protocol + '//' + location.host

  return (
    <div className="min-h-full flex flex-col">
      {/* header */}
      <header className="sticky top-0 z-20 border-b border-[var(--border)] backdrop-blur-xl" style={{ background: 'color-mix(in srgb, var(--bg) 82%, transparent)' }}>
        <div className="max-w-[1180px] mx-auto px-6 py-3.5 flex items-center gap-5 flex-wrap">
          <div className="flex items-center gap-3 font-extrabold text-[17px]">
            <span className="w-8 h-8 rounded-[9px] grid place-items-center text-[#04121f] text-lg shadow-[0_0_20px_rgba(110,231,183,.25)] bg-gradient-to-br from-[var(--brand)] to-[var(--accent)]"><I.Logo /></span>
            KiroPool
          </div>
          <nav className="flex gap-1 rounded-xl p-1 bg-[var(--panel2)] border border-[var(--border)]">
            {([['accounts', 'Accounts', I.Users], ['connect', 'Kết nối', I.Plug], ['settings', 'Cài đặt', I.Gear]] as const).map(([id, label, Ico]) => (
              <button key={id} onClick={() => setTab(id)}
                className={`inline-flex items-center gap-2 rounded-[9px] px-3.5 py-2 text-[13px] font-semibold cursor-pointer transition-all ${tab === id ? 'bg-[var(--panel3)] text-[var(--text)] shadow-[0_1px_3px_rgba(0,0,0,.2)]' : 'text-[var(--muted)] hover:text-[var(--text)]'}`}>
                <span className="text-[15px]"><Ico /></span><span>{label}</span>
              </button>
            ))}
          </nav>
          <div className="flex items-center gap-2.5 ml-auto">
            <button className="w-[38px] h-[38px] grid place-items-center rounded-[10px] bg-[var(--panel2)] border border-[var(--border)] text-[var(--text)] cursor-pointer hover:text-[var(--accent)] hover:border-[var(--accent)] text-base" onClick={theme.toggle} title="Đổi giao diện">
              {theme.theme === 'dark' ? <I.Moon /> : <I.Sun />}
            </button>
            {props.authRequired ? (
              <button className={`${cls.btn} ${cls.btnGhost} ${cls.sm}`} onClick={logout}><I.Power /> Đăng xuất</button>
            ) : (
              <span className="text-xs text-[var(--muted)]">auth: off</span>
            )}
          </div>
        </div>
      </header>

      <div className="max-w-[1180px] w-full mx-auto px-6 pt-6 pb-14 flex-1">
        {/* stats */}
        <div className="grid gap-4 mb-6" style={{ gridTemplateColumns: 'repeat(auto-fit,minmax(210px,1fr))' }}>
          <Stat kind="a" icon={<I.Users />} title="Accounts" value={ov?.totalAccounts ?? 0}
            sub={`${ov?.enabled ?? 0} bật · ${ov?.available ?? 0} sẵn sàng`} />
          <Stat kind="b" icon={<I.Route />} title="Tổng requests" value={fmtNum(ov?.totalRequests ?? 0)} sub="qua proxy" />
          <Stat kind="c" icon={<I.Coin />} title="Tổng credits" value={fmtNum(ov?.totalCredits ?? 0)} sub="chi phí tích lũy" />
          <Stat kind="d" icon={<I.Gauge />} title="Quota" value={ov && ov.quotaLimit > 0 ? qpct + '%' : '—'}
            sub={ov && ov.quotaLimit > 0 ? `${fmtNum(ov.quotaUsed)} / ${fmtNum(ov.quotaLimit)} invocations` : 'chưa poll'} />
        </div>

        {tab === 'accounts' && (
          <div className={cls.card}>
            <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)] flex-wrap">
              <span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Users /></span> Accounts</span>
              <div className="ml-auto flex gap-2.5 items-center flex-wrap">
                <select className={`${cls.input} w-auto`} value={ov?.strategy || 'smart'} onChange={(e) => setStrategy(e.target.value)}>
                  <option value="round-robin">round-robin</option>
                  <option value="smart">smart (quota-aware)</option>
                </select>
                <button className={`${cls.btn} ${cls.sm}`} onClick={() => setImportOpen(true)}><I.Download /> Import kiro-cli</button>
                <button className={`${cls.btn} ${cls.btnPrimary} ${cls.sm}`} onClick={() => setAddOpen(true)}><I.Plus /> Thêm account</button>
              </div>
            </div>
            <div className="p-5">
              {accs.length === 0 ? (
                <div className="text-center py-10 text-[var(--muted)]">
                  <div className="text-4xl opacity-40 mb-2.5 flex justify-center"><I.Users /></div>
                  Chưa có account.<br />Bấm "Thêm account" hoặc "Import kiro-cli".
                </div>
              ) : (
                <div className="grid gap-3.5" style={{ gridTemplateColumns: 'repeat(auto-fill,minmax(340px,1fr))' }}>
                  {accs.map((a) => (
                    <AccountCard key={a.id} a={a}
                      onToggle={async () => { await api.toggleAccount(a.id, !a.enabled); toast(a.enabled ? 'Đã tắt account' : 'Đã bật account'); refresh() }}
                      onDelete={async () => { if (confirm('Xóa account ' + a.id + ' ?')) { await api.deleteAccount(a.id); toast('Đã xóa account'); refresh() } }} />
                  ))}
                </div>
              )}
            </div>
          </div>
        )}

        {tab === 'connect' && <ConnectTab url={url} region={region} toast={toast} />}

        {tab === 'settings' && (
          <SettingsTab
            strategy={ov?.strategy || 'smart'} onStrategy={setStrategy}
            listenAddr={listenAddr} theme={theme} toast={toast}
            refreshSec={refreshMs / 1000}
            onRefresh={(v) => { setRefreshMs(v * 1000); localStorage.setItem('kpp_refresh', String(v)) }} />
        )}
      </div>

      <footer className="border-t border-[var(--border)] mt-5">
        <div className="max-w-[1180px] mx-auto px-6 py-5 flex items-center gap-3.5 text-[12.5px] text-[var(--muted)] flex-wrap">
          <span className="flex items-center gap-2 font-extrabold text-sm text-[var(--text)]">
            <span className="w-6 h-6 rounded-md grid place-items-center text-[#04121f] bg-gradient-to-br from-[var(--brand)] to-[var(--accent)]"><I.Logo /></span> KiroPool
          </span>
          <span className="w-px h-5 bg-[var(--border2)]" />
          <span><span className="inline-block w-2 h-2 rounded-full bg-[var(--ok)] mr-1.5 shadow-[0_0_8px_var(--ok)]" style={{ animation: 'pulse 2s infinite' }} />Đang chạy</span>
          <span className="ml-auto">plain reverse-proxy · account rotation · credit accounting</span>
        </div>
      </footer>

      {addOpen && <AddModal onClose={() => setAddOpen(false)} onDone={() => { setAddOpen(false); refresh() }} toast={toast} />}
      {importOpen && <ImportModal region={region} onClose={() => setImportOpen(false)} onDone={() => { setImportOpen(false); refresh() }} toast={toast} />}
    </div>
  )
}

/* ---------- Stat card ---------- */
function Stat({ kind, icon, title, value, sub }: { kind: 'a' | 'b' | 'c' | 'd'; icon: React.ReactNode; title: string; value: React.ReactNode; sub?: string }) {
  const glow = { a: 'rgba(110,231,183,.16)', b: 'rgba(96,165,250,.16)', c: 'rgba(167,139,250,.16)', d: 'rgba(251,191,36,.16)' }[kind]
  const iconColor = { a: 'var(--brand)', b: 'var(--accent)', c: 'var(--purple)', d: 'var(--warn)' }[kind]
  return (
    <div className="relative overflow-hidden bg-[var(--panel)] border border-[var(--border)] rounded-2xl p-5 shadow-[0_10px_40px_rgba(0,0,0,.4)]">
      <div className="absolute inset-y-0 right-0 w-[120px] pointer-events-none" style={{ background: `radial-gradient(circle at 70% 30%, ${glow}, transparent 70%)` }} />
      <div className="absolute top-[18px] right-[18px] text-[26px] opacity-50" style={{ color: iconColor }}>{icon}</div>
      <div className="text-xs uppercase tracking-wider font-semibold text-[var(--muted)]">{title}</div>
      <div className="text-3xl font-extrabold mt-2 tracking-tight">{value}</div>
      <div className="text-[12.5px] text-[var(--muted)] mt-1">{sub || '\u00a0'}</div>
    </div>
  )
}

/* ---------- Account card ---------- */
function AccountCard({ a, onToggle, onDelete }: { a: Account; onToggle: () => void; onDelete: () => void }) {
  const pct = a.usageLimit > 0 ? Math.min(100, Math.round((a.usageCurrent / a.usageLimit) * 100)) : 0
  const hot = pct >= 85
  const name = a.email || a.id
  const initial = (name.replace(/[^A-Za-z0-9]/g, '').charAt(0) || '#').toUpperCase()
  let pill = <span className="pill on inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-bold text-[var(--ok)] bg-[rgba(52,211,153,.14)]">active</span>
  if (!a.enabled) pill = <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-bold text-[var(--muted)] bg-[rgba(139,152,169,.14)]">disabled</span>
  else if (!a.hasProfileArn) pill = <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-bold text-[var(--warn)] bg-[rgba(251,191,36,.14)]">thiếu ARN</span>
  else if (a.usageLimit > 0 && a.usageCurrent >= a.usageLimit) pill = <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-bold text-[var(--warn)] bg-[rgba(251,191,36,.14)]">hết quota</span>

  return (
    <div className={`bg-[var(--panel2)] border border-[var(--border)] rounded-[14px] p-4 transition-all hover:border-[var(--border2)] ${a.enabled ? '' : 'opacity-60'}`}>
      <div className="flex items-start gap-3 mb-3.5">
        <div className="w-10 h-10 rounded-[11px] flex-none grid place-items-center font-extrabold text-white text-[15px] bg-gradient-to-br from-[var(--accent)] to-[var(--purple)]">{initial}</div>
        <div className="min-w-0">
          <div className="font-bold text-sm break-all">{name}</div>
          <div className="text-[var(--faint)] text-[11px] font-mono break-all">{a.id}</div>
        </div>
        <div className="ml-auto">{pill}</div>
      </div>
      <div className="flex gap-3.5 flex-wrap text-[12.5px] text-[var(--muted)] mb-1.5">
        <span>Auth <b className="text-[var(--text)] font-semibold">{a.authMethod}</b></span>
        <span>Region <b className="text-[var(--text)] font-semibold font-mono">{a.region || '—'}</b></span>
      </div>
      <div className="my-3">
        <div className="flex justify-between text-xs text-[var(--muted)] mb-1.5">
          <span>Quota (invocations)</span>
          <span>{a.usageLimit > 0 ? `${fmtNum(a.usageCurrent)} / ${fmtNum(a.usageLimit)} · ${pct}%` : 'chưa poll'}</span>
        </div>
        <div className="h-[7px] rounded-md bg-[var(--panel3)] overflow-hidden">
          <div className="h-full rounded-md transition-all" style={{ width: pct + '%', background: hot ? 'linear-gradient(90deg,var(--warn),var(--danger))' : 'linear-gradient(90deg,var(--brand),var(--accent))' }} />
        </div>
      </div>
      <div className="flex gap-3.5 flex-wrap text-[12.5px] text-[var(--muted)]">
        <span className="inline-flex items-center gap-1"><I.Coin /> <b className="text-[var(--text)] font-semibold">{fmtNum(a.credits)}</b></span>
        <span className="inline-flex items-center gap-1"><I.Route /> <b className="text-[var(--text)] font-semibold">{fmtNum(a.requests)}</b></span>
        <span>{fmtTime(a.lastUsedUnix)}</span>
      </div>
      <div className="flex gap-2 mt-3.5 pt-3 border-t border-[var(--border)]">
        <button className={`${cls.btn} ${cls.sm}`} onClick={onToggle}><I.Power /> {a.enabled ? 'Tắt' : 'Bật'}</button>
        <button className={`${cls.btn} ${cls.btnDanger} ${cls.sm} ml-auto`} onClick={onDelete}><I.Trash /> Xóa</button>
      </div>
    </div>
  )
}

/* ---------- Connect tab ---------- */
function CodeBlock({ text, toast }: { text: string; toast: (m: string, e?: boolean) => void }) {
  return (
    <div className="relative rounded-xl p-3.5 pr-11 font-mono text-[12.5px] leading-7 whitespace-pre-wrap break-all bg-[var(--bg2)] border border-[var(--border2)] text-[var(--brand)]">
      <button className={`${cls.btn} ${cls.sm} absolute top-2 right-2`} onClick={() => { navigator.clipboard.writeText(text); toast('Đã copy vào clipboard') }}><I.Copy /></button>
      {text}
    </div>
  )
}
function ConnectTab({ url, region, toast }: { url: string; region: string; toast: (m: string, e?: boolean) => void }) {
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
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Rocket /></span> Máy khách — Zero-login</span></div>
        <div className="p-5">
          <span className="inline-block px-2.5 py-0.5 rounded-md text-[11px] font-bold mb-2 bg-[rgba(52,211,153,.14)] text-[var(--ok)]">Khuyến nghị</span>
          <p className="text-[var(--muted)] mt-0 mb-3">Máy khách <b className="text-[var(--text)]">không cần login</b>, không cần account riêng — chỉ cần <b className="text-[var(--text)]">API key</b>. Thay <span className="font-mono">&lt;API_KEY&gt;</span> bằng key tạo ở tab Cài đặt (cần <span className="font-mono">curl</span> + <span className="font-mono">python3</span>):</p>
          <CodeBlock text={zero} toast={toast} />
          <p className="text-[var(--muted)] mb-0 mt-3">Xong rồi chạy <span className="font-mono">kiro-cli chat</span>. Proxy validate key → thay account pool → đếm credit theo key.</p>
        </div>
      </div>
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Plug /></span> Cấu hình thủ công</span></div>
        <div className="p-5">
          <span className="inline-block px-2.5 py-0.5 rounded-md text-[11px] font-bold mb-2 bg-[rgba(96,165,250,.14)] text-[var(--accent)]">Nếu client đã login sẵn</span>
          <p className="text-[var(--muted)] mt-0 mb-3">Trỏ endpoint kiro-cli vào proxy (không cần cert/MITM):</p>
          <CodeBlock text={manual} toast={toast} />
          <p className="text-[var(--muted)] mb-0 mt-3">Khôi phục: <span className="font-mono">kiro-cli settings -d api.krs.service</span></p>
        </div>
      </div>
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Layers /></span> Endpoints</span></div>
        <div className="p-5">
          {eps.map(([ic, t, m, u], i) => (
            <div key={i} className="flex items-center gap-3.5 px-4 py-3.5 border border-[var(--border)] rounded-xl bg-[var(--panel2)] mb-2.5 flex-wrap">
              <div className="w-[38px] h-[38px] flex-none rounded-[10px] grid place-items-center bg-[var(--panel3)] text-[var(--accent)] text-base">{ic}</div>
              <div><div className="font-semibold text-[13.5px]">{t}</div><div className="text-[var(--faint)] text-[11.5px]">{m}</div></div>
              <div className="ml-auto font-mono text-xs text-[var(--muted)] break-all text-right">{u}</div>
            </div>
          ))}
        </div>
      </div>
    </>
  )
}

/* ---------- Settings tab ---------- */
function SettingsTab({ strategy, onStrategy, listenAddr, theme, refreshSec, onRefresh, toast }: {
  strategy: string; onStrategy: (s: string) => void; listenAddr: string
  theme: ReturnType<typeof useTheme>; refreshSec: number; onRefresh: (v: number) => void
  toast: (m: string, e?: boolean) => void
}) {
  return (
    <>
      <ApiKeysCard toast={toast} />
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Gear /></span> Chiến lược xoay account</span></div>
        <div className="p-5">
          <label className={cls.label}>Strategy</label>
          <select className={cls.input} value={strategy} onChange={(e) => onStrategy(e.target.value)}>
            <option value="round-robin">round-robin — xoay tuần tự</option>
            <option value="smart">smart — ưu tiên account còn nhiều quota</option>
          </select>
          <small className="text-[var(--faint)] text-[11.5px] block mt-1.5">Áp dụng ngay cho các request tiếp theo.</small>
        </div>
      </div>
      <div className={cls.card}>
        <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)]"><span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Info /></span> Thông tin proxy</span></div>
        <div className="p-5">
          <div className="grid grid-cols-2 gap-x-3.5">
            <div><label className={cls.label}>Listen address</label><input className={`${cls.input} font-mono`} value={listenAddr} readOnly /></div>
            <div><label className={cls.label}>Giao diện</label>
              <select className={cls.input} value={theme.theme} onChange={(e) => theme.setTheme(e.target.value)}>
                <option value="dark">Tối</option><option value="light">Sáng</option>
              </select>
            </div>
          </div>
          <label className={cls.label}>Tự làm mới (giây)</label>
          <select className={cls.input} value={refreshSec} onChange={(e) => onRefresh(+e.target.value)}>
            <option value={5}>5s</option><option value={8}>8s</option><option value={15}>15s</option><option value={0}>Tắt</option>
          </select>
        </div>
      </div>
    </>
  )
}

/* ---------- Modal shell ---------- */
function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-5" style={{ background: 'rgba(4,7,12,.7)', backdropFilter: 'blur(4px)', animation: 'fade .18s' }} onClick={onClose}>
      <div className="w-[540px] max-w-full max-h-[88vh] overflow-auto rounded-2xl bg-[var(--panel)] border border-[var(--border2)] shadow-[0_10px_40px_rgba(0,0,0,.4)]" style={{ animation: 'pop .2s' }} onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center px-[22px] py-[18px] border-b border-[var(--border)]">
          <h3 className="m-0 text-base font-bold">{title}</h3>
          <button className="ml-auto bg-transparent border-0 text-[var(--muted)] text-2xl cursor-pointer leading-none hover:text-[var(--text)]" onClick={onClose}><I.X /></button>
        </div>
        <div className="px-[22px] py-5">{children}</div>
      </div>
    </div>
  )
}

/* ---------- Add modal ---------- */
const AUTH_FIELDS: Record<string, string[]> = {
  idc: ['accessToken', 'refreshToken', 'clientId', 'clientSecret', 'region', 'profileArn'],
  social: ['accessToken', 'refreshToken', 'region', 'profileArn'],
  external_idp: ['accessToken', 'refreshToken', 'clientId', 'tokenEndpoint', 'scopes', 'region', 'profileArn'],
  api_key: ['accessToken', 'region', 'profileArn'],
}
const PH: Record<string, string> = {
  accessToken: 'eyJ... / ksk_...', refreshToken: 'eyJ...',
  profileArn: 'arn:aws:codewhisperer:us-east-1:...:profile/...', region: 'us-east-1',
}
function AddModal({ onClose, onDone, toast }: { onClose: () => void; onDone: () => void; toast: (m: string, e?: boolean) => void }) {
  const [auth, setAuth] = useState('idc')
  const [f, setF] = useState<Record<string, string>>({})
  const set = (k: string, v: string) => setF((s) => ({ ...s, [k]: v }))
  const submit = async () => {
    if (!f.accessToken) { toast('accessToken bắt buộc', true); return }
    const acc: Record<string, unknown> = { authMethod: auth, email: f.email || '' }
    AUTH_FIELDS[auth].forEach((k) => { if (f[k]) acc[k] = f[k] })
    const r = await api.addAccount(acc)
    if (r.ok) { toast('Đã thêm account'); onDone() } else toast('Lỗi thêm account', true)
  }
  return (
    <Modal title="Thêm account" onClose={onClose}>
      <label className={cls.label}>Auth method</label>
      <select className={cls.input} value={auth} onChange={(e) => { setAuth(e.target.value); setF({}) }}>
        <option value="idc">idc (IAM Identity Center / Builder ID)</option>
        <option value="social">social</option>
        <option value="external_idp">external_idp</option>
        <option value="api_key">api_key</option>
      </select>
      <label className={cls.label}>Email (tùy chọn)</label>
      <input className={cls.input} placeholder="user@example.com" value={f.email || ''} onChange={(e) => set('email', e.target.value)} />
      {AUTH_FIELDS[auth].map((k) => (
        <div key={k}><label className={cls.label}>{k}</label>
          <input className={cls.input} placeholder={PH[k] || ''} value={f[k] || ''} onChange={(e) => set(k, e.target.value)} /></div>
      ))}
      <div className="flex gap-2.5 justify-end mt-[22px]">
        <button className={`${cls.btn} ${cls.btnGhost}`} onClick={onClose}>Hủy</button>
        <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={submit}>Thêm account</button>
      </div>
    </Modal>
  )
}

/* ---------- Import modal ---------- */
function ImportModal({ region, onClose, onDone, toast }: { region: string; onClose: () => void; onDone: () => void; toast: (m: string, e?: boolean) => void }) {
  const [path, setPath] = useState('')
  const [arn, setArn] = useState('')
  const [reg, setReg] = useState(region)
  const submit = async () => {
    const r = await api.importLocal({ path, profileArn: arn, region: reg })
    const j = await r.json()
    if (r.ok) { toast('Import thành công: ' + (j.authMethod || '')); onDone() }
    else toast('Import lỗi: ' + (j.error || ''), true)
  }
  return (
    <Modal title="Import từ kiro-cli SQLite" onClose={onClose}>
      <p className="text-[var(--muted)] mt-0">Đọc credential từ máy đang chạy proxy (kiro-cli đã login).</p>
      <label className={cls.label}>Đường dẫn DB (trống = mặc định)</label>
      <input className={cls.input} placeholder="~/.local/share/kiro-cli/data.sqlite3" value={path} onChange={(e) => setPath(e.target.value)} />
      <label className={cls.label}>Profile ARN (điền để chat hoạt động)</label>
      <input className={`${cls.input} font-mono`} placeholder="arn:aws:codewhisperer:us-east-1:...:profile/..." value={arn} onChange={(e) => setArn(e.target.value)} />
      <label className={cls.label}>Region</label>
      <input className={cls.input} placeholder="us-east-1" value={reg} onChange={(e) => setReg(e.target.value)} />
      <div className="flex gap-2.5 justify-end mt-[22px]">
        <button className={`${cls.btn} ${cls.btnGhost}`} onClick={onClose}>Hủy</button>
        <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={submit}>Import</button>
      </div>
    </Modal>
  )
}

/* ---------- API Keys card ---------- */
function ApiKeysCard({ toast }: { toast: (m: string, e?: boolean) => void }) {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [name, setName] = useState('')
  const [limit, setLimit] = useState('')

  const load = useCallback(async () => {
    try {
      setKeys(await api.keys())
    } catch { /* ignore */ }
  }, [])
  useEffect(() => { load() }, [load])

  const create = async () => {
    const k = await api.createKey(name.trim(), parseFloat(limit) || 0)
    setName(''); setLimit('')
    toast('Đã tạo key: ' + k.key)
    navigator.clipboard.writeText(k.key).catch(() => {})
    load()
  }
  const toggle = async (k: ApiKey) => { await api.toggleKey(k.id, !k.enabled); toast(k.enabled ? 'Đã tắt key' : 'Đã bật key'); load() }
  const del = async (k: ApiKey) => { if (confirm('Xóa key ' + (k.name || k.id) + ' ?')) { await api.deleteKey(k.id); toast('Đã xóa key'); load() } }

  return (
    <div className={cls.card}>
      <div className="flex items-center gap-3 px-5 py-4 border-b border-[var(--border)] flex-wrap">
        <span className="font-bold text-[15px] flex items-center gap-2.5"><span className="text-[var(--brand)] text-[17px]"><I.Key /></span> API Keys</span>
        <span className="ml-auto inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-bold text-[var(--ok)] bg-[rgba(52,211,153,.14)]"><I.Shield /> Bắt buộc</span>
      </div>
      <div className="p-5">
        <p className="text-[var(--muted)] mt-0 mb-3 text-[13px]">
          API key <b className="text-[var(--text)]">luôn bắt buộc</b>. Client phải seed key làm token:
          <span className="font-mono"> setup-client.sh URL REGION KEY</span>. Đặt credit limit để giới hạn từng key (như Kiro-Go).
        </p>

        <div className="flex gap-2 flex-wrap items-end mb-4">
          <div className="flex-1 min-w-[160px]"><label className={cls.label}>Tên key</label>
            <input className={cls.input} placeholder="vd: máy-của-A" value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div className="w-40"><label className={cls.label}>Credit limit (0 = ∞)</label>
            <input className={cls.input} type="number" min="0" step="0.01" placeholder="0" value={limit} onChange={(e) => setLimit(e.target.value)} /></div>
          <button className={`${cls.btn} ${cls.btnPrimary}`} onClick={create}><I.Plus /> Tạo key</button>
        </div>

        {keys.length === 0 ? (
          <div className="text-center py-6 text-[var(--muted)] text-[13px]">Chưa có API key.</div>
        ) : (
          <div className="flex flex-col gap-2.5">
            {keys.map((k) => {
              const pct = k.creditLimit > 0 ? Math.min(100, Math.round((k.credits / k.creditLimit) * 100)) : 0
              const hot = pct >= 85
              return (
                <div key={k.id} className={`rounded-xl border border-[var(--border)] bg-[var(--panel2)] p-3.5 ${k.enabled ? '' : 'opacity-60'}`}>
                  <div className="flex items-center gap-2.5 flex-wrap">
                    <span className="font-semibold text-[13.5px]">{k.name || k.id}</span>
                    {k.enabled
                      ? <span className="px-2 py-0.5 rounded-full text-[10.5px] font-bold text-[var(--ok)] bg-[rgba(52,211,153,.14)]">enabled</span>
                      : <span className="px-2 py-0.5 rounded-full text-[10.5px] font-bold text-[var(--muted)] bg-[rgba(139,152,169,.14)]">disabled</span>}
                    <div className="ml-auto flex gap-2">
                      <button className={`${cls.btn} ${cls.sm}`} onClick={() => { navigator.clipboard.writeText(k.key); toast('Đã copy key') }}><I.Copy /> Copy</button>
                      <button className={`${cls.btn} ${cls.sm}`} onClick={() => toggle(k)}><I.Power /> {k.enabled ? 'Tắt' : 'Bật'}</button>
                      <button className={`${cls.btn} ${cls.btnDanger} ${cls.sm}`} onClick={() => del(k)}><I.Trash /></button>
                    </div>
                  </div>
                  <div className="font-mono text-[11.5px] text-[var(--faint)] break-all mt-1.5">{k.key}</div>
                  <div className="flex justify-between text-[11.5px] text-[var(--muted)] mt-2 mb-1">
                    <span>Credits: <b className="text-[var(--text)]">{fmtNum(k.credits)}</b>{k.creditLimit > 0 ? <> / {fmtNum(k.creditLimit)}</> : ' (∞)'}</span>
                    <span>{fmtNum(k.requests)} req · {fmtTime(k.lastUsedUnix)}</span>
                  </div>
                  {k.creditLimit > 0 && (
                    <div className="h-[6px] rounded bg-[var(--panel3)] overflow-hidden">
                      <div className="h-full rounded" style={{ width: pct + '%', background: hot ? 'linear-gradient(90deg,var(--warn),var(--danger))' : 'linear-gradient(90deg,var(--brand),var(--accent))' }} />
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
