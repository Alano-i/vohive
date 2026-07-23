<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import LoadingScreen from '../components/LoadingScreen.vue'
import ErrorBoundary from '../components/ErrorBoundary.vue'
import SwitchDark from '../components/SwitchDark.vue'
import { debugCollector } from '../debug/collector'
import {
  Mail24Regular,
  Alert24Regular,
  Settings24Regular,
  SignOut24Regular,
  Board24Regular,
  Phone24Regular,
  Globe24Regular,
  DocumentText24Regular
} from '@vicons/fluent'

defineProps({
  isDark: {
    type: Boolean,
    required: true
  }
})

const emit = defineEmits(['toggle-theme'])
const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const isMobile = ref(false)
const debugOpen = ref(false)
const DebugPanel = defineAsyncComponent(() => import('../components/DebugPanel.vue'))

const menuItems = [
  { index: '/', label: '仪表盘', shortLabel: '仪表', icon: Board24Regular },
  { index: '/devices', label: '设备管理', shortLabel: '设备', icon: Phone24Regular },
  { index: '/proxy', label: '代理管理', shortLabel: '代理', icon: Globe24Regular },
  { index: '/sms', label: '短信中心', shortLabel: '短信', icon: Mail24Regular },
  { index: '/notifications', label: '通知中心', shortLabel: '通知', icon: Alert24Regular },
  { index: '/logs', label: '实时日志', shortLabel: '日志', icon: DocumentText24Regular },
  { index: '/settings', label: '系统设置', shortLabel: '设置', icon: Settings24Regular }
]

const activePath = computed(() => route.path)
const currentItem = computed(() => menuItems.find(item => item.index === activePath.value) || menuItems[0])

async function handleLogout() {
  const { ElMessageBox } = await import('element-plus')
  const confirmed = await ElMessageBox.confirm('确认退出当前控制台？', '退出登录', {
    confirmButtonText: '退出',
    cancelButtonText: '取消',
    type: 'warning'
  })
    .then(() => true)
    .catch(() => false)
  if (!confirmed) return
  auth.logout()
  router.push('/login')
}

function syncIsMobile() {
  if (typeof window === 'undefined') return
  isMobile.value = window.matchMedia('(max-width: 767px)').matches
}

function onKeydown(e: KeyboardEvent) {
  if (e.ctrlKey && e.shiftKey && String(e.key || '').toLowerCase() === 'd') {
    e.preventDefault()
    debugOpen.value = !debugOpen.value
    localStorage.setItem('debug_panel_open', debugOpen.value ? '1' : '0')
  }
}

onMounted(() => {
  syncIsMobile()
  window.addEventListener('resize', syncIsMobile, { passive: true })
  debugOpen.value = localStorage.getItem('debug_panel_open') === '1'
  window.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  window.removeEventListener('resize', syncIsMobile)
  window.removeEventListener('keydown', onKeydown)
})

watch(debugOpen, value => {
  localStorage.setItem('debug_panel_open', value ? '1' : '0')
})

watch(
  () => debugCollector.openPanelRequestAt.value,
  ts => {
    if (ts) debugOpen.value = true
  }
)
</script>

<template>
  <el-container v-if="auth.isAuthenticated && route.name !== 'Login'" class="app-shell h-full">
    <el-aside
      v-if="!isMobile"
      width="252px"
      class="desktop-sidebar relative h-full"
    >
      <div class="sidebar-ambient" />

      <div class="sidebar-brand">
        <div class="brand-mark" aria-hidden="true">
          <span class="brand-glyph">V</span>
          <span class="brand-signal" />
        </div>
        <div class="min-w-0">
          <div class="brand-name">VoHive</div>
          <div class="brand-caption">CONTROL CENTER</div>
        </div>
      </div>

      <el-menu
        :default-active="activePath"
        class="sidebar-menu !border-0 !bg-transparent"
        router
      >
        <el-menu-item v-for="item in menuItems" :key="item.index" :index="item.index">
          <el-icon><component :is="item.icon" /></el-icon>
          <template #title>
            <span class="sidebar-menu-copy">
              <span class="sidebar-menu-label">{{ item.label }}</span>
            </span>
          </template>
        </el-menu-item>
      </el-menu>

      <div class="sidebar-footer px-4">
        <div class="system-card">
          <div class="system-card-topline">
            <span class="system-live-dot"><i /></span>
            <span class="system-status-label">系统在线</span>
            <span class="system-latency">LIVE</span>
          </div>
          <div class="system-user-row">
            <div class="user-avatar">A</div>
            <div class="min-w-0 flex-1">
              <div class="user-name">Administrator</div>
              <div class="user-role">Local console</div>
            </div>
            <button type="button" class="logout-button" aria-label="退出登录" @click="handleLogout">
              <SignOut24Regular />
            </button>
          </div>
        </div>
      </div>
    </el-aside>

    <el-container class="workspace-shell h-full min-w-0">
      <el-header class="topbar">
        <div class="topbar-left">
          <div v-if="isMobile" class="mobile-brand-lockup">
            <div class="brand-mark brand-mark-small" aria-hidden="true">
              <span class="brand-glyph">V</span>
              <span class="brand-signal" />
            </div>
            <div>
              <div class="mobile-brand-name">VoHive</div>
              <div class="mobile-route-name">{{ currentItem.label }}</div>
            </div>
          </div>

          <div v-else class="route-context">
            <span class="route-kicker">VOHIVE</span>
            <span class="route-divider">/</span>
            <span class="route-name">{{ currentItem.label }}</span>
          </div>
        </div>

        <div class="topbar-actions">
          <SwitchDark :is-dark="isDark" @toggle="(e) => emit('toggle-theme', e)" />
        </div>
      </el-header>

      <el-main class="app-main overflow-auto">
        <div class="main-inner">
          <router-view v-slot="{ Component, route: viewRoute }">
            <ErrorBoundary v-if="Component" title="页面渲染失败">
              <component :is="Component" :key="String(viewRoute.name || viewRoute.path)" />
            </ErrorBoundary>
            <LoadingScreen v-else title="正在加载页面…" subtitle="正在准备控制台资源" />
          </router-view>
        </div>
      </el-main>
    </el-container>

    <nav v-if="isMobile" class="mobile-tabbar" aria-label="主导航">
      <button
        v-for="item in menuItems"
        :key="item.index"
        type="button"
        class="mobile-tab"
        :class="{ 'is-active': activePath === item.index }"
        :aria-current="activePath === item.index ? 'page' : undefined"
        @click="router.push(item.index)"
      >
        <span class="mobile-tab-icon"><component :is="item.icon" /></span>
        <span class="mobile-tab-label">{{ item.shortLabel }}</span>
      </button>
    </nav>

    <DebugPanel v-model="debugOpen" />
  </el-container>
</template>

<style scoped>
.desktop-sidebar {
  z-index: 20;
  overflow: hidden;
  border-right: 1px solid rgba(151, 181, 221, 0.1);
  background:
    linear-gradient(180deg, rgba(17, 26, 44, 0.98), rgba(8, 14, 26, 0.985)),
    #0a1120;
  color: var(--vh-text);
  box-shadow: 12px 0 40px rgba(13, 25, 45, 0.08);
}

.sidebar-ambient {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background:
    radial-gradient(circle at 8% 0%, rgba(37, 87, 202, 0.22), transparent 22rem),
    linear-gradient(rgba(111, 150, 201, 0.038) 1px, transparent 1px),
    linear-gradient(90deg, rgba(111, 150, 201, 0.038) 1px, transparent 1px);
  background-size: auto, 28px 28px, 28px 28px;
  mask-image: linear-gradient(to bottom, #000, transparent 86%);
}

.sidebar-brand {
  position: relative;
  z-index: 1;
  display: flex;
  height: 88px;
  align-items: center;
  gap: 12px;
  padding: 0 21px;
}

.brand-mark {
  position: relative;
  display: grid;
  width: 40px;
  height: 40px;
  flex: 0 0 auto;
  place-items: center;
  overflow: hidden;
  border: 1px solid rgba(111, 152, 245, 0.26);
  border-radius: 13px;
  background: linear-gradient(145deg, var(--vh-accent), var(--vh-accent-strong));
  box-shadow: 0 10px 26px rgba(37, 87, 202, 0.36), inset 0 1px 0 rgba(255, 255, 255, 0.28);
}

.brand-mark::before {
  position: absolute;
  inset: 0;
  content: "";
  background: linear-gradient(130deg, rgba(255, 255, 255, 0.18), transparent 48%);
}

.brand-glyph {
  position: relative;
  color: #ffffff;
  font-size: 17px;
  font-weight: 850;
  letter-spacing: -0.08em;
}

.brand-signal {
  position: absolute;
  top: 7px;
  right: 7px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #61e8ff;
  box-shadow: 0 0 10px #61e8ff;
}

.brand-name {
  color: var(--vh-text);
  font-size: 20px;
  font-weight: 760;
  line-height: 1.05;
  letter-spacing: -0.045em;
}

.brand-caption {
  margin-top: 5px;
  color: var(--vh-text-soft);
  font-size: 8px;
  font-weight: 750;
  letter-spacing: 0.22em;
}

.sidebar-section-label {
  position: relative;
  z-index: 1;
  padding: 12px 22px 7px;
  color: #667893;
  font-size: 9px;
  font-weight: 750;
  letter-spacing: 0.18em;
  text-transform: uppercase;
}

:deep(.sidebar-menu) {
  position: relative;
  z-index: 1;
  --el-menu-text-color: var(--vh-text-muted);
  --el-menu-active-color: var(--vh-accent);
  --el-menu-hover-bg-color: var(--vh-accent-soft);
}

:deep(.sidebar-menu .el-menu-item) {
  height: 52px;
  margin: 4px 11px;
  padding: 0 14px !important;
  border: 1px solid transparent;
  border-radius: 13px;
  color: var(--vh-text-muted);
  line-height: 52px;
  transition: color 180ms ease, border-color 180ms ease, background 180ms ease, box-shadow 180ms ease;
}

:deep(.sidebar-menu .el-menu-item::before) {
  position: absolute;
  left: -1px;
  width: 2px;
  height: 20px;
  border-radius: 99px;
  content: "";
  opacity: 0;
  background: #61E8FF;
  box-shadow: none;
  transition: opacity 180ms ease;
}

:deep(.sidebar-menu .el-menu-item .el-icon) {
  width: 22px;
  margin-right: 12px;
  color: var(--vh-text-soft);
  font-size: 20px;
}

:deep(.sidebar-menu .el-menu-item:hover) {
  color: var(--vh-text);
  background: var(--vh-accent-soft);
}

:deep(.sidebar-menu .el-menu-item.is-active) {
  border-color: transparent;
  color: var(--vh-accent);
  background: var(--vh-accent-soft);
  box-shadow: none;
}

:deep(.sidebar-menu .el-menu-item.is-active::before) {
  opacity: 1;
}

:deep(.sidebar-menu .el-menu-item.is-active .el-icon) {
  color: var(--vh-accent);
  filter: none;
}

.sidebar-menu-copy {
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.sidebar-menu-label {
  font-size: 13px;
  font-weight: 400;
}

:deep(.sidebar-menu .el-menu-item.is-active .sidebar-menu-label) {
  font-weight: 700;
}

.sidebar-footer {
  position: absolute;
  z-index: 2;
  right: 0;
  bottom: 18px;
  left: 0;
}

.system-card {
  padding: 13px;
  border: 1px solid var(--ui-border-muted);
  border-radius: 15px;
  background: var(--ui-surface-muted);
  box-shadow: var(--ui-shadow-sm);
}

.system-card-topline {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--vh-text-muted);
  font-size: 9px;
  font-weight: 650;
  letter-spacing: 0.04em;
}

.system-live-dot {
  display: grid;
  width: 10px;
  height: 10px;
  place-items: center;
  border-radius: 50%;
  background: rgba(63, 220, 166, 0.13);
}

.system-live-dot i {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #43d5a3;
  box-shadow: 0 0 7px #43d5a3;
}

.system-latency {
  margin-left: auto;
  color: #4fd9ac;
  font-size: 8px;
  letter-spacing: 0.14em;
}

.system-user-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
  padding-top: 11px;
  border-top: 1px solid var(--ui-border-muted);
}

.user-avatar {
  display: grid;
  width: 31px;
  height: 31px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--vh-accent) 24%, transparent);
  border-radius: 10px;
  color: var(--vh-accent);
  background: var(--vh-accent-soft);
  font-size: 11px;
  font-weight: 750;
}

.user-name {
  overflow: hidden;
  color: var(--vh-text);
  font-size: 11px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-role {
  margin-top: 2px;
  color: var(--vh-text-soft);
  font-size: 8px;
  letter-spacing: 0.04em;
}

.logout-button {
  display: grid;
  border: 0;
  place-items: center;
  cursor: pointer;
}

.logout-button {
  width: 28px;
  height: 28px;
  border-radius: 9px;
  color: var(--vh-text-soft);
  background: transparent;
  transition: color 160ms ease, background 160ms ease;
}

.logout-button:hover {
  color: #ff8796;
  background: rgba(255, 103, 125, 0.1);
}

.logout-button svg {
  width: 17px;
}

.topbar {
  display: flex;
  height: 64px !important;
  flex: 0 0 64px;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  border-bottom: 1px solid var(--ui-border-muted);
  background: color-mix(in srgb, var(--vh-page) 80%, transparent);
  backdrop-filter: blur(24px) saturate(135%);
  -webkit-backdrop-filter: blur(24px) saturate(135%);
}

.topbar-left,
.topbar-actions {
  display: flex;
  align-items: center;
}

.topbar-left {
  gap: 15px;
}

.topbar-actions {
  gap: 10px;
}

.route-context {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
}

.route-kicker {
  color: var(--vh-text-soft);
}

.route-divider {
  color: var(--ui-border);
}

.route-name {
  color: var(--vh-text);
}

.mobile-tabbar {
  position: fixed;
  z-index: 100;
  right: 10px;
  bottom: max(9px, env(safe-area-inset-bottom));
  left: 10px;
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  min-height: 65px;
  padding: 6px;
  border: 1px solid var(--ui-border);
  border-radius: 21px;
  background: color-mix(in srgb, var(--ui-surface-strong) 93%, transparent);
  box-shadow: 0 18px 48px rgba(8, 17, 32, 0.22), inset 0 1px 0 rgba(255, 255, 255, 0.12);
  backdrop-filter: blur(28px) saturate(160%);
  -webkit-backdrop-filter: blur(28px) saturate(160%);
}

.mobile-tab {
  position: relative;
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 3px;
  padding: 5px 0;
  border: 0;
  border-radius: 15px;
  color: var(--vh-text-soft);
  background: transparent;
  cursor: pointer;
}

.mobile-tab::after {
  position: absolute;
  top: 4px;
  width: 18px;
  height: 2px;
  border-radius: 99px;
  content: "";
  opacity: 0;
  background: linear-gradient(90deg, var(--vh-accent), var(--vh-cyan));
  box-shadow: 0 0 8px color-mix(in srgb, var(--vh-accent) 55%, transparent);
}

.mobile-tab.is-active {
  color: var(--vh-accent);
  background: var(--vh-accent-soft);
}

.mobile-tab.is-active::after {
  opacity: 1;
}

.mobile-tab-icon,
.mobile-tab-icon svg {
  width: 20px;
  height: 20px;
}

.mobile-tab-label {
  overflow: hidden;
  max-width: 100%;
  font-size: 9px;
  font-weight: 680;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mobile-brand-lockup {
  display: flex;
  align-items: center;
  gap: 10px;
}

.brand-mark-small {
  width: 34px;
  height: 34px;
  border-radius: 11px;
}

.brand-mark-small .brand-glyph {
  font-size: 14px;
}

.mobile-brand-name {
  color: var(--vh-text);
  font-size: 14px;
  font-weight: 760;
  line-height: 1.05;
  letter-spacing: -0.03em;
}

.mobile-route-name {
  margin-top: 3px;
  color: var(--vh-text-soft);
  font-size: 8px;
  font-weight: 650;
  letter-spacing: 0.08em;
}

/* One floating rail, then an open canvas. Surfaces appear only around grouped data. */
.app-shell {
  gap: 18px;
  padding: 14px;
  background: var(--vh-page);
}

.desktop-sidebar {
  height: 100%;
  border: 1px solid rgba(30, 30, 32, 0.1);
  border-radius: 28px;
  background: rgba(255, 255, 255, 0.76);
  box-shadow: 0 16px 42px rgba(24, 31, 44, 0.08);
  backdrop-filter: blur(35px) saturate(135%);
  -webkit-backdrop-filter: blur(35px) saturate(135%);
}

.sidebar-ambient {
  opacity: 0;
  background: none;
}

.workspace-shell {
  min-width: 0;
  border-radius: 22px;
  background: transparent;
}

.topbar {
  height: 70px !important;
  flex-basis: 70px;
  padding: 0 12px 0 8px;
  border-bottom: 0;
  background: transparent;
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
}

:deep(.sidebar-menu .el-menu-item) {
  border: 0;
  border-radius: 14px;
}

:deep(.sidebar-menu .el-menu-item.is-active) {
  border: 0;
  color: var(--vh-accent);
  background: rgba(37, 87, 202, 0.12);
  box-shadow: none;
}

:deep(.sidebar-menu .el-menu-item.is-active .el-icon) {
  color: var(--vh-accent);
  filter: none;
}

.main-inner {
  max-width: 1420px;
}

html.dark .app-shell {
  background: #000;
}

html.dark .desktop-sidebar {
  border-color: rgba(255, 255, 255, 0.14);
  background: rgba(22, 22, 23, 0.85);
  box-shadow: 0 18px 52px rgba(0, 0, 0, 0.34);
}

html.dark .brand-caption {
  color: var(--vh-dark-text-secondary);
}

html.dark :deep(.sidebar-menu .el-menu-item .el-icon) {
  color: var(--vh-dark-text-secondary);
}

html.dark .user-avatar {
  color: #d6e2ff;
  background: rgba(37, 87, 202, 0.2);
}

html.dark :deep(.sidebar-menu .el-menu-item.is-active) {
  color: var(--vh-accent);
  background: rgba(255, 255, 255, 0.11);
}

html.dark :deep(.sidebar-menu .el-menu-item),
html.dark :deep(.sidebar-menu .el-menu-item:hover) {
  color: var(--vh-dark-text-primary);
}

html.dark :deep(.sidebar-menu .el-menu-item.is-active .sidebar-menu-label) {
  color: var(--vh-accent);
}

html.dark :deep(.sidebar-menu .el-menu-item.is-active .el-icon) {
  color: #69a7ff;
}

html.dark .system-card {
  border-color: rgba(255, 255, 255, 0.1);
  background: rgba(255, 255, 255, 0.055);
}

html.dark .topbar {
  color: var(--vh-dark-text-primary);
}

@media (max-width: 767px) {
  .app-shell {
    gap: 0;
    padding: 0;
  }

  .workspace-shell {
    border-radius: 0;
  }

  .topbar {
    height: calc(60px + env(safe-area-inset-top)) !important;
    flex-basis: calc(60px + env(safe-area-inset-top));
    padding: env(safe-area-inset-top) 14px 0;
  }
}
</style>
