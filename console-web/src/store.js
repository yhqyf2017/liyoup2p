import { reactive } from 'vue'

function loadSavedUser() {
  try {
    return JSON.parse(localStorage.getItem('ly_user') || 'null')
  } catch {
    return null
  }
}

// 全局响应式状态：当前用户与令牌
export const store = reactive({
  user: loadSavedUser(),
  accessToken: localStorage.getItem('ly_access_token') || '',
  refreshToken: localStorage.getItem('ly_refresh_token') || '',
  alertUnread: 0,
})

// 写入认证信息（登录 / 刷新令牌后调用）
export function setAuth(data) {
  if (data.access_token) {
    store.accessToken = data.access_token
    localStorage.setItem('ly_access_token', data.access_token)
  }
  if (data.refresh_token) {
    store.refreshToken = data.refresh_token
    localStorage.setItem('ly_refresh_token', data.refresh_token)
  }
  if (data.user) {
    store.user = data.user
    localStorage.setItem('ly_user', JSON.stringify(data.user))
  }
}

// 清空认证信息并跳转登录（由调用方负责跳转）
export function clearAuth() {
  store.accessToken = ''
  store.refreshToken = ''
  store.user = null
  store.alertUnread = 0
  localStorage.removeItem('ly_access_token')
  localStorage.removeItem('ly_refresh_token')
  localStorage.removeItem('ly_user')
}

export function hasRole(...roles) {
  return !!store.user && roles.includes(store.user.role)
}

export const isSuper = () => hasRole('super_admin')
// 成员 / 租户写权限
export const canWriteUsers = () => hasRole('super_admin', 'tenant_admin')
// 设备注册 / 吊销权限
export const canOperateDevices = () => hasRole('super_admin', 'tenant_admin', 'operator')

export const ROLE_LABELS = {
  super_admin: '超级管理员',
  tenant_admin: '租户管理员',
  operator: '操作员',
  auditor: '审计员',
}

export const ROLE_TAG_TYPES = {
  super_admin: 'danger',
  tenant_admin: 'primary',
  operator: 'success',
  auditor: 'info',
}
