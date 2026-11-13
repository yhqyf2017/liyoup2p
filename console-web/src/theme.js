// 主题管理：约定默认（html 无 light 类）= 暗黑；'light' 时切换为 html.light。
// Element Plus 暗黑变量依赖 html.dark，因此暗黑时同时加 dark 类、浅色时移除。
const THEME_KEY = 'console-theme'

export function getTheme() {
  return localStorage.getItem(THEME_KEY) === 'light' ? 'light' : 'dark'
}

export function setTheme(theme) {
  localStorage.setItem(THEME_KEY, theme === 'light' ? 'light' : 'dark')
  applyTheme(getTheme())
}

export function applyTheme(theme) {
  const el = document.documentElement
  const light = theme === 'light'
  el.classList.toggle('light', light)
  el.classList.toggle('dark', !light)
}
