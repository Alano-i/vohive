<script setup lang="ts">
defineProps<{
  title: string
  subtitle?: string
}>()
</script>

<template>
  <header class="page-header flex flex-col sm:flex-row sm:items-end sm:justify-between gap-4">
    <div>
      <div class="page-eyebrow">Control module</div>
      <h1 class="page-title">{{ title }}</h1>
      <p v-if="subtitle" class="page-subtitle">{{ subtitle }}</p>
    </div>
    <div class="page-header-actions flex items-center gap-2">
      <slot name="actions" />
    </div>
  </header>
</template>
