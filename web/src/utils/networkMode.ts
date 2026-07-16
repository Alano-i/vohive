export function formatNetworkMode(duplex: unknown, mode: unknown): string {
  const normalizedMode = String(mode || '').trim()
  if (!normalizedMode) return ''
  if (normalizedMode.toUpperCase().includes('LTE')) return '4G LTE'
  return [String(duplex || '').trim(), normalizedMode].filter(Boolean).join(' ')
}
