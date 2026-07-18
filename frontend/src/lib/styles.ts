/** Shared Tailwind class fragments for consistent controls. */
export const cls = {
  btn: 'inline-flex items-center gap-2 rounded-[var(--radius)] px-3.5 py-2 text-[13px] font-medium cursor-pointer transition-colors border bg-[var(--panel)] border-[var(--border)] text-[var(--text)] hover:bg-[var(--panel3)] whitespace-nowrap',
  btnPrimary: 'bg-[var(--primary)]! border-[var(--primary)]! text-[var(--primary-fg)]! font-semibold hover:opacity-90',
  btnDanger: 'border-transparent! text-[var(--danger)]! bg-[rgba(229,75,79,.1)] hover:bg-[rgba(229,75,79,.18)]',
  btnGhost: 'bg-transparent! border-[var(--border)]',
  sm: 'px-2.5 py-1.5 text-xs',
  input: 'w-full rounded-[var(--radius)] px-3 py-2.5 text-[13px] bg-[var(--panel)] border border-[var(--border)] text-[var(--text)] outline-none focus:border-[var(--text)]',
  card: 'bg-[var(--panel)] border border-[var(--border)] rounded-xl mb-[18px]',
  label: 'block text-xs font-semibold text-[var(--muted)] mt-3 mb-1.5',
}
