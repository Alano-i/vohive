<script setup lang="ts">
import { ArrowSync24Regular } from '@vicons/fluent'
import { computed, onBeforeUnmount, ref } from 'vue'

const props = defineProps<{
  loading?: boolean
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'click'): void
}>()

const MINIMUM_SPIN_MS = 850
const minimumSpinActive = ref(false)
let minimumSpinTimer: ReturnType<typeof setTimeout> | undefined

const spinning = computed(() => Boolean(props.loading) || minimumSpinActive.value)

function handleClick() {
  minimumSpinActive.value = true
  if (minimumSpinTimer) clearTimeout(minimumSpinTimer)
  minimumSpinTimer = setTimeout(() => {
    minimumSpinActive.value = false
    minimumSpinTimer = undefined
  }, MINIMUM_SPIN_MS)
  emit('click')
}

onBeforeUnmount(() => {
  if (minimumSpinTimer) clearTimeout(minimumSpinTimer)
})
</script>

<template>
  <el-button
    :disabled="disabled"
    :aria-busy="loading ? 'true' : 'false'"
    class="!bg-white/60 dark:!bg-white/5 ui-action-btn"
    @click="handleClick"
  >
    <el-icon :class="{ 'refresh-icon-spinning': spinning }"><ArrowSync24Regular /></el-icon>
    刷新
  </el-button>
</template>
