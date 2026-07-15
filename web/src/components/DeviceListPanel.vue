<script setup lang="ts">
import { computed } from 'vue'
import EmptyState from './EmptyState.vue'
import type { DeviceMgmtListItem } from '../types/api'
import { isControlOnline, isRadioRegistered, isSIMMissing, lifecycleStatusLabel, primaryLifecycleStatus } from '../utils/deviceLifecycle'

const props = defineProps<{
  query: string
  statusFilter: 'all' | 'online' | 'offline'
  sortKey: 'name' | 'signal'
  sortDir: 'asc' | 'desc'
  selectedId: string
  filteredDevices: DeviceMgmtListItem[]
}>()

const emit = defineEmits<{
  'update:query': [value: string]
  'update:statusFilter': [value: 'all' | 'online' | 'offline']
  'update:sortKey': [value: 'name' | 'signal']
  'update:sortDir': [value: 'asc' | 'desc']
  'select-device': [id: string]
}>()

const modelQuery = computed({
  get: () => props.query,
  set: (value: string) => emit('update:query', value)
})

const modelStatusFilter = computed({
  get: () => props.statusFilter,
  set: (value: 'all' | 'online' | 'offline') => emit('update:statusFilter', value)
})



const modelSortKey = computed({
  get: () => props.sortKey,
  set: (value: 'name' | 'signal') => emit('update:sortKey', value)
})

const modelSortDir = computed({
  get: () => props.sortDir,
  set: (value: 'asc' | 'desc') => emit('update:sortDir', value)
})

const primaryStatus = primaryLifecycleStatus

const statusTagClass = (tone: ReturnType<typeof primaryLifecycleStatus>['tone']) => {
  switch (tone) {
    case 'success':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300'
    case 'warning':
      return 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300'
    case 'danger':
      return 'border-red-200 bg-red-50 text-red-700 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-300'
  }
}

const registrationText = (d: DeviceMgmtListItem) => {
	if (isSIMMissing(d)) return '未插卡'
	if (isRadioRegistered(d)) {
		const network = [d?.modem?.network_duplex, d?.modem?.network_mode].filter(Boolean).join(' ') || '--'
		const publicIP = String(d?.public_ip || '').trim()
		return [d?.modem?.operator || '--', network, publicIP].filter(Boolean).join(' · ')
	}
	const phaseText = lifecycleStatusLabel(d.lifecycle_phase)
	if (phaseText && d.lifecycle_phase !== 'online' && d.lifecycle_phase !== 'offline') return phaseText
  if (!isControlOnline(d)) return '控制面恢复中'
  if (d.registration_state_label === 'searching') return '搜索网络中'
  if (d.registration_state_label === 'denied') return '驻网被拒'
  return '未驻网'
}

const dataNetworkText = (d: DeviceMgmtListItem) => {
  if (d?.vowifi_enabled) return ''
  if (d?.network_connected || String(d?.public_ip || '').trim() || String(d?.public_ipv6 || '').trim()) return ''
  if (!d?.network_enabled) return '数据未开启'
  if (!d?.network_connected) return '数据网络未连接'
  return ''
}

const secondaryStatus = (d: DeviceMgmtListItem) => {
  if (isSIMMissing(d)) return '未插卡'
  if (d?.vowifi_enabled) return vowifiStatusText(d)
  return [registrationText(d), dataNetworkText(d)].filter(Boolean).join(' · ')
}

const vowifiStatusText = (d: DeviceMgmtListItem) => {
  const rt = d?.vowifi_runtime
  if (!rt) return 'VoWiFi 启动中'
  if (rt.phase === 'failed') return `VoWiFi 失败${vowifiErrorText(rt.last_error)}`
  if (rt.sms_ready) return 'WiFi-Calling'
  if (rt.ims_ready) return 'IMS 已就绪 · SMS 未就绪'
  if (rt.tunnel_ready) return 'Tunnel 已就绪 · IMS 未就绪'
  if (rt.access_ready || rt.sim_ready) return 'VoWiFi 启动中 · IMS 未就绪'
  return 'VoWiFi 未就绪'
}

const vowifiErrorText = (err?: string) => {
  const text = String(err || '').trim()
  if (!text) return ''
  if (text.includes('epdg tunnel establishment timed out')) return ' · ePDG 隧道超时'
  return ` · ${text}`
}

const devicePathText = (d: DeviceMgmtListItem) => {
  if (d?.interface) return d.interface
  if (d?.at_port) return 'AT'
  if (d?.control_device) return d.control_device
  return ''
}
</script>

<template>
  <div class="ui-card p-5">
    <div class="flex items-center gap-3 mb-4">
      <el-input v-model="modelQuery" placeholder="搜索设备 / ICCID / IMEI / 网卡" />
    </div>

    <div class="grid grid-cols-2 gap-2 mb-4">
      <el-select v-model="modelStatusFilter" size="small" placeholder="在线">
        <el-option label="全部状态" value="all" />
        <el-option label="仅在线" value="online" />
        <el-option label="仅离线" value="offline" />
      </el-select>

      <el-select v-model="modelSortKey" size="small" placeholder="排序">
        <el-option label="排序：名称" value="name" />
        <el-option label="排序：信号" value="signal" />
      </el-select>
      <el-select v-model="modelSortDir" size="small" placeholder="方向" class="col-span-2">
        <el-option label="升序" value="asc" />
        <el-option label="降序" value="desc" />
      </el-select>
    </div>

    <EmptyState v-if="filteredDevices.length === 0" bare title="暂无设备" subtitle="点击右上角“添加设备”开始接管" />

    <div v-else class="device-list-scroll max-h-[65vh] overflow-y-auto pr-1">
      <div class="device-list-grid">
        <div v-for="d in filteredDevices" :key="d.id" class="device-list-item">
          <button
            type="button"
            class="w-full h-full text-left p-3 rounded-xl border transition-all"
            :class="selectedId === d.id
              ? 'border-primary-200 dark:border-primary-500/30 bg-primary-50/70 dark:bg-primary-500/10'
              : 'border-gray-100 dark:border-white/10 hover:bg-gray-50/60 dark:hover:bg-white/5'"
            @click="emit('select-device', d.id)"
          >
            <div class="min-w-0">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0 font-bold text-gray-800 dark:text-gray-100 truncate">{{ d.name || d.id }}</div>
                <span
                  class="inline-flex shrink-0 items-center rounded-md border px-2 py-0.5 text-xs font-medium leading-5"
                  :class="statusTagClass(primaryStatus(d).tone)"
                >
                  {{ primaryStatus(d).label }}
                </span>
              </div>
              <div class="text-xs text-gray-500 mt-0.5 truncate">
                {{ [d.id, devicePathText(d)].filter(Boolean).join(' · ') }}
              </div>
              <div class="text-xs text-gray-400 mt-1 truncate">
                {{ secondaryStatus(d) }}
              </div>
            </div>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.device-list-scroll {
  container-type: inline-size;
}

.device-list-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.5rem;
}

.device-list-item {
  min-width: 0;
}

@container (min-width: 700px) {
  .device-list-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
