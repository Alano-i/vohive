export function isWwanQmiControlPath(path: string | null | undefined): boolean {
  const value = String(path || '').trim()
  if (!value) return false
  const basename = value.replace(/\\/g, '/').split('/').filter(Boolean).pop() || value
  return /^wwan\d+qmi\d+$/.test(basename)
}

type DiscoveredBackendCandidate = {
  mode?: string
  control_path?: string
  at_port?: string
}

export function isHybridATQmiDiscovery(device: DiscoveredBackendCandidate | null | undefined): boolean {
  if (!device) return false
  return String(device.mode || '').toLowerCase() === 'qmi' &&
    !!String(device.control_path || '').trim() &&
    !!String(device.at_port || '').trim() &&
    !isWwanQmiControlPath(device.control_path)
}

export function preferredBackendForDiscovery(
  device: DiscoveredBackendCandidate | null | undefined
): 'at' | 'qmi' | 'mbim' {
  const mode = String(device?.mode || '').toLowerCase()
  if (mode === 'mbim') return 'mbim'
  if (isWwanQmiControlPath(device?.control_path)) return 'qmi'
  if (mode === 'qmi' && !String(device?.at_port || '').trim()) return 'qmi'
  return 'at'
}
