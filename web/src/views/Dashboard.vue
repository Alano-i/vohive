<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useRouter } from 'vue-router'
import DeviceCard from '../components/DeviceCard.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import ErrorState from '../components/ErrorState.vue'
import RefreshButton from '../components/RefreshButton.vue'
import TrafficAnalysisPanel from '../components/TrafficAnalysisPanel.vue'
import { usePollingScheduler } from '../composables/usePollingScheduler'
import { useDashboardStore } from '../stores/dashboard'
import type { TrafficRange } from '../services/traffic'
import {
  ArrowSync24Regular,
  PhoneDesktop24Regular,
  PlugConnected24Regular,
  PlugDisconnected24Regular
} from '@vicons/fluent'

const dashboard = useDashboardStore()
const router = useRouter()
const {
  devices,
  devicesLastOkAt,
  devicesError,
  analysis,
  analysisLoading,
  analysisLastOkAt,
  analysisError
} = storeToRefs(dashboard)

const lastUpdatedAt = ref<number | null>(null)
const refreshing = ref(false)

const analysisRange = ref<TrafficRange>('day')

const totalCount = computed(() => devices.value.length)
const onlineCount = computed(() => devices.value.filter(d => d?.healthy && d?.sim_inserted !== false).length)
const offlineCount = computed(() => Math.max(0, totalCount.value - onlineCount.value))

async function fetchDevices() {
  await dashboard.fetchDevices()
  lastUpdatedAt.value = Date.now()
}

async function refreshDevices() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await fetchDevices()
  } finally {
    refreshing.value = false
  }
}

async function fetchTrafficAnalysis() {
  await dashboard.fetchAnalysis(analysisRange.value)
}

function handleAnalysisRangeChange(range: TrafficRange) {
  if (analysisRange.value === range) return
  analysisRange.value = range
  void fetchTrafficAnalysis()
}

function openDeviceOverview(id: string) {
  const deviceID = String(id || '').trim()
  if (!deviceID) return
  void router.push({
    name: 'Devices',
    query: {
      device: deviceID,
      tab: 'overview'
    }
  })
}

usePollingScheduler(fetchDevices, 5000, {
  immediate: true,
  maxIntervalMs: 30000,
  backgroundIntervalMs: 15000
})
usePollingScheduler(fetchTrafficAnalysis, 60000, {
  immediate: false,
  maxIntervalMs: 300000,
  backgroundIntervalMs: 120000
})

onMounted(() => {
  const win = window as Window & {
    requestIdleCallback?: (cb: IdleRequestCallback, opts?: IdleRequestOptions) => number
  }
  if (typeof win.requestIdleCallback === 'function') {
    win.requestIdleCallback(() => fetchTrafficAnalysis(), { timeout: 1500 })
  } else {
    setTimeout(fetchTrafficAnalysis, 800)
  }
})
</script>

<template>
  <div class="dashboard-page">
    <PageHeader title="运行总览" subtitle="实时掌握蜂窝设备、链路健康度与出口流量">
      <template #actions>
        <RefreshButton :loading="refreshing" @click="refreshDevices" />
      </template>
    </PageHeader>

    <div class="metric-grid grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-4 mb-7">
      <div class="metric-card ui-panel">
        <div class="metric-head">
          <span class="metric-sigil metric-sigil-indigo"><PhoneDesktop24Regular /></span>
          <span>设备总数</span>
        </div>
        <div class="metric-value">{{ totalCount }}</div>
        <div class="metric-foot">CELLULAR NODES</div>
      </div>
      <div class="metric-card ui-panel">
        <div class="metric-head">
          <span class="metric-sigil metric-sigil-green"><PlugConnected24Regular /></span>
          <span>在线设备</span>
        </div>
        <div class="metric-value text-green-600 dark:text-emerald-400">{{ onlineCount }}</div>
        <div class="metric-foot">ACTIVE LINKS</div>
      </div>
      <div class="metric-card ui-panel">
        <div class="metric-head">
          <span class="metric-sigil metric-sigil-red"><PlugDisconnected24Regular /></span>
          <span>离线设备</span>
        </div>
        <div class="metric-value text-red-600 dark:text-rose-400">{{ offlineCount }}</div>
        <div class="metric-foot">ATTENTION</div>
      </div>
      <div class="metric-card ui-panel">
        <div class="metric-head">
          <span class="metric-sigil metric-sigil-cyan"><ArrowSync24Regular /></span>
          <span>最近同步</span>
        </div>
        <div class="metric-time">
          {{ lastUpdatedAt ? new Date(lastUpdatedAt).toLocaleTimeString() : '--:--:--' }}
        </div>
        <div class="metric-foot">AUTO REFRESH / 5S</div>
      </div>
    </div>

    <ErrorState
      v-if="devicesError"
      class="mb-6"
      title="设备列表加载失败"
      :message="devicesError.message"
      :status-code="devicesError.status"
      :request-method="devicesError.method"
      :request-url="devicesError.url"
      :last-success-at="devicesLastOkAt"
      retry-text="重试"
      @retry="fetchDevices"
    />

    <EmptyState
      v-if="devices.length === 0"
      class="dashboard-empty-state"
      title="暂无设备接入"
      subtitle="请先在设备管理中添加或接管设备"
    />

    <!-- Grid View -->
    <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 gap-5">
      <DeviceCard
        v-for="dev in devices"
        :key="dev.id"
        :device="dev"
        @open-device="openDeviceOverview"
      />
    </div>

    <TrafficAnalysisPanel
      class="mt-8"
      :analysis="analysis"
      :loading="analysisLoading"
      :error="analysisError"
      :last-ok-at="analysisLastOkAt"
      :range="analysisRange"
      mode="global"
      @update:range="handleAnalysisRangeChange"
      @refresh="fetchTrafficAnalysis"
    />
  </div>
</template>

<style scoped>
.dashboard-page {
  width: 100%;
}

.metric-card {
  position: relative;
  min-height: 138px;
  overflow: hidden;
  padding: 18px 19px 16px;
}

.metric-head {
  display: flex;
  align-items: center;
  gap: 9px;
  color: var(--vh-text-muted);
  font-size: 11px;
  font-weight: 650;
}

.metric-sigil {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  place-items: center;
  border-radius: 10px;
}

.metric-sigil svg {
  width: 18px;
  height: 18px;
}

.metric-sigil-indigo { color: var(--vh-accent); background: var(--vh-accent-soft); }
.metric-sigil-green { color: var(--vh-positive); background: color-mix(in srgb, var(--vh-positive) 11%, transparent); }
.metric-sigil-red { color: var(--vh-danger); background: color-mix(in srgb, var(--vh-danger) 10%, transparent); }
.metric-sigil-cyan { color: var(--vh-cyan); background: color-mix(in srgb, var(--vh-cyan) 10%, transparent); }

.metric-value {
  position: relative;
  z-index: 1;
  margin-top: 12px;
  font-size: 32px;
  font-weight: 760;
  line-height: 1;
  letter-spacing: -0.055em;
}

.metric-value,
.metric-time {
  font-family: "VoHive Number", "SFMono-Regular", ui-monospace, monospace;
  font-variant-numeric: tabular-nums;
}

.metric-time {
  position: relative;
  z-index: 1;
  margin-top: 16px;
  color: var(--vh-text);
  font-size: 18px;
  font-weight: 650;
  letter-spacing: -0.04em;
}

.metric-foot {
  position: relative;
  z-index: 1;
  margin-top: 13px;
  color: var(--vh-text-soft);
  font-size: 8px;
  font-weight: 700;
  letter-spacing: 0.13em;
}

@media (max-width: 480px) {
  .metric-card {
    min-height: 124px;
    padding: 15px;
  }

  .metric-head { gap: 7px; font-size: 10px; }
  .metric-sigil { width: 28px; height: 28px; flex-basis: 28px; }
  .metric-sigil svg { width: 16px; height: 16px; }
  .metric-value { font-size: 28px; }
  .metric-time { font-size: 15px; }
  .metric-foot { font-size: 7px; letter-spacing: 0.09em; }
}
</style>
