import { createRouter, createWebHistory } from 'vue-router'
import { store } from '../store'

const routes = [
  {
    path: '/login',
    component: () => import('../views/Login.vue'),
    meta: { title: '登录', public: true },
  },
  {
    path: '/',
    component: () => import('../layouts/Layout.vue'),
    redirect: '/dashboard',
    children: [
      { path: 'dashboard', component: () => import('../views/Dashboard.vue'), meta: { title: '工作台' } },
      { path: 'devices', component: () => import('../views/Devices.vue'), meta: { title: '设备管理' } },
      { path: 'ports', component: () => import('../views/Ports.vue'), meta: { title: '服务端口' } },
      { path: 'audit', component: () => import('../views/Audit.vue'), meta: { title: '访问审计' } },
      { path: 'alerts', component: () => import('../views/Alerts.vue'), meta: { title: '告警中心' } },
      { path: 'users', component: () => import('../views/Users.vue'), meta: { title: '成员管理' } },
      { path: 'tenants', component: () => import('../views/Tenants.vue'), meta: { title: '租户管理', roles: ['super_admin'] } },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach((to) => {
  if (!to.meta.public && !store.accessToken) return { path: '/login' }
  if (to.path === '/login' && store.accessToken) return { path: '/dashboard' }
  if (to.meta.roles && !to.meta.roles.includes(store.user && store.user.role)) {
    return { path: '/dashboard' }
  }
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} - 励友 P2P 管理控制台` : '励友 P2P 管理控制台'
})

export default router
