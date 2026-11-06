// 通用格式化工具

const pad = (n) => String(n).padStart(2, '0')

// 时间统一输出 yyyy-MM-dd HH:mm
// 兼容三种输入：RFC3339 字符串、sql.NullTime 对象 {Time, Valid}、Date
export function fmtTime(v) {
  if (v == null || v === '') return '—'
  if (typeof v === 'object' && 'Valid' in v) return v.Valid ? fmtTime(v.Time) : '—'
  const d = v instanceof Date ? v : new Date(v)
  if (isNaN(d.getTime())) return String(v)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// sql.NullInt64 → int | null
export function nullInt(v) {
  return v && v.Valid ? v.Int64 : null
}

// 字节数人性化显示
export function fmtBytes(n) {
  if (n == null) return '—'
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`
}

// 设备角色 / 状态等标签映射
export const DEVICE_ROLE_LABELS = { server: '服务端', client: '客户端' }
export const DEVICE_STATUS_LABELS = { online: '在线', offline: '离线', revoked: '已吊销' }
export const DEVICE_STATUS_TAG_TYPES = { online: 'success', offline: 'info', revoked: 'danger' }

export const AUDIT_ACTION_LABELS = { connect: '连接', close: '关闭', blocked: '拦截', login: '登录' }
export const AUDIT_ACTION_TAG_TYPES = { connect: 'success', close: 'info', blocked: 'danger', login: 'primary' }

export const ALERT_LEVEL_LABELS = { info: '提示', warn: '警告', error: '严重' }
export const ALERT_LEVEL_TAG_TYPES = { info: 'info', warn: 'warning', error: 'danger' }

export const SESSION_MODE_LABELS = { p2p: 'P2P 直连', relay: '中继转发' }
