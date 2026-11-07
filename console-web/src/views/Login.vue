<template>
  <div class="login-page">
    <!-- 左侧品牌区：深蓝渐变 -->
    <div class="brand-side">
      <div class="brand-glow glow-a"></div>
      <div class="brand-glow glow-b"></div>
      <div class="brand-inner">
        <div class="brand-logo-row">
          <Logo :size="56" on-dark />
          <span class="brand-logo-name">励联 LiLink</span>
        </div>
        <h1 class="brand-slogan">励联 LiLink 安全穿透云</h1>
        <p class="brand-desc">企业级内网穿透与设备接入管理平台，安全直达每一台内网服务器。</p>
        <ul class="brand-points">
          <li>
            <el-icon><Lock /></el-icon>
            <span>端到端加密隧道，令牌化设备接入</span>
          </li>
          <li>
            <el-icon><Connection /></el-icon>
            <span>P2P 直连优先，中继兜底保障可用</span>
          </li>
          <li>
            <el-icon><OfficeBuilding /></el-icon>
            <span>多租户隔离，细粒度角色权限管控</span>
          </li>
        </ul>
      </div>
    </div>

    <!-- 右侧登录表单：浅色 -->
    <div class="form-side">
      <div class="login-card">
        <div class="login-head">
          <Logo :size="40" />
          <h2>欢迎登录</h2>
          <p>励联 LiLink 管理控制台</p>
        </div>
        <el-alert
          v-if="errorMsg"
          :title="errorMsg"
          type="error"
          show-icon
          :closable="false"
          class="login-error"
        />
        <el-form label-position="top" size="large" @submit.prevent="submit">
          <el-form-item label="租户编码">
            <el-input v-model.trim="form.tenant" placeholder="租户编码，平台账号填 platform">
              <template #prefix><el-icon><OfficeBuilding /></el-icon></template>
            </el-input>
          </el-form-item>
          <el-form-item label="账号">
            <el-input v-model.trim="form.username" placeholder="请输入用户名" autocomplete="username">
              <template #prefix><el-icon><User /></el-icon></template>
            </el-input>
          </el-form-item>
          <el-form-item label="密码">
            <el-input
              v-model="form.password"
              type="password"
              show-password
              placeholder="请输入密码"
              autocomplete="current-password"
              @keyup.enter="submit"
            >
              <template #prefix><el-icon><Lock /></el-icon></template>
            </el-input>
          </el-form-item>
          <el-button
            type="primary"
            class="login-btn"
            native-type="submit"
            :loading="loading"
          >
            {{ loading ? '登录中…' : '登 录' }}
          </el-button>
        </el-form>
        <div class="login-foot">默认租户 liyou，如需平台管理请使用 platform</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import http from '../api'
import { setAuth } from '../store'
import Logo from '../components/Logo.vue'

const router = useRouter()
const form = reactive({ tenant: 'liyou', username: '', password: '' })
const loading = ref(false)
const errorMsg = ref('')

async function submit() {
  if (loading.value) return
  if (!form.username || !form.password) {
    errorMsg.value = '请输入账号和密码'
    return
  }
  loading.value = true
  errorMsg.value = ''
  try {
    const data = await http.post(
      '/api/auth/login',
      {
        tenant: form.tenant || 'platform',
        username: form.username,
        password: form.password,
        client_type: 'pc',
      },
      { silent: true }
    )
    setAuth(data)
    router.push('/dashboard')
  } catch (err) {
    errorMsg.value = err.response && err.response.data && err.response.data.msg
      ? err.response.data.msg
      : (err.message || '登录失败，请稍后重试')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  display: flex;
  height: 100vh;
  overflow: hidden;
}

/* 左侧品牌区 */
.brand-side {
  position: relative;
  flex: 1.1;
  min-width: 420px;
  background: linear-gradient(135deg, #0A1A33 0%, #0C2144 100%);
  color: #FFFFFF;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

.brand-glow {
  position: absolute;
  border-radius: 50%;
  filter: blur(90px);
  opacity: 0.35;
}

.glow-a {
  width: 420px;
  height: 420px;
  background: #2AA4E9;
  top: -120px;
  left: -100px;
}

.glow-b {
  width: 360px;
  height: 360px;
  background: #0F51A8;
  bottom: -120px;
  right: -80px;
}

.brand-inner {
  position: relative;
  max-width: 480px;
  padding: 40px;
}

.brand-logo-row {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 28px;
}

.brand-logo-name {
  font-size: 22px;
  font-weight: 700;
  letter-spacing: 2px;
}

.brand-slogan {
  font-size: 32px;
  font-weight: 700;
  margin: 0 0 14px;
  letter-spacing: 1px;
}

.brand-desc {
  font-size: 14.5px;
  line-height: 1.7;
  color: rgba(255, 255, 255, 0.78);
  margin: 0 0 34px;
}

.brand-points {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.brand-points li {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 14px;
  color: rgba(255, 255, 255, 0.88);
}

.brand-points .el-icon {
  color: #F5A623;
  font-size: 18px;
}

/* 右侧表单区 */
.form-side {
  flex: 1;
  min-width: 420px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #F3F5F9;
  padding: 24px;
}

.login-card {
  width: 400px;
  max-width: 100%;
  background: #FFFFFF;
  border: 1px solid #E3E8F0;
  border-radius: 10px;
  box-shadow: 0 1px 2px rgba(22, 35, 60, 0.04);
  padding: 36px 36px 24px;
}

.login-head {
  text-align: center;
  margin-bottom: 24px;
}

.login-head h2 {
  font-size: 20px;
  font-weight: 700;
  color: #16233C;
  margin: 12px 0 4px;
}

.login-head p {
  font-size: 13px;
  color: #8C99AE;
  margin: 0;
}

.login-error {
  margin-bottom: 16px;
}

.login-btn {
  width: 100%;
  margin-top: 4px;
  letter-spacing: 4px;
}

.login-foot {
  margin-top: 18px;
  text-align: center;
  font-size: 12px;
  color: #8C99AE;
}
</style>
