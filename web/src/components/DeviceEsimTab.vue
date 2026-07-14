<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { EsimChipInfo, EsimEUICCProfiles, EsimNotificationItem, EsimSpaceDelta } from '../types/api'
import { devicesService } from '../services/devices'
import { errorMessage } from '../services/http'
import { api } from '../stores/auth'
import { SENSITIVE_VALUE_PLACEHOLDER, useSensitiveVisibility } from '../composables/useSensitiveVisibility'
import EsimCardPolicyInline from './EsimCardPolicyInline.vue'
import { applyOptimisticActiveState } from './deviceEsimOptimistic'
import { pickNextDownloadAid } from './deviceEsimOverviewRefresh'
import { describeDeleteResultNotice, describeDownloadTerminalNotice, describeSpaceDelta } from './deviceEsimOperationNotice'
import {
  formatEsimNotificationEvent,
  notificationDialogWidth,
  notificationListItemLayoutClass,
  notificationMetaContainerClass,
  notificationMetaItemClass,
  reconcileEsimNotificationDialogState,
  shouldShowEsimNotificationIcon
} from './deviceEsimNotifications'
import {
  Add24Regular,
  Alert24Regular,
  ArrowDownload24Regular,
  ArrowSync24Regular,
  Eye24Regular,
  EyeOff24Regular
} from '@vicons/fluent'

const props = defineProps<{
  deviceId: string
  deviceImei?: string
  isActive?: boolean
  deviceOnline?: boolean
}>()

// 数据状态
const loading = ref(false)
const profilesRefreshing = ref(false)
const chipInfo = ref<EsimChipInfo | null>(null)
const profiles = ref<EsimEUICCProfiles[]>([])

// 操作状态
const switching = ref<string | null>(null)
const deleting = ref<string | null>(null)
const renaming = ref<string | null>(null)
const editingPhone = ref<string | null>(null)
const profilePhones = ref<Record<string, string>>({})
const showSensitive = useSensitiveVisibility()
const renameValue = ref('')
// 行内卡策略展开态（手风琴，一次只展开一行，按 iccid 记）
const expandedPolicyIccid = ref<string | null>(null)
function togglePolicyPanel(iccid: string) {
  expandedPolicyIccid.value = expandedPolicyIccid.value === iccid ? null : iccid
}
const notifications = ref<EsimNotificationItem[]>([])
const notificationsLoading = ref(false)
const notificationsDialogOpen = ref(false)
const retryingNotificationSequence = ref<number | null>(null)

// 下载表单
const downloadForm = ref({
  smdp: '',
  matchingId: '',
  confirmationCode: '',
  aidHex: '',
  imei: ''
})
const downloading = ref(false)
const downloadProgress = ref(0)
const downloadMsg = ref('')
const downloadError = ref('')
const downloadSessionId = ref(0)
const recentSpaceDelta = ref<{ aidHex: string; message: string } | null>(null)
let recentSpaceDeltaTimer: number | null = null
let lastDeviceImeiDefault = ''

const downloadTargets = computed(() => {
  const detailed = chipInfo.value?.eids || []
  if (detailed.length > 0) return detailed
  return profiles.value.map(group => ({
    aid: group.aid_hex,
    eid: group.eid,
    free_nvram: ''
  }))
})

function pickAvailableDownloadAid(currentAidHex: string): string {
  const detailed = pickNextDownloadAid(chipInfo.value, currentAidHex)
  if (detailed) return detailed
  if (currentAidHex && downloadTargets.value.some(item => item.aid === currentAidHex)) {
    return currentAidHex
  }
  return downloadTargets.value[0]?.aid || ''
}

function defaultDeviceImei() {
  return (props.deviceImei || '').trim()
}

function applyDeviceImeiDefault(force = false) {
  const next = defaultDeviceImei()
  if (force || !downloadForm.value.imei || downloadForm.value.imei === lastDeviceImeiDefault) {
    downloadForm.value.imei = next
  }
  lastDeviceImeiDefault = next
}

// 智能解析完整的 LPA 激活码或移除 URL 前缀
watch(() => downloadForm.value.smdp, (newVal) => {
  if (!newVal) return

  if (newVal.startsWith('LPA:')) {
    const parts = newVal.split('$')
    if (parts.length >= 3) {
      downloadForm.value.smdp = parts[1] // SM-DP+
      downloadForm.value.matchingId = parts[2] // Matching ID
      ElMessage.success('已自动解析完整的 LPA 激活码')
    }
  } else if (newVal.startsWith('http://') || newVal.startsWith('https://')) {
    downloadForm.value.smdp = newVal.replace(/^https?:\/\//i, '')
  }
})

let fetchAbortController: AbortController | null = null
let fetchOverviewRequestId = 0

function normalizeAidHex(aidHex: string | undefined | null): string {
  return (aidHex || '').trim().toUpperCase()
}

function clearRecentSpaceDelta() {
  if (recentSpaceDeltaTimer !== null) {
    window.clearTimeout(recentSpaceDeltaTimer)
    recentSpaceDeltaTimer = null
  }
  recentSpaceDelta.value = null
}

function showRecentSpaceDelta(aidHex: string, spaceDelta?: EsimSpaceDelta) {
  const normalizedAidHex = normalizeAidHex(aidHex)
  const message = describeSpaceDelta(spaceDelta)
  if (!normalizedAidHex || !message) return
  clearRecentSpaceDelta()
  recentSpaceDelta.value = { aidHex: normalizedAidHex, message }
  recentSpaceDeltaTimer = window.setTimeout(() => {
    recentSpaceDelta.value = null
    recentSpaceDeltaTimer = null
  }, 75000)
}

async function fetchNotifications() {
  notificationsLoading.value = true
  const result = await devicesService.getEsimNotifications(props.deviceId)
  try {
    if (!result.ok) throw result.error
    notifications.value = result.data
  } catch (e: unknown) {
    ElMessage.error(errorMessage(e, '获取当前通知列表失败'))
  } finally {
    notificationsLoading.value = false
  }
}

async function fetchProfilePhones() {
  const result = await devicesService.getSMSProfiles()
  if (!result.ok) return
  const next: Record<string, string> = {}
  for (const profile of result.data.profiles) {
    if (profile.device_id === props.deviceId && profile.iccid) {
      next[profile.iccid] = String(profile.phone_number || '')
    }
  }
  profilePhones.value = next
}

async function openNotificationsDialog() {
  notificationsDialogOpen.value = true
  await fetchNotifications()
}

async function retryNotification(item: EsimNotificationItem) {
  if (!item.can_retry || retryingNotificationSequence.value !== null) return
  retryingNotificationSequence.value = item.sequence_number
  const result = await devicesService.retryEsimNotification(props.deviceId, item.sequence_number, item.aid_hex)
  try {
    if (!result.ok) throw result.error
    retryingNotificationSequence.value = null
    ElMessage.success(result.data.message)
    const refreshed = await devicesService.getEsimNotifications(props.deviceId)
    if (!refreshed.ok) {
      ElMessage.warning(refreshed.error.message || '通知已发送，但刷新通知列表失败')
      return
    }
    const nextState = reconcileEsimNotificationDialogState({
      isOpen: notificationsDialogOpen.value,
      items: notifications.value,
      refreshedItems: refreshed.data,
      retriedSequenceNumber: item.sequence_number
    })
    notificationsDialogOpen.value = nextState.isOpen
    notifications.value = nextState.items
    retryingNotificationSequence.value = nextState.retryingSequenceNumber
  } catch (e: unknown) {
    const nextState = reconcileEsimNotificationDialogState({
      isOpen: notificationsDialogOpen.value,
      items: notifications.value,
      refreshedItems: notifications.value,
      retriedSequenceNumber: null
    })
    notificationsDialogOpen.value = nextState.isOpen
    notifications.value = nextState.items
    retryingNotificationSequence.value = nextState.retryingSequenceNumber
    ElMessage.error(errorMessage(e, '通知重试发送失败'))
  }
}

// 获取 eSIM 总览数据
async function fetchOverview(refresh = false) {
  if (refresh && profilesRefreshing.value) return
  fetchOverviewRequestId += 1
  const requestId = fetchOverviewRequestId

  if (fetchAbortController) {
    fetchAbortController.abort()
  }
  const controller = new AbortController()
  fetchAbortController = controller

  if (refresh) {
    profilesRefreshing.value = true
  } else {
    loading.value = true
  }

  const currentAidHex = downloadForm.value.aidHex
  const result = await devicesService.getEsimOverview(props.deviceId, {
    refresh,
    signal: controller.signal
  })
  let shouldResetLoading = true
  try {
    if (requestId !== fetchOverviewRequestId) {
      shouldResetLoading = false
      return
    }
    if (!result.ok) throw result.error
    chipInfo.value = result.data.chipInfo
    profiles.value = result.data.profiles
    void fetchProfilePhones()
    downloadForm.value.aidHex = pickAvailableDownloadAid(currentAidHex)
  } catch (e: unknown) {
    if (result.ok === false && result.error.code === 'ERR_CANCELED') {
      return
    }
    ElMessage.error(errorMessage(e, '获取 eSIM 信息失败'))
  } finally {
    if (shouldResetLoading) {
      if (refresh) {
        profilesRefreshing.value = false
      } else {
        loading.value = false
      }
    }
  }
}

async function fetchProfiles(refresh = false) {
  profilesRefreshing.value = true
  const result = await devicesService.getEsimProfiles(props.deviceId, { refresh })
  try {
    if (!result.ok) throw result.error
    profiles.value = result.data
    void fetchProfilePhones()
  } catch (e: unknown) {
    ElMessage.error(errorMessage(e, '获取 eSIM Profiles 失败'))
  } finally {
    profilesRefreshing.value = false
  }
}

function applyOptimisticActive(targetICCID: string, aidHex: string) {
  profiles.value = applyOptimisticActiveState(profiles.value, targetICCID, aidHex)
}

// 切换 profile（启用/禁用）
async function switchProfile(iccid: string, currentState: number, aidHex: string) {
  const action = currentState === 1 ? '禁用' : '启用'
  const confirmed = await ElMessageBox.confirm(
    `确定要${action}此 Profile (${iccid}) 吗？切换后设备会短暂断网。`,
    `${action} Profile`,
    { confirmButtonText: action, cancelButtonText: '取消', type: 'warning' }
  ).then(() => true).catch(() => false)
  if (!confirmed) return

  switching.value = iccid
  try {
    const result = await devicesService.switchEsimProfile(props.deviceId, {
      iccid,
      aid_hex: aidHex
    })
    if (!result.ok) throw new Error(result.error.message || `${action}失败`)
    ElMessage.success(`Profile ${action}成功`)
    applyOptimisticActive(iccid, aidHex)
  } catch (e: unknown) {
    ElMessage.error(errorMessage(e, `${action}失败`))
  } finally {
    switching.value = null
  }
}

// 开始编辑名称
function startRename(iccid: string, currentName: string) {
  renaming.value = iccid
  renameValue.value = currentName
}

// 保存名称
async function saveRename(iccid: string, aidHex: string) {
  const name = renameValue.value.trim()
  if (!name) {
    ElMessage.warning('名称不能为空')
    return
  }
  try {
    const result = await devicesService.renameEsimProfile(props.deviceId, iccid, { name, aid_hex: aidHex })
    if (!result.ok) throw new Error(result.error.message || '修改名称失败')
    ElMessage.success('名称修改成功')
    renaming.value = null
    await fetchProfiles(true)
  } catch (e: unknown) {
    ElMessage.error(errorMessage(e, '修改名称失败'))
  }
}

// 取消编辑
function cancelRename() {
  renaming.value = null
  renameValue.value = ''
}

async function editProfilePhone(iccid: string, profileName: string) {
  if (editingPhone.value) return
  editingPhone.value = iccid
  try {
    const currentResult = await devicesService.getEsimProfilePhone(iccid)
    if (!currentResult.ok) throw currentResult.error
    const { value } = await ElMessageBox.prompt(
      '请输入包含国家或地区代码的完整号码；留空并保存可清除人工号码。',
      `设置本机号码 · ${profileName || iccid}`,
      {
        confirmButtonText: '保存',
        cancelButtonText: '取消',
        inputValue: currentResult.data,
        inputPlaceholder: '例如 +85212345678',
        inputValidator: input => {
          const phone = String(input || '').trim()
          if (!phone) return true
          return /^\+?[0-9]{5,20}$/.test(phone) || '请输入 5-20 位数字，可使用 + 开头'
        }
      }
    )
    const saveResult = await devicesService.setEsimProfilePhone(iccid, String(value || '').trim())
    if (!saveResult.ok) throw saveResult.error
    profilePhones.value = { ...profilePhones.value, [iccid]: saveResult.data }
    ElMessage.success(saveResult.data ? '本机号码已保存' : '人工本机号码已清除')
  } catch (e: unknown) {
    const message = errorMessage(e, '')
    if (message && !message.toLowerCase().includes('cancel')) {
      ElMessage.error(errorMessage(e, '设置本机号码失败'))
    }
  } finally {
    editingPhone.value = null
  }
}

// 删除 profile（需要输入 ICCID 后 4 位确认）
async function deleteProfile(iccid: string, name: string, aidHex: string) {
  const last4 = iccid.slice(-4)
  const { value: input } = await ElMessageBox.prompt(
    `此操作不可逆！请输入 ICCID 后 4 位「${last4}」以确认删除 Profile「${name}」`,
    '⚠️ 删除 Profile',
    {
      confirmButtonText: '确认删除',
      cancelButtonText: '取消',
      inputPattern: new RegExp(`^${last4}$`),
      inputErrorMessage: `请输入 ${last4} 以确认`,
      inputPlaceholder: `输入 ${last4}`,
      type: 'error',
      confirmButtonClass: '!bg-red-600 !border-red-600 hover:!bg-red-700'
    }
  ).catch(() => ({ value: '' }))
  if (input !== last4) return

  deleting.value = iccid
  try {
    const result = await devicesService.deleteEsimProfile(props.deviceId, iccid, aidHex)
    if (!result.ok) throw new Error(result.error.message || '删除失败')
    showRecentSpaceDelta(aidHex, result.data.space_delta)
    const notice = describeDeleteResultNotice(result.data)
    if (notice.tone === 'warning') {
      ElMessage.warning(notice.message)
    } else {
      ElMessage.success(notice.message)
    }
    await fetchOverview(true)
  } catch (e: unknown) {
    ElMessage.error(errorMessage(e, '删除失败'))
  } finally {
    deleting.value = null
  }
}

// 下载新 profile（SSE 流式进度）
async function downloadProfile() {
  const { smdp, matchingId, confirmationCode, aidHex, imei } = downloadForm.value
  const targetAidHex = aidHex || pickAvailableDownloadAid('')
  if (!smdp) {
    ElMessage.warning('请输入 SM-DP+ 地址')
    return
  }

  downloadSessionId.value++
  downloading.value = true
  downloadProgress.value = 0
  downloadMsg.value = '正在连接...'
  downloadError.value = ''

  const params = new URLSearchParams({ smdp })
  if (matchingId) params.set('matching_id', matchingId)
  if (confirmationCode) params.set('confirmation_code', confirmationCode)
  if (targetAidHex) params.set('aid_hex', targetAidHex)
  if (imei.trim()) params.set('imei', imei.trim())

  const base = api.defaults.baseURL || ''
  const url = `${base}/devices/${props.deviceId}/esim/actions/download?${params}`
  const token = localStorage.getItem('token') || ''
  const controller = new AbortController()

  try {
    const res = await fetch(url, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' },
      signal: controller.signal
    })
    if (!res.ok) {
      const text = await res.text()
      throw new Error(text || `HTTP ${res.status}`)
    }
    if (!res.body) throw new Error('No stream body')

    const reader = res.body.getReader()
    const decoder = new TextDecoder('utf-8')
    let buffer = ''

    outer: while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })

      while (true) {
        const nl = buffer.indexOf('\n')
        if (nl < 0) break
        let line = buffer.slice(0, nl)
        buffer = buffer.slice(nl + 1)
        if (line.endsWith('\r')) line = line.slice(0, -1)
        if (!line.startsWith('data:')) continue

        const payload = line.slice('data:'.length).trim()
        try {
          const evt = JSON.parse(payload) as { step: string; msg: string; pct: number; code?: string; warning?: string; space_delta?: EsimSpaceDelta }
          if (evt.step === 'error') {
            downloadError.value = evt.code === 'euicc_insufficient_memory'
              ? 'eUICC 安装 profile 时空间不足，请删除未使用的 profile 后重试。'
              : evt.msg
            break outer
          }
          downloadProgress.value = evt.pct
          downloadMsg.value = evt.msg
          if (evt.step === 'done') {
            showRecentSpaceDelta(targetAidHex, evt.space_delta)
            const notice = describeDownloadTerminalNotice(evt)
            if (notice.tone === 'warning') {
              ElMessage.warning(notice.message)
            } else {
              ElMessage.success(notice.message)
            }
            downloadForm.value = { smdp: '', matchingId: '', confirmationCode: '', aidHex: targetAidHex, imei }
            await fetchOverview(true)
            break outer
          }
        } catch { /* 非 JSON 行，忽略 */ }
      }
    }
  } catch (e: unknown) {
    if (!downloadError.value) {
      downloadError.value = errorMessage(e, '下载失败')
    }
  } finally {
    downloading.value = false
  }
}

// 切换设备或改换 tab 时重新获取数据
watch(
  [() => props.deviceId, () => props.isActive],
  ([newId, newActive]) => {
    if (fetchAbortController) {
      fetchAbortController.abort()
    }
    expandedPolicyIccid.value = null
    if (!newId || !newActive) return

    clearRecentSpaceDelta()
    chipInfo.value = null
    profiles.value = []
    downloadForm.value.aidHex = ''
    applyDeviceImeiDefault(true)
    fetchOverview()
  },
  { immediate: true }
)

watch(() => props.deviceImei, () => {
  applyDeviceImeiDefault(false)
})

onBeforeUnmount(() => {
  clearRecentSpaceDelta()
  if (fetchAbortController) {
    fetchAbortController.abort()
  }
})
</script>

<template>
  <div class="space-y-5">
      <!-- 芯片信息 -->
      <div v-if="chipInfo || profiles.length > 0" class="ui-panel-muted p-4 relative">
      <div class="flex items-center justify-between gap-3 mb-3">
        <div class="flex items-center gap-3 min-w-0">
          <div class="w-9 h-9 rounded-xl bg-gradient-to-br from-[#2557ca] to-[#1947ad] text-white text-xs font-bold flex items-center justify-center shadow-lg shadow-blue-700/25">
            ESIM
          </div>
          <div>
            <div class="text-base font-bold text-gray-900 dark:text-white">
              {{ chipInfo?.sku_name || 'eUICC' }}
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400 font-mono">
              <span>固件 {{ chipInfo?.firmware || '--' }}</span>
              <template v-if="chipInfo?.serial_number">
                · SN: <span :class="{ 'select-none': !showSensitive }">{{ showSensitive ? chipInfo.serial_number : SENSITIVE_VALUE_PLACEHOLDER }}</span>
              </template>
            </div>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <el-tooltip content="手动刷新" placement="top">
            <el-button circle text :aria-busy="profilesRefreshing ? 'true' : 'false'" @click="fetchOverview(true)">
              <el-icon size="18" :class="{ 'refresh-icon-spinning': profilesRefreshing }"><ArrowSync24Regular /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip content="当前通知" placement="top">
            <el-button circle text :loading="notificationsLoading" @click="openNotificationsDialog">
              <el-icon v-if="shouldShowEsimNotificationIcon(notificationsLoading)" size="18"><Alert24Regular /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip :content="showSensitive ? '隐藏敏感信息' : '显示敏感信息'" placement="top">
            <el-button circle text @click="showSensitive = !showSensitive">
              <el-icon size="18">
                <Eye24Regular v-if="showSensitive" />
                <EyeOff24Regular v-else />
              </el-icon>
            </el-button>
          </el-tooltip>
        </div>
      </div>
    </div>

      <!-- 按 eUICC 分组的 Profiles -->
      <div v-for="(group, gi) in profiles" :key="group.aid_hex || group.eid || ('group-' + gi)" class="ui-panel-muted overflow-hidden">
      <!-- eUICC 头部 -->
      <div class="px-4 py-3 border-b border-gray-100 dark:border-white/10">
        <div class="flex items-center justify-between">
          <div>
            <span class="text-sm font-bold text-gray-900 dark:text-white">eUICC #{{ gi + 1 }}</span>
            <span class="text-xs text-gray-400 font-mono ml-2" :class="{ 'select-none': !showSensitive }">
              {{ showSensitive ? group.eid : SENSITIVE_VALUE_PLACEHOLDER }}
            </span>
          </div>
          <div v-if="chipInfo?.eids" class="text-xs text-gray-500">
            <template v-for="eid in chipInfo.eids" :key="eid.eid">
              <span v-if="eid.eid === group.eid" class="inline-flex flex-col items-end gap-1">
                <span class="inline-flex items-center gap-1">
                  <span class="w-2 h-2 rounded-full" :class="eid.free_nvram_bytes > 100000 ? 'bg-green-500' : 'bg-yellow-500'" />
                  可用 {{ eid.free_nvram }}
                </span>
                <span v-if="recentSpaceDelta && normalizeAidHex(group.aid_hex) === recentSpaceDelta.aidHex" class="text-[11px] text-emerald-600 dark:text-emerald-400">
                  {{ recentSpaceDelta.message }}
                </span>
              </span>
            </template>
          </div>
        </div>
        <!-- PKI 信息行 -->
        <template v-if="chipInfo?.eids">
          <template v-for="eid in chipInfo.eids" :key="'pki-' + eid.eid">
            <div v-if="eid.eid === group.eid && (eid.manufacturer || eid.certificates?.length || eid.default_smdp_address || eid.root_ds_address || eid.sas_accreditation_number || eid.info_source)" class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-gray-400 dark:text-gray-500">
              <span v-if="eid.manufacturer" class="inline-flex items-center gap-1">
                <span class="text-[10px]">生产商:</span> {{ eid.manufacturer }}
              </span>
              <span v-if="eid.certificates?.length" class="inline-flex items-center gap-1">
                <span class="text-[10px]">证书:</span> {{ eid.certificates.join(' · ') }}
              </span>
              <span v-if="eid.default_smdp_address" class="inline-flex items-center gap-1">
                <span class="text-[10px]">Default SM-DP+:</span> {{ eid.default_smdp_address }}
              </span>
              <span v-if="eid.root_ds_address" class="inline-flex items-center gap-1">
                <span class="text-[10px]">Root SM-DS:</span> {{ eid.root_ds_address }}
              </span>
              <span v-if="eid.sas_accreditation_number" class="inline-flex items-center gap-1">
                <span class="text-[10px]">SAS:</span> {{ eid.sas_accreditation_number }}
              </span>
              <span v-if="eid.info_source" class="inline-flex items-center gap-1">
                <span class="text-[10px]">来源:</span> {{ eid.info_source }}
              </span>
            </div>
          </template>
        </template>
      </div>

      <!-- Profile 列表 -->
      <div v-if="group.profiles?.length === 0" class="p-4 text-sm text-gray-400">
        暂无 Profile
      </div>
      <div v-else class="divide-y divide-gray-100 dark:divide-white/10">
        <template v-for="p in group.profiles" :key="p.iccid">
        <div
          class="px-4 py-3 flex items-center justify-between gap-3 hover:bg-gray-50/50 dark:hover:bg-white/5 transition-colors"
        >
          <div class="min-w-0 flex-1">
            <!-- 正常显示模式 -->
            <template v-if="renaming !== p.iccid">
              <div class="flex items-center gap-2">
                <span class="w-2 h-2 rounded-full flex-shrink-0" :class="p.state === 1 ? 'bg-green-500' : 'bg-gray-300 dark:bg-gray-600'" />
                <span class="font-medium text-sm text-gray-900 dark:text-white truncate">
                  {{ p.name || (showSensitive ? p.iccid : SENSITIVE_VALUE_PLACEHOLDER) }}
                </span>
                <el-tag size="small" :type="p.state === 1 ? 'success' : 'info'" class="flex-shrink-0">
                  {{ p.state_text }}
                </el-tag>
              </div>
              <div class="text-xs text-gray-500 dark:text-gray-400 mt-0.5 ml-4 flex flex-wrap items-center gap-x-2 gap-y-1 transition-all">
                <span>{{ p.service_provider_name }}</span>
                <span :class="{ 'select-none': !showSensitive }">{{ showSensitive ? p.iccid : SENSITIVE_VALUE_PLACEHOLDER }}</span>
                <span v-if="profilePhones[p.iccid]" :class="{ 'select-none': !showSensitive }">
                  本机号 {{ showSensitive ? profilePhones[p.iccid] : SENSITIVE_VALUE_PLACEHOLDER }}
                </span>
              </div>
            </template>
            <!-- 编辑名称模式 -->
            <template v-else>
              <div class="flex items-center gap-2">
                <el-input
                  v-model="renameValue"
                  size="small"
                  placeholder="输入新名称"
                  @keyup.enter="saveRename(p.iccid, group.aid_hex)"
                  @keyup.escape="cancelRename"
                  autofocus
                  class="!w-52"
                />
                <el-button size="small" type="primary" @click="saveRename(p.iccid, group.aid_hex)" class="!border-0">保存</el-button>
                <el-button size="small" @click="cancelRename" class="!border-0">取消</el-button>
              </div>
            </template>
          </div>

          <!-- 操作按钮 -->
          <div v-if="renaming !== p.iccid" class="flex items-center gap-2 flex-shrink-0">
            <el-button
              v-if="p.state !== 1"
              size="small"
              type="success"
              :loading="switching === p.iccid"
              @click="switchProfile(p.iccid, p.state, group.aid_hex)"
              plain
            >
              切换
            </el-button>
            <el-button
              size="small"
              type="primary"
              @click="startRename(p.iccid, p.name)"
              plain
            >
              改名
            </el-button>
            <el-button
              size="small"
              :loading="editingPhone === p.iccid"
              @click="editProfilePhone(p.iccid, p.name)"
              plain
            >
              号码
            </el-button>
            <el-button
              size="small"
              :type="expandedPolicyIccid === p.iccid ? 'primary' : 'default'"
              @click="togglePolicyPanel(p.iccid)"
              plain
            >
              策略
            </el-button>
            <el-button
              size="small"
              type="danger"
              :loading="deleting === p.iccid"
              @click="deleteProfile(p.iccid, p.name, group.aid_hex)"
              plain
            >
              删除
            </el-button>
          </div>
        </div>
          <div v-if="expandedPolicyIccid === p.iccid" class="px-4 pb-3 border-t-0">
            <EsimCardPolicyInline
              :device-id="props.deviceId"
              :iccid="p.iccid"
              :is-active-card="p.state === 1"
              :device-online="props.deviceOnline === true"
              @policy-changed="fetchOverview(true)"
            />
          </div>
        </template>
      </div>
    </div>

      <el-dialog
        v-model="notificationsDialogOpen"
        title="当前通知列表"
        :width="notificationDialogWidth()"
        class="glass-modal"
      >
        <div v-if="notificationsLoading" class="py-10 text-sm text-center text-gray-400">正在加载通知...</div>
        <div v-else-if="notifications.length === 0" class="py-10 text-sm text-center text-gray-400">当前没有可展示的通知</div>
        <div v-else class="space-y-2 max-h-[420px] overflow-auto pr-1">
          <div
            v-for="item in notifications"
            :key="item.sequence_number"
            :class="notificationListItemLayoutClass()"
          >
            <div class="min-w-0 flex-1 space-y-1">
              <div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
                <span>#{{ item.sequence_number }}</span>
                <el-tag size="small" type="info">{{ formatEsimNotificationEvent(item.event) }}</el-tag>
              </div>
              <div :class="notificationMetaContainerClass()">
                <div v-if="item.iccid" :class="notificationMetaItemClass()">
                  <span class="mr-1 text-gray-400 dark:text-gray-500">ICCID</span>
                  <span class="break-all">{{ item.iccid }}</span>
                </div>
                <div v-if="item.address" :class="notificationMetaItemClass()">
                  <span class="mr-1 text-gray-400 dark:text-gray-500">地址</span>
                  <span class="break-all">{{ item.address }}</span>
                </div>
                <div v-if="item.aid_hex" :class="notificationMetaItemClass()">
                  <span class="mr-1 text-gray-400 dark:text-gray-500">AID</span>
                  <span class="break-all">{{ item.aid_hex }}</span>
                </div>
              </div>
            </div>
            <el-button
              size="small"
              type="danger"
              plain
              class="self-start sm:self-auto"
              :disabled="!item.can_retry"
              :loading="retryingNotificationSequence === item.sequence_number"
              @click="retryNotification(item)"
            >
              重发
            </el-button>
          </div>
        </div>
      </el-dialog>

      <!-- 下载新 Profile -->
      <div v-if="chipInfo || profiles.length > 0" class="ui-panel-muted p-4">
      <div class="flex items-center gap-2 mb-3">
        <div class="w-7 h-7 rounded-lg bg-primary-50 dark:bg-primary-500/10 flex items-center justify-center text-primary-600 dark:text-primary-400">
          <el-icon size="16"><Add24Regular /></el-icon>
        </div>
        <div class="text-sm font-bold text-gray-900 dark:text-white">下载新 Profile</div>
      </div>
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-3">
        <div class="space-y-1">
          <div class="text-[11px] font-bold text-gray-500 uppercase tracking-wider">SM-DP+ 地址 *</div>
          <el-input v-model="downloadForm.smdp" placeholder="例如 rsp.truphone.com" />
        </div>
        <div class="space-y-1">
          <div class="text-[11px] font-bold text-gray-500 uppercase tracking-wider">Matching ID</div>
          <el-input v-model="downloadForm.matchingId" placeholder="可选" />
        </div>
        <div class="space-y-1">
          <div class="text-[11px] font-bold text-gray-500 uppercase tracking-wider">确认码</div>
          <el-input v-model="downloadForm.confirmationCode" placeholder="可选" />
        </div>
        <div class="space-y-1">
          <div class="text-[11px] font-bold text-gray-500 uppercase tracking-wider">IMEI</div>
          <el-input v-model="downloadForm.imei" maxlength="15" placeholder="默认使用设备 IMEI，可修改" />
        </div>
        <div class="space-y-1">
          <div class="text-[11px] font-bold text-gray-500 uppercase tracking-wider">目标 eUICC</div>
          <el-select v-model="downloadForm.aidHex" placeholder="选择目标 eUICC">
            <el-option
              v-for="(eid, ei) in downloadTargets"
              :key="eid.aid"
              :label="eid.free_nvram ? `eUICC #${Number(ei) + 1} (...${eid.eid.slice(-4)}) — ${eid.free_nvram} 可用` : `eUICC #${Number(ei) + 1} (...${eid.eid.slice(-4)})`"
              :value="eid.aid"
            />
          </el-select>
        </div>
      </div>
      <!-- 下载进度条 -->
      <div v-if="downloading || downloadError" class="mt-4 space-y-1.5">
        <el-progress
          :key="downloadSessionId"
          :percentage="downloadProgress"
          :status="downloadError ? 'exception' : downloadProgress >= 100 ? 'success' : undefined"
          :striped="downloading && downloadProgress < 100"
          :striped-flow="downloading && downloadProgress < 100"
          :duration="8"
          :stroke-width="10"
        />
        <div class="text-xs" :class="downloadError ? 'text-red-500' : 'text-gray-500 dark:text-gray-400'">
          {{ downloadError || downloadMsg }}
        </div>
      </div>

      <div class="flex justify-end mt-4">
        <el-button type="primary" :loading="downloading" :disabled="downloading" @click="downloadProfile" class="!border-0">
          <el-icon><ArrowDownload24Regular /></el-icon>
          开始下载
        </el-button>
      </div>
    </div>

      <!-- 空状态 -->
      <EmptyState v-if="profiles.length === 0 && !chipInfo && !loading" bare title="未检测到 eUICC" subtitle="此SIM卡可能不支持 eUICC 功能" />
  </div>
</template>
