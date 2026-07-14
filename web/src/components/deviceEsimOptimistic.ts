import type { EsimEUICCProfiles } from '../types/api'

function normalizeICCID(value: string | undefined | null): string {
  if (!value) return ''
  return value.replace(/\s+/g, '').trim()
}

export function applyOptimisticActiveState(
  groups: EsimEUICCProfiles[],
  targetICCID: string,
  aidHex: string
): EsimEUICCProfiles[] {
  const target = normalizeICCID(targetICCID)
  const targetGroup = aidHex.trim().toUpperCase()
  const groupProfiles = (group: EsimEUICCProfiles) => Array.isArray(group.profiles) ? group.profiles : []
  const matchedGroup = groups.find((group) => {
    if (targetGroup && group.aid_hex.trim().toUpperCase() !== targetGroup) {
      return false
    }
    return groupProfiles(group).some((profile) => normalizeICCID(profile.iccid) === target)
  }) ?? groups.find((group) => groupProfiles(group).some((profile) => normalizeICCID(profile.iccid) === target))

  if (!matchedGroup) {
    return groups
  }

  return groups.map((group) => ({
    ...group,
    profiles: groupProfiles(group).map((profile) => {
      const isTarget = normalizeICCID(profile.iccid) === target
      return {
        ...profile,
        state: isTarget ? 1 : 0,
        state_text: isTarget ? '已启用' : '已禁用'
      }
    })
  }))
}
