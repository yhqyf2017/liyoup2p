import axios from 'axios'
import { ElMessage } from 'element-plus'
import router from '../router'
import { store, setAuth, clearAuth } from '../store'

// 统一 axios 实例：响应拆包 {code, msg, data}，401 自动续期重放
const http = axios.create({ timeout: 20000 })

http.interceptors.request.use((cfg) => {
  const token = store.accessToken || localStorage.getItem('ly_access_token')
  if (token) cfg.headers.Authorization = `Bearer ${token}`
  return cfg
})

let refreshingPromise = null

function doRefresh() {
  const rt = store.refreshToken || localStorage.getItem('ly_refresh_token')
  if (!rt) return Promise.reject(new Error('no refresh token'))
  // 原始 axios，避免递归经过本实例拦截器
  return axios
    .post('/api/auth/refresh', { refresh_token: rt }, { timeout: 10000 })
    .then((resp) => {
      const body = resp.data
      if (!body || body.code !== 0 || !body.data || !body.data.access_token) {
        throw new Error((body && body.msg) || '刷新令牌无效')
      }
      setAuth(body.data)
      return body.data.access_token
    })
}

http.interceptors.response.use(
  (resp) => {
    const body = resp.data
    if (body && typeof body === 'object' && 'code' in body) {
      if (body.code !== 0) {
        return Promise.reject(Object.assign(new Error(body.msg || '请求失败'), { handled: true }))
      }
      return body.data
    }
    return body
  },
  async (error) => {
    const { response, config } = error
    const bizCode = response && response.data && response.data.code
    // 4016：同端账号在其他设备登录，本会话被顶替——不尝试续期，直接退出到登录页
    if (response && response.status === 401 && bizCode === 4016) {
      clearAuth()
      ElMessage.error((response.data && response.data.msg) || '账号已在其他设备登录，请重新登录')
      if (router.currentRoute.value.path !== '/login') router.push('/login')
      return Promise.reject(error)
    }
    // 401：先尝试用 refresh_token 续期并重放一次（登录接口除外）
    if (
      response && response.status === 401 &&
      config && config.url !== '/api/auth/login' && !config.__retried
    ) {
      config.__retried = true
      try {
        refreshingPromise = refreshingPromise || doRefresh()
        const token = await refreshingPromise.finally(() => { refreshingPromise = null })
        config.headers.Authorization = `Bearer ${token}`
        return http(config)
      } catch {
        clearAuth()
        ElMessage.error('登录已过期，请重新登录')
        if (router.currentRoute.value.path !== '/login') router.push('/login')
        return Promise.reject(error)
      }
    }
    if (!config || !config.silent) {
      const msg = (response && response.data && response.data.msg) || error.message || '网络请求失败'
      ElMessage.error(msg)
    }
    return Promise.reject(error)
  }
)

// 登录成功后拉取未读告警数（顶栏铃铛徽标）
export function refreshAlertUnread() {
  return http
    .get('/api/alerts', { params: { unread: 1 }, silent: true })
    .then((d) => { store.alertUnread = d.total || 0 })
    .catch(() => { store.alertUnread = 0 })
}

export default http
