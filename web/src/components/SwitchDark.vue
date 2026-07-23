<script setup lang="ts">
import { WeatherMoon24Regular, WeatherSunny24Regular } from '@vicons/fluent'

const props = defineProps<{
  isDark: boolean
}>()

const emit = defineEmits<{
  toggle: [next: boolean]
}>()

function onToggle() {
  emit('toggle', !props.isDark)
}
</script>

<template>
  <el-button aria-label="切换明暗主题" @click="onToggle" class="theme-toggle-button !bg-white/50 dark:!bg-white/5">
    <el-icon v-if="isDark"><WeatherSunny24Regular /></el-icon>
    <el-icon v-else><WeatherMoon24Regular /></el-icon>
  </el-button>
</template>

<style scoped>
:deep(.theme-toggle-button) {
  width: 52px;
  height: 52px;
  padding: 0;
  border-radius: 16px !important;
}
</style>
