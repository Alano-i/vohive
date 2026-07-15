<script setup lang="ts">
import { ref } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useRoute, useRouter } from 'vue-router'
import { Person24Regular, LockClosed24Regular, ArrowRight24Regular } from '@vicons/fluent'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const form = ref({
  username: '',
  password: ''
})
const loading = ref(false)

async function handleLogin() {
  const { ElMessage } = await import('element-plus')
  if (!form.value.username || !form.value.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }

  loading.value = true
  await new Promise<void>(resolve => setTimeout(resolve, 600))
  const success = await auth.login(form.value.username, form.value.password)
  loading.value = false

  if (success) {
    ElMessage.success('欢迎回来')
    const queryRedirect = typeof route.query.redirect === 'string' ? route.query.redirect : ''
    let redirect = queryRedirect ? decodeURIComponent(queryRedirect) : ''
    if (!redirect) {
      try {
        redirect = sessionStorage.getItem('post_login_redirect') || ''
      } catch {
        // Ignore sessionStorage read failures.
      }
    }
    if (redirect) {
      try {
        sessionStorage.removeItem('post_login_redirect')
      } catch {
        // Ignore sessionStorage delete failures.
      }
      router.push(redirect)
    } else {
      router.push('/')
    }
  } else {
    ElMessage.error('登录失败，请检查凭证')
  }
}
</script>

<template>
  <div class="login-layout">
    <section class="login-intro" aria-label="VoHive 产品介绍">
      <div>
        <div class="intro-brand">
          <div class="login-brand-mark">
            <span>V</span>
            <i />
          </div>
          <div>
            <div class="intro-brand-name">VoHive</div>
            <div class="intro-brand-caption">CELLULAR CONTROL FABRIC</div>
          </div>
        </div>

        <div class="intro-copy">
          <div class="intro-eyebrow"><span /> PRIVATE EDGE CONSOLE</div>
          <h1>连接每一张卡，<br><em>掌控每一条链路。</em></h1>
          <p>面向蜂窝网络设备的统一控制中心。设备、代理、短信与链路遥测，在一个安全界面内协同运转。</p>
        </div>
      </div>

      <div class="intro-status-panel">
        <div class="intro-status-head">
          <div>
            <span class="intro-live"><i /></span>
            CONTROL PLANE READY
          </div>
          <span>V6 / SECURE</span>
        </div>
        <div class="intro-metrics">
          <div><strong>24/7</strong><span>持续监控</span></div>
          <div><strong>6</strong><span>控制模块</span></div>
          <div><strong>LOCAL</strong><span>私有部署</span></div>
        </div>
        <div class="signal-line"><span /><span /><span /><span /><span /></div>
      </div>
    </section>

    <section class="login-panel">
      <div class="login-card">
        <div class="mobile-login-brand">
          <div class="login-brand-mark"><span>V</span><i /></div>
          <div>
            <div class="intro-brand-name">VoHive</div>
            <div class="intro-brand-caption">CONTROL CENTER</div>
          </div>
        </div>

        <div class="login-heading">
          <div class="login-kicker">AUTHENTICATION / 01</div>
          <h2>登录控制台</h2>
          <p>使用管理员凭证进入 VoHive 控制中心</p>
        </div>

        <form class="login-form" @submit.prevent="handleLogin">
          <label class="field-group">
            <span class="field-label"><b>用户名</b><em>USERNAME</em></span>
            <span class="field-control">
              <Person24Regular />
              <input
                v-model="form.username"
                type="text"
                name="username"
                autocomplete="username"
                autocapitalize="none"
                spellcheck="false"
                placeholder="输入管理员用户名"
              >
            </span>
          </label>

          <label class="field-group">
            <span class="field-label"><b>密码</b><em>PASSWORD</em></span>
            <span class="field-control">
              <LockClosed24Regular />
              <input
                v-model="form.password"
                type="password"
                name="password"
                autocomplete="current-password"
                enterkeyhint="go"
                placeholder="输入访问密码"
              >
            </span>
          </label>

          <button type="submit" class="login-submit" :disabled="loading">
            <span v-if="loading" class="login-spinner" />
            <template v-else>
              <span>进入控制中心</span>
              <ArrowRight24Regular />
            </template>
          </button>
        </form>

        <div class="login-security">
          <span class="security-dot" />
          <span>凭证仅发送至当前私有节点</span>
          <span class="security-code">TLS</span>
        </div>
      </div>

      <div class="login-footer">
        <span>VoHive © 2026</span>
        <span>PRIVATE CELLULAR INFRASTRUCTURE</span>
      </div>
    </section>
  </div>
</template>

<style scoped>
.login-layout {
  position: relative;
  z-index: 2;
  display: grid;
  width: min(1120px, calc(100vw - 56px));
  min-height: min(720px, calc(100dvh - 72px));
  grid-template-columns: minmax(0, 1.08fr) minmax(390px, 0.92fr);
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 30px;
  background: var(--ui-surface);
  box-shadow: 0 38px 110px rgba(17, 33, 60, 0.16), inset 0 1px 0 rgba(255, 255, 255, 0.12);
  backdrop-filter: blur(28px) saturate(145%);
  -webkit-backdrop-filter: blur(28px) saturate(145%);
}

.dark .login-layout {
  box-shadow: 0 42px 120px rgba(0, 0, 0, 0.45), inset 0 1px 0 rgba(255, 255, 255, 0.035);
}

.login-intro {
  position: relative;
  display: flex;
  overflow: hidden;
  justify-content: space-between;
  flex-direction: column;
  padding: 46px;
  color: #edf4ff;
  background:
    radial-gradient(circle at 12% 0%, rgba(122, 135, 255, 0.26), transparent 25rem),
    radial-gradient(circle at 92% 96%, rgba(44, 202, 233, 0.12), transparent 22rem),
    linear-gradient(148deg, #101a2e, #080e1a 75%);
}

.login-intro::before {
  position: absolute;
  inset: 0;
  content: "";
  opacity: 0.7;
  background-image:
    linear-gradient(rgba(121, 155, 204, 0.05) 1px, transparent 1px),
    linear-gradient(90deg, rgba(121, 155, 204, 0.05) 1px, transparent 1px);
  background-size: 32px 32px;
  mask-image: linear-gradient(130deg, #000, transparent 80%);
}

.login-intro::after {
  position: absolute;
  top: 23%;
  right: -150px;
  width: 340px;
  height: 340px;
  border: 1px solid rgba(124, 141, 255, 0.12);
  border-radius: 50%;
  content: "";
  box-shadow: 0 0 0 38px rgba(124, 141, 255, 0.025), 0 0 0 78px rgba(124, 141, 255, 0.018);
}

.intro-brand,
.intro-copy,
.intro-status-panel {
  position: relative;
  z-index: 1;
}

.intro-brand {
  display: flex;
  align-items: center;
  gap: 13px;
}

.login-brand-mark {
  position: relative;
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  overflow: hidden;
  border: 1px solid rgba(169, 181, 255, 0.25);
  border-radius: 14px;
  color: #fff;
  background: linear-gradient(145deg, var(--vh-accent), var(--vh-accent-strong));
  box-shadow: 0 12px 28px rgba(64, 78, 211, 0.36), inset 0 1px 0 rgba(255, 255, 255, 0.3);
}

.login-brand-mark span {
  font-size: 18px;
  font-weight: 850;
  letter-spacing: -0.08em;
}

.login-brand-mark i {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #5de6ff;
  box-shadow: 0 0 10px #5de6ff;
}

.intro-brand-name {
  color: #f3f6ff;
  font-size: 20px;
  font-weight: 760;
  line-height: 1.05;
  letter-spacing: -0.045em;
}

.intro-brand-caption {
  margin-top: 5px;
  color: #7588a7;
  font-size: 8px;
  font-weight: 750;
  letter-spacing: 0.2em;
}

.intro-copy {
  max-width: 470px;
  margin-top: 72px;
}

.intro-eyebrow {
  display: flex;
  align-items: center;
  gap: 9px;
  color: #8595b1;
  font-size: 9px;
  font-weight: 720;
  letter-spacing: 0.17em;
}

.intro-eyebrow span {
  width: 24px;
  height: 1px;
  background: linear-gradient(90deg, #8790ff, #59ddff);
}

.intro-copy h1 {
  margin: 22px 0 18px;
  color: #f5f7ff;
  font-size: clamp(37px, 4vw, 52px);
  font-weight: 700;
  line-height: 1.16;
  letter-spacing: -0.055em;
}

.intro-copy h1 em {
  color: transparent;
  background: linear-gradient(90deg, #9da4ff, #61ddf8);
  background-clip: text;
  -webkit-background-clip: text;
  font-style: normal;
}

.intro-copy p {
  max-width: 450px;
  margin: 0;
  color: #8fa0ba;
  font-size: 13px;
  line-height: 1.85;
}

.intro-status-panel {
  padding: 16px;
  border: 1px solid rgba(141, 165, 201, 0.12);
  border-radius: 17px;
  background: rgba(105, 130, 169, 0.055);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.025);
}

.intro-status-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: #7587a3;
  font-size: 8px;
  font-weight: 720;
  letter-spacing: 0.12em;
}

.intro-status-head > div {
  display: flex;
  align-items: center;
  gap: 7px;
}

.intro-live {
  display: grid;
  width: 11px;
  height: 11px;
  place-items: center;
  border-radius: 50%;
  background: rgba(67, 213, 163, 0.12);
}

.intro-live i {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #43d5a3;
  box-shadow: 0 0 7px #43d5a3;
}

.intro-metrics {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  margin-top: 15px;
  border-top: 1px solid rgba(141, 165, 201, 0.09);
  border-bottom: 1px solid rgba(141, 165, 201, 0.09);
}

.intro-metrics div {
  padding: 13px 10px;
  border-right: 1px solid rgba(141, 165, 201, 0.09);
}

.intro-metrics div:first-child { padding-left: 0; }
.intro-metrics div:last-child { border-right: 0; }
.intro-metrics strong { display: block; color: #e9effb; font-size: 12px; letter-spacing: 0.02em; }
.intro-metrics span { display: block; margin-top: 4px; color: #687b99; font-size: 8px; }

.signal-line {
  display: grid;
  height: 3px;
  grid-template-columns: 1fr 1.3fr 0.6fr 1.8fr 0.8fr;
  gap: 4px;
  margin-top: 13px;
}

.signal-line span {
  border-radius: 99px;
  background: linear-gradient(90deg, rgba(118, 130, 255, 0.3), rgba(83, 219, 246, 0.8));
}

.login-panel {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  padding: 42px 54px 26px;
  background: color-mix(in srgb, var(--ui-surface-strong) 64%, transparent);
}

.login-card {
  width: 100%;
  max-width: 382px;
}

.mobile-login-brand { display: none; }

.login-heading {
  margin-bottom: 32px;
}

.login-kicker {
  color: var(--vh-accent);
  font-size: 9px;
  font-weight: 800;
  letter-spacing: 0.17em;
}

.login-heading h2 {
  margin: 11px 0 8px;
  color: var(--vh-text);
  font-size: 32px;
  font-weight: 740;
  line-height: 1.15;
  letter-spacing: -0.05em;
}

.login-heading p {
  margin: 0;
  color: var(--vh-text-muted);
  font-size: 12px;
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.field-group,
.field-label {
  display: flex;
}

.field-group {
  flex-direction: column;
  gap: 8px;
}

.field-label {
  align-items: center;
  justify-content: space-between;
}

.field-label b {
  color: var(--vh-text);
  font-size: 11px;
  font-weight: 680;
}

.field-label em {
  color: var(--vh-text-soft);
  font-size: 8px;
  font-style: normal;
  font-weight: 700;
  letter-spacing: 0.12em;
}

.field-control {
  display: flex;
  height: 50px;
  align-items: center;
  gap: 11px;
  padding: 0 15px;
  border: 1px solid var(--ui-border);
  border-radius: 13px;
  background: var(--ui-surface-muted);
  box-shadow: inset 0 1px 1px rgba(20, 38, 65, 0.02);
  transition: border-color 180ms ease, box-shadow 180ms ease, background 180ms ease;
}

.field-control:focus-within {
  border-color: var(--vh-accent);
  background: var(--ui-surface-strong);
  box-shadow: none;
}

.field-control svg {
  width: 19px;
  height: 19px;
  flex: 0 0 auto;
  color: var(--vh-text-soft);
}

.field-control:focus-within svg { color: var(--vh-accent); }

.field-control input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  color: var(--vh-text);
  background: transparent;
  font-size: 13px;
}

.field-control input::placeholder { color: var(--vh-text-soft); }

.login-submit {
  display: flex;
  height: 50px;
  align-items: center;
  justify-content: center;
  gap: 10px;
  margin-top: 4px;
  border: 0;
  border-radius: 13px;
  color: #fff;
  background: linear-gradient(135deg, var(--vh-accent), var(--vh-accent-strong));
  box-shadow: var(--ui-btn-primary-shadow), inset 0 1px 0 rgba(255, 255, 255, 0.16);
  cursor: pointer;
  font-size: 13px;
  font-weight: 720;
  transition: transform 180ms ease, box-shadow 180ms ease, filter 180ms ease;
}

.login-submit:hover:not(:disabled) {
  transform: translateY(-2px);
  background: linear-gradient(135deg, var(--vh-accent), var(--vh-accent-strong));
  box-shadow: var(--ui-btn-primary-shadow-hover), inset 0 1px 0 rgba(255, 255, 255, 0.2);
}

.login-submit:active:not(:disabled) { transform: translateY(0); }
.login-submit:disabled { opacity: 0.65; cursor: wait; }
.login-submit svg { width: 19px; height: 19px; }

.login-spinner {
  width: 20px;
  height: 20px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: #fff;
  border-radius: 50%;
  animation: login-spin 0.7s linear infinite;
}

@keyframes login-spin { to { transform: rotate(360deg); } }

.login-security {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 22px;
  padding-top: 18px;
  border-top: 1px solid var(--ui-border-muted);
  color: var(--vh-text-soft);
  font-size: 9px;
}

.security-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--vh-positive);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--vh-positive) 12%, transparent);
}

.security-code {
  margin-left: auto;
  color: var(--vh-positive);
  font-size: 8px;
  font-weight: 750;
  letter-spacing: 0.12em;
}

.login-footer {
  display: flex;
  width: 100%;
  max-width: 382px;
  align-items: center;
  justify-content: space-between;
  margin-top: auto;
  padding-top: 34px;
  color: var(--vh-text-soft);
  font-size: 7px;
  font-weight: 650;
  letter-spacing: 0.09em;
}

@media (max-width: 900px) {
  .login-layout {
    width: min(520px, calc(100vw - 30px));
    min-height: auto;
    grid-template-columns: 1fr;
  }

  .login-intro { display: none; }
  .login-panel { padding: 38px 42px 24px; }
  .mobile-login-brand { display: flex; align-items: center; gap: 12px; margin-bottom: 42px; }
  .mobile-login-brand .intro-brand-name { color: var(--vh-text); }
  .login-footer { margin-top: 38px; }
}

@media (max-width: 540px) {
  .login-layout {
    width: 100%;
    overflow: visible;
    border-radius: 22px;
  }

  .login-panel { padding: 28px 22px 22px; }
  .mobile-login-brand { margin-bottom: 32px; }
  .login-heading { margin-bottom: 26px; }
  .login-heading h2 { font-size: 29px; }
  .login-footer span:last-child { display: none; }
}
</style>
