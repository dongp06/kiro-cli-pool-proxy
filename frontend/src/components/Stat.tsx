export function Stat({ kind, icon, title, value, sub }: {
  kind: 'a' | 'b' | 'c' | 'd'
  icon: React.ReactNode
  title: string
  value: React.ReactNode
  sub?: string
}) {
  const iconColor = { a: 'var(--brand)', b: 'var(--accent)', c: 'var(--purple)', d: 'var(--warn)' }[kind]
  return (
    <div className="relative overflow-hidden bg-[var(--panel)] border border-[var(--border)] rounded-xl p-5">
      <div className="absolute top-[18px] right-[18px] text-[26px] opacity-40" style={{ color: iconColor }} aria-hidden="true">{icon}</div>
      <div className="text-xs uppercase tracking-wider font-semibold text-[var(--muted)]">{title}</div>
      <div className="text-3xl font-extrabold mt-2 tracking-tight">{value}</div>
      <div className="text-[12.5px] text-[var(--muted)] mt-1">{sub || ' '}</div>
    </div>
  )
}
