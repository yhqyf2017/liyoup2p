<template>
  <div class="layout">
    <!-- 左侧导航：浅色底 + 深色字 -->
    <aside class="layout-aside">
      <div class="aside-brand">
        <Logo :size="34" />
        <div class="brand-text">
          <div class="brand-name">励联 LiLink</div>
          <div class="brand-sub">管理控制台</div>
        </div>
      </div>
      <el-menu class="layout-menu" :default-active="activePath" router>
        <div class="menu-group-title">总览</div>
        <el-menu-item index="/dashboard">
          <el-icon><Odometer /></el-icon>
          <span>工作台</span>
        </el-menu-item>
        <div class="menu-group-title">资源管理</div>
        <el-menu-item index="/devices">
          <el-icon><Monitor /></el-icon>
          <span>设备管理</span>
        </el-menu-item>
        <div class="menu-group-title">运营监控</div>
        <el-menu-item index="/audit">
          <el-icon><Document /></el-icon>
          <span>访问审计</span>
        </el-menu-item>
        <el-menu-item index="/alerts">
          <el-icon><Bell /></el-icon>
          <span>告警中心</span>
        </el-menu-item>
        <div class="menu-group-title">权限管理</div>
        <el-menu-item index="/users">
          <el-icon><User /></el-icon>
          <span>成员管理</span>
        </el-menu-item>
        <el-menu-item v-if="superRole" index="/tenants">
          <el-icon><OfficeBuilding /></el-icon>
          <span>租户管理</span>
        </el-menu-item>
        <el-menu-item v-if="superRole" index="/updates">
          <el-icon><Upload /></el-icon>
          <span>版本发布</span>
        </el-menu-item>
      </el-menu>
      <div class="aside-footer">励联 LiLink 平台 v1.0 · © 2026 济源市励友软件有限公司</div>
    </aside>

    <div class="layout-right">
      <!-- 顶栏：白底 1px 边框 -->
      <header class="layout-header">
        <div class="header-title">{{ route.meta.title }}</div>
        <div class="header-actions">
          <el-tooltip content="切换主题" placement="bottom">
            <el-button text class="bell-btn" @click="toggleTheme">
              <el-icon :size="18"><Sunny v-if="theme === 'dark'" /><Moon v-else /></el-icon>
            </el-button>
          </el-tooltip>
          <el-badge :value="store.alertUnread" :hidden="!store.alertUnread" :max="99" class="bell-badge">
            <el-button text class="bell-btn" @click="goAlerts">
              <el-icon :size="18"><Bell /></el-icon>
            </el-button>
          </el-badge>
          <el-divider direction="vertical" />
          <span class="header-user">{{ store.user ? store.user.username : '' }}</span>
          <el-tag :type="roleTag" size="small" effect="light">{{ roleLabel }}</el-tag>
          <el-tooltip content="退出登录" placement="bottom">
            <el-button text class="bell-btn" @click="logout">
              <el-icon :size="18"><SwitchButton /></el-icon>
            </el-button>
          </el-tooltip>
        </div>
      </header>
      <main class="layout-main">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { store, clearAuth, ROLE_LABELS, ROLE_TAG_TYPES } from '../store'
import { refreshAlertUnread } from '../api'
import { getTheme, setTheme } from '../theme'
import Logo from '../components/Logo.vue'

const route = useRoute()
const router = useRouter()

// 主题切换：默认暗黑（Sunny 表示当前暗黑、点击切浅色）
const theme = ref(getTheme())

function toggleTheme() {
  const next = theme.value === 'dark' ? 'light' : 'dark'
  setTheme(next)
  theme.value = next
}

const activePath = computed(() => route.path)
const superRole = computed(() => !!store.user && store.user.role === 'super_admin')
const roleLabel = computed(() => (store.user ? ROLE_LABELS[store.user.role] || store.user.role : ''))
const roleTag = computed(() => (store.user ? ROLE_TAG_TYPES[store.user.role] || 'info' : 'info'))

let unreadTimer = null
onMounted(() => {
  refreshAlertUnread()
  unreadTimer = setInterval(refreshAlertUnread, 60000)
})
onUnmounted(() => clearInterval(unreadTimer))

function goAlerts() {
  refreshAlertUnread()
  router.push('/alerts')
}

function logout() {
  ElMessageBox.confirm('确定要退出登录吗？', '退出登录', {
    confirmButtonText: '退出',
    cancelButtonText: '取消',
    type: 'warning',
  })
    .then(() => {
      clearAuth()
      router.push('/login')
    })
    .catch(() => {})
}
</script>

<style scoped>
.layout {
  display: flex;
  height: 100vh;
  overflow: hidden;
}

.layout-aside {
  width: 220px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--app-surface-2);
  border-right: 1px solid var(--app-border);
}

.aside-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 16px 18px;
  border-bottom: 1px solid var(--app-border);
}

.brand-text {
  line-height: 1.25;
}

.brand-name {
  font-size: 15px;
  font-weight: 700;
  color: var(--app-text-1);
  letter-spacing: 0.5px;
}

.brand-sub {
  font-size: 12px;
  color: var(--app-text-3);
}

.layout-menu {
  flex: 1;
  overflow-y: auto;
  background: transparent;
  border-right: none;
  padding: 8px 0;
  --el-menu-bg-color: transparent;
  --el-menu-hover-bg-color: transparent;
}

.layout-menu :deep(.el-menu-item) {
  height: 44px;
  line-height: 44px;
  margin: 2px 10px;
  padding: 0 var(--sp-3);
  border-radius: 6px;
  color: var(--app-text-1);
  font-size: 13.5px;
  transition: background-color 0.18s ease, color 0.18s ease;
}

.layout-menu :deep(.el-menu-item .el-icon) {
  color: var(--app-text-2);
  transition: color 0.18s ease;
}

.layout-menu :deep(.el-menu-item:hover) {
  background: var(--app-menu-hover-bg);
  color: var(--app-text-1);
}

.layout-menu :deep(.el-menu-item.is-active) {
  position: relative;
  background: var(--app-menu-active-bg);
  color: var(--app-brand);
  font-weight: 600;
}

.layout-menu :deep(.el-menu-item.is-active .el-icon) {
  color: var(--app-brand);
}

/* 选中项左侧 3px 品牌渐变指示条 */
.layout-menu :deep(.el-menu-item.is-active::before) {
  content: '';
  position: absolute;
  left: -10px;
  top: 9px;
  bottom: 9px;
  width: 3px;
  border-radius: 2px;
  background: var(--app-brand-grad);
}

.menu-group-title {
  padding: var(--sp-4) 22px 6px;
  font-size: 12px;
  color: var(--app-text-3);
  letter-spacing: 1px;
}

.aside-footer {
  padding: 12px 18px;
  font-size: 12px;
  color: var(--app-text-3);
  border-top: 1px solid var(--app-border);
}

.layout-right {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.layout-header {
  height: 60px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  background: var(--app-surface);
  border-bottom: 1px solid var(--app-border);
}

.header-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--app-text-1);
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.bell-btn {
  padding: 6px;
  color: var(--app-text-2);
}

.header-user {
  font-size: 13.5px;
  color: var(--app-text-1);
  font-weight: 500;
}

.layout-main {
  flex: 1;
  overflow-y: auto;
  padding: var(--sp-5) var(--sp-6);
}
</style>
