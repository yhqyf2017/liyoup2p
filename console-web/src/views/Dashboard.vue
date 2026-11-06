<template>
  <div class="page-view">
    <!-- 页头 -->
    <div class="page-header">
      <div>
        <h2 class="ph-title">工作台</h2>
        <p class="ph-desc">平台运行状态总览，KPI 与会话数据实时刷新</p>
      </div>
    </div>

    <!-- KPI 指标卡 -->
    <div class="kpi-grid">
      <div v-for="card in kpiCards" :key="card.label" class="page-card kpi-card">
        <div class="kpi-icon" :style="{ background: card.iconBg, color: card.iconColor }">
          <el-icon :size="22"><component :is="card.icon" /></el-icon>
        </div>
        <div class="kpi-body">
          <div class="kpi-label">{{ card.label }}</div>
          <div class="kpi-value num">{{ card.value }}</div>
          <div class="kpi-sub">{{ card.sub }}</div>
        </div>
      </div>
    </div>

    <!-- 最近隧道会话 -->
    <div class="page-card table-card">
      <div class="card-head">
        <div>
          <div class="page-title">最近隧道会话</div>
          <div class="page-desc">展示最近的 P2P 直连与中继转发记录</div>
        </div>
        <el-button :icon="Refresh" circle text @click="loadSessions" />
      </div>
      <el-table v-loading="loading" :data="sessions" style="width: 100%">
        <el-table-column label="客户端" prop="client_device_uid" min-width="150">
          <template #default="{ row }"><span class="mono">{{ row.client_device_uid }}</span></template>
        </el-table-column>
        <el-table-column label="服务端" prop="server_device_uid" min-width="150">
          <template #default="{ row }"><span class="mono">{{ row.server_device_uid }}</span></template>
        </el-table-column>
        <el-table-column label="端口" width="80" align="center">
          <template #default="{ row }"><span class="num">{{ row.port }}</span></template>
        </el-table-column>
        <el-table-column label="模式" width="110" align="center">
          <template #default="{ row }">
            <el-tag :type="row.mode === 'p2p' ? 'success' : 'warning'" size="small" effect="light">
              {{ SESSION_MODE_LABELS[row.mode] || row.mode }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small" effect="light">
              {{ row.status === 'active' ? '进行中' : '已结束' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="中继流量" width="110" align="right">
          <template #default="{ row }"><span class="num">{{ fmtBytes(row.relay_bytes) }}</span></template>
        </el-table-column>
        <el-table-column label="开始时间" width="150">
          <template #default="{ row }"><span class="num">{{ fmtTime(row.started_at) }}</span></template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无隧道会话记录" :image-size="80" />
        </template>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import http from '../api'
import { fmtBytes, fmtTime, SESSION_MODE_LABELS } from '../utils'

const stats = ref(null)
const sessions = ref([])
const loading = ref(false)

const kpiCards = computed(() => {
  const s = stats.value || {}
  return [
    {
      label: '在线设备',
      value: `${s.online_devices ?? 0} / ${s.total_devices ?? 0}`,
      sub: '当前在线 / 总设备数',
      icon: 'Monitor',
      iconBg: '#E8F1FC',
      iconColor: '#1667C8',
    },
    {
      label: '今日请求',
      value: s.today_requests ?? 0,
      sub: '今日连接请求总数',
      icon: 'DataLine',
      iconBg: '#E6F6EF',
      iconColor: '#1E9E63',
    },
    {
      label: '直连率',
      value: `${s.p2p_rate ?? 0}%`,
      sub: `今日会话 ${s.today_sessions ?? 0} 次`,
      icon: 'Connection',
      iconBg: '#FDF3E4',
      iconColor: '#B97A0D',
    },
    {
      label: '中继流量',
      value: fmtBytes(s.relay_bytes ?? 0),
      sub: '今日中继转发流量',
      icon: 'Histogram',
      iconBg: '#F0EBFB',
      iconColor: '#6C4FC4',
    },
  ]
})

async function loadStats() {
  stats.value = await http.get('/api/stats', { silent: true }).catch(() => null)
}

async function loadSessions() {
  loading.value = true
  try {
    const data = await http.get('/api/sessions')
    sessions.value = (data.items || []).slice(0, 8)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadStats()
  loadSessions()
})
</script>

<style scoped>
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.kpi-card {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding: 18px 20px;
}

.kpi-icon {
  width: 52px;
  height: 52px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.kpi-label {
  font-size: 13px;
  color: #57677F;
  margin-bottom: var(--sp-2);
}

.kpi-value {
  font-size: 26px;
  font-weight: 700;
  color: #16233C;
  line-height: 1.3;
  margin: 0 0 var(--sp-1);
}

.kpi-sub {
  font-size: 12px;
  color: #8C99AE;
}

.table-card {
  padding: var(--sp-3);
}

.card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: var(--sp-1) var(--sp-2) var(--sp-3);
}

.mono {
  font-family: 'Consolas', 'Courier New', monospace;
  font-size: 12.5px;
  color: #57677F;
}
</style>
