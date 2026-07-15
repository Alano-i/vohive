<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useSettingsStore } from '../stores/settings'
import PageHeader from '../components/PageHeader.vue'
import FieldRow from '../components/FieldRow.vue'
import {
  Key24Regular,
  Save24Regular,
  Server24Regular,
  Alert24Regular,
  DocumentText24Regular
} from '@vicons/fluent'
import { systemService, type UpdateInfo } from '../services/system'

const settingsStore = useSettingsStore()
const { systemInfo, changingPassword, passwordForm } = storeToRefs(settingsStore)

async function changePassword() {
  if (passwordForm.value.new_password !== passwordForm.value.confirm_password) {
    ElMessage.error('两次输入的新密码不一致')
    return
  }
  
  try {
     const result = await settingsStore.changePasswordFromForm()
     if (!result.ok) throw new Error(result.error.message || '更新失败')
     ElMessage.success('密码已更新')
     settingsStore.resetPasswordForm()
  } catch {
     ElMessage.error('失败：后端尚未实现该功能或请求失败')
  }
}

async function loadSystemInfo() {
  const result = await settingsStore.fetchSystemInfo()
  if (!result.ok) {
    console.error('系统信息读取失败', result.error)
  }
}

function openAPIDocs() {
  const docsURL = String(systemInfo.value.docs?.swagger_ui || '').trim()
  if (!docsURL) {
    ElMessage.warning('API 文档入口暂不可用')
    return
  }
  window.open(docsURL, '_blank', 'noopener,noreferrer')
}

const checkingUpdate = ref(false)
const applyingUpdate = ref(false)
const updateInfo = ref<UpdateInfo | null>(null)

async function doCheckUpdate() {
  checkingUpdate.value = true
  try {
    const res = await systemService.checkUpdate()
    if (!res.ok) throw new Error(res.error.message || '检查更新失败')
    updateInfo.value = res.data
    if (!res.data.has_update) {
      ElMessage.success('当前已是最新版本')
    }
  } catch (e: any) {
    ElMessage.error(e.message || '检查更新失败')
  } finally {
    checkingUpdate.value = false
  }
}

async function doApplyUpdate() {
  if (!updateInfo.value) return

  if (updateInfo.value.is_docker) {
    ElMessageBox.alert(
      '检测到当前系统运行在 Docker 环境下。<br><br>不建议在 Docker 容器内直接执行文件热替换。请按当前部署方式拉取最新镜像并重启容器来完成升级。',
      '环境警告',
      { dangerouslyUseHTMLString: true, type: 'warning' }
    )
    return
  }

  try {
    await ElMessageBox.confirm(
      `最新版本：${updateInfo.value.latest_version}<br>当前平台：${updateInfo.value.platform || 'Unknown'}<br>更新包：${updateInfo.value.asset_name || 'Unknown'}<br><br>确定要现在更新并重启服务吗？<br><br><pre style="white-space: pre-wrap; font-size: 12px; max-height: 200px; overflow-y: auto; background: var(--el-fill-color-light); padding: 8px; border-radius: 4px; margin-top: 8px;">${updateInfo.value.release_note}</pre>`,
      '应用更新',
      { dangerouslyUseHTMLString: true, confirmButtonText: '立即更新', cancelButtonText: '取消', type: 'warning' }
    )
    applyingUpdate.value = true
    const res = await systemService.applyUpdate()
    if (!res.ok) throw new Error(res.error.message || '请求应用更新失败')
    ElMessage.success(res.data?.message || '正在更新...')
    setTimeout(() => {
      window.location.reload()
    }, 5000)
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e.message || '应用更新失败')
    }
  } finally {
    applyingUpdate.value = false
  }
}

onMounted(() => {
  loadSystemInfo()
})
</script>

<template>
  <div>
    <PageHeader title="系统设置" subtitle="管理网关参数与运行信息" />

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-8">
      <!-- Security Card -->
      <div class="ui-card p-8 relative overflow-hidden group">
         <div class="ui-card-glow absolute top-0 left-0 w-40 h-40 rounded-br-full -ml-10 -mt-10 transition-transform group-hover:scale-110"></div>
         
         <div class="flex items-center gap-3 mb-6 relative z-10">
            <div class="w-12 h-12 rounded-xl bg-primary-50 dark:bg-primary-500/10 flex items-center justify-center text-primary-600 dark:text-primary-400">
               <el-icon size="24"><Key24Regular /></el-icon>
            </div>
            <div>
               <h3 class="text-lg font-bold text-gray-800 dark:text-gray-100">安全</h3>
               <p class="text-xs text-gray-500">更新访问凭证</p>
            </div>
         </div>

         <div class="space-y-4 relative z-10">
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">当前密码</label>
                <el-input v-model="passwordForm.old_password" type="password" show-password placeholder="••••••••" size="large" />
             </div>
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">新密码</label>
                <el-input v-model="passwordForm.new_password" type="password" show-password placeholder="••••••••" size="large" />
             </div>
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">确认新密码</label>
                <el-input v-model="passwordForm.confirm_password" type="password" show-password placeholder="••••••••" size="large" />
             </div>
             
             <div class="pt-4">
                 <el-button type="primary" :loading="changingPassword" @click="changePassword" size="large" class="w-full !border-0">
                   <el-icon><Save24Regular /></el-icon>
                   更新凭证
                 </el-button>
             </div>
         </div>
      </div>

      <!-- System Info Card -->
      <div class="ui-card p-8 relative overflow-hidden group flex flex-col">
         <div class="ui-card-glow absolute top-0 left-0 w-40 h-40 rounded-br-full -ml-10 -mt-10 transition-transform group-hover:scale-110"></div>

         <div class="flex items-start justify-between gap-4 mb-6 relative z-10">
            <div class="flex items-center gap-3 min-w-0">
              <div class="w-12 h-12 rounded-xl bg-primary-50 dark:bg-primary-500/10 flex items-center justify-center text-primary-600 dark:text-primary-400">
                 <el-icon size="24"><Server24Regular /></el-icon>
              </div>
              <div class="min-w-0">
                 <h3 class="text-lg font-bold text-gray-800 dark:text-gray-100">系统信息</h3>
                 <p class="text-xs text-gray-500">运行环境</p>
              </div>
            </div>
            <el-button size="small" type="primary" class="shrink-0 !border-0" :loading="checkingUpdate" @click.stop="doCheckUpdate">
              检查更新
            </el-button>
         </div>

         <div class="space-y-4 text-sm relative z-10 flex flex-1 flex-col">
            <div v-if="updateInfo?.has_update" class="p-4 bg-emerald-50 dark:bg-emerald-500/10 rounded-lg border border-emerald-200 dark:border-emerald-500/20">
               <div class="flex items-center gap-2 text-emerald-800 dark:text-emerald-200 mb-2 font-bold text-[13px]">
                 <el-icon><Alert24Regular /></el-icon>发现新版本: {{ updateInfo.latest_version }}
               </div>
               <div class="text-xs text-emerald-800 dark:text-emerald-200 mb-2 space-y-1">
                 <div>当前平台: {{ updateInfo.platform || 'Unknown' }}</div>
                 <div>更&nbsp;&nbsp;新&nbsp;&nbsp;包: {{ updateInfo.asset_name || 'Unknown' }}</div>
               </div>
               <div class="text-xs text-emerald-700 dark:text-emerald-300/80 mb-4 whitespace-pre-wrap max-h-32 overflow-y-auto pr-2 custom-scrollbar">
                 {{ updateInfo.release_note || '暂无更新说明' }}
               </div>
               <el-button
                 :loading="applyingUpdate"
                 @click="doApplyUpdate"
                 class="w-full !border-0 !text-white"
                 style="--el-button-bg-color: #0ba176; --el-button-border-color: #0ba176; --el-button-hover-bg-color: #0db686; --el-button-hover-border-color: #0db686; --el-button-active-bg-color: #088b66; --el-button-active-border-color: #088b66;"
               >
                 立即更新并重启
               </el-button>
            </div>
            <div class="flex flex-1 flex-col justify-between gap-8">
              <div class="space-y-5 px-1 text-gray-700 dark:text-gray-200">
                <FieldRow label="版本" :value="systemInfo.version" monospace />
                <FieldRow label="构建时间" :value="systemInfo.build_time" monospace />
                <FieldRow label="配置路径" :value="systemInfo.config" monospace copyable />
              </div>
              <div class="api-doc-panel px-4 py-4 bg-gray-50 dark:!bg-black/20 !border-0 !shadow-none">
                <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
                  <div class="min-w-0">
                    <div class="flex items-center gap-3">
                      <div class="w-9 h-9 rounded-xl bg-primary-50 dark:bg-primary-500/10 flex items-center justify-center text-primary-600 dark:text-primary-400">
                        <el-icon size="18"><DocumentText24Regular /></el-icon>
                      </div>
                      <div>
                        <div class="text-sm font-bold text-gray-800 dark:text-gray-100">API 文档</div>
                        <div class="text-xs text-gray-500">打开后端直出的 OpenAPI 页面</div>
                      </div>
                    </div>

                  </div>
                  <el-button
                    type="primary"
                    class="self-start sm:self-center shrink-0 !border-0"
                    :disabled="!systemInfo.docs?.swagger_ui"
                    @click="openAPIDocs"
                  >
                    <el-icon><DocumentText24Regular /></el-icon>
                    打开 API 文档
                  </el-button>
                </div>
              </div>
            </div>
         </div>
      </div>

    </div>
  </div>
</template>

<style scoped>
.api-doc-panel {
  border-radius: 1rem;
}
</style>
