<script setup lang="ts">
import LoadingScreen from '../components/LoadingScreen.vue'
import SwitchDark from '../components/SwitchDark.vue'

defineProps({
  isDark: {
    type: Boolean,
    required: true
  }
})

const emit = defineEmits(['toggle-theme'])
</script>

<template>
  <main class="auth-shell">
    <div class="auth-grid" aria-hidden="true" />
    <div class="auth-orb auth-orb-one" aria-hidden="true" />
    <div class="auth-orb auth-orb-two" aria-hidden="true" />

    <div class="auth-theme-toggle">
      <SwitchDark :is-dark="isDark" @toggle="(e) => emit('toggle-theme', e)" />
    </div>

    <router-view v-slot="{ Component }">
      <component :is="Component" v-if="Component" />
      <LoadingScreen v-else />
    </router-view>
  </main>
</template>

<style scoped>
.auth-shell {
  position: relative;
  display: grid;
  width: 100%;
  height: 100%;
  min-height: 560px;
  place-items: center;
  overflow: auto;
  padding: max(28px, env(safe-area-inset-top)) max(22px, env(safe-area-inset-right)) max(28px, env(safe-area-inset-bottom)) max(22px, env(safe-area-inset-left));
  background:
    radial-gradient(circle at 18% 8%, rgba(37, 87, 202, 0.17), transparent 30rem),
    radial-gradient(circle at 88% 88%, rgba(20, 176, 213, 0.12), transparent 28rem),
    var(--vh-page);
}

.auth-grid {
  position: fixed;
  inset: 0;
  pointer-events: none;
  opacity: 0.55;
  background-image:
    linear-gradient(rgba(72, 98, 133, 0.045) 1px, transparent 1px),
    linear-gradient(90deg, rgba(72, 98, 133, 0.045) 1px, transparent 1px);
  background-size: 40px 40px;
  mask-image: radial-gradient(circle at center, #000 20%, transparent 78%);
}

.dark .auth-grid {
  opacity: 0.8;
  background-image:
    linear-gradient(rgba(124, 155, 197, 0.045) 1px, transparent 1px),
    linear-gradient(90deg, rgba(124, 155, 197, 0.045) 1px, transparent 1px);
}

.auth-orb {
  position: fixed;
  pointer-events: none;
  border-radius: 50%;
  filter: blur(90px);
}

.auth-orb-one {
  top: -180px;
  left: -80px;
  width: 480px;
  height: 480px;
  background: rgba(95, 106, 237, 0.12);
}

.auth-orb-two {
  right: -140px;
  bottom: -180px;
  width: 520px;
  height: 520px;
  background: rgba(35, 194, 225, 0.08);
}

.auth-theme-toggle {
  position: fixed;
  z-index: 50;
  top: max(16px, env(safe-area-inset-top));
  right: max(16px, env(safe-area-inset-right));
}

@media (max-width: 767px) {
  .auth-shell {
    min-height: 100dvh;
    place-items: start center;
    padding-top: max(74px, calc(env(safe-area-inset-top) + 58px));
  }
}
</style>
