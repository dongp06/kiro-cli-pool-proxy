export const fmtNum = (n: number) =>
  (n || 0).toLocaleString('en-US', { maximumFractionDigits: 2 })

export function fmtTime(u: number): string {
  if (!u) return '—'
  const d = Date.now() / 1000 - u
  if (d < 60) return Math.floor(d) + 's'
  if (d < 3600) return Math.floor(d / 60) + 'm'
  if (d < 86400) return Math.floor(d / 3600) + 'h'
  return new Date(u * 1000).toLocaleDateString()
}

export function fmtClock(u: number): string {
  if (!u) return '—'
  return new Date(u * 1000).toLocaleTimeString()
}

// mask obscures the middle of a string, keeping the head/tail visible.
export function mask(s: string): string {
  if (!s) return s
  const at = s.indexOf('@')
  if (at > 0) {
    const user = s.slice(0, at)
    const dom = s.slice(at + 1)
    return maskPart(user) + '@' + maskPart(dom)
  }
  return maskPart(s)
}

function maskPart(s: string): string {
  if (s.length <= 4) return s[0] + '***'
  return s.slice(0, 3) + '***' + s.slice(-2)
}
