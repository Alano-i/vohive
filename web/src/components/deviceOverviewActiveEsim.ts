import type { DeviceOverviewItem } from '../types/api'

export function activeEsimProfileDisplayName(device: Pick<DeviceOverviewItem, 'active_esim_profile_name'> | null | undefined) {
  return device?.active_esim_profile_name?.trim() || ''
}

export function deviceDisplayName(device: Pick<DeviceOverviewItem, 'id' | 'name' | 'active_esim_profile_name'> | null | undefined) {
  const base = String(device?.name || device?.id || '').trim()
  const profile = activeEsimProfileDisplayName(device)
  if (!profile || profile === base) return base
  if (!base) return profile
  return `${base} · ${profile}`
}
