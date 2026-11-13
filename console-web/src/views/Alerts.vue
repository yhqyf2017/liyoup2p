<template>
  <div class="page-view">
    <div class="page-header">
      <div>
        <h2 class="ph-title">告警中心</h2>
        <p class="ph-desc">
          设备连接异常与安全事件通知，共 {{ total }} 条，未读
          <b class="unread-count">{{ unreadCount }}</b> 条
        </p>
      </div>
      <div class="ph-actions">
        <el-button :icon="Refresh" circle text @click="load" />
        <el-button type="primary" plain :disabled="unreadCount === 0" :loading="readAllLoading" @click="readAll">
          全部标记已读
        </el-button>
      </div>
    </div>

    <div class="page-card table-card">

      <el-table v-loading="loading" :data="items" height="100%" style="width: 100%" :row-class-name="rowClass">
        <el-table-column label="级别" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="ALERT_LEVEL_TAG_TYPES[row.level] || 'info'" size="small" effect="light">
              {{ ALERT_LEVEL_LABELS[row.level] || row.level }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="标题" width="150">
          <template #default="{ row }">
            <span class="alert-title">
              <span v-if="!row.is_read" class="unread-dot"></span>{{ row.title }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="租户" prop="tenant_name" min-width="110" show-overflow-tooltip>
          <template #default="{ row }">{{ row.tenant_name || '—' }}</template>
        </el-table-column>
        <el-table-column label="告警详情" min-width="260" show-overflow-tooltip>
          <template #default="{ row }">{{ row.detail || '—' }}</template>
        </el-table-column>
        <el-table-column label="发生时间" prop="created_at" sortable width="165">
          <template #default="{ row }"><span class="num">{{ fmtTime(row.created_at) }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.is_read ? 'info' : 'danger'" size="small" effect="light">
              {{ row.is_read ? '已读' : '未读' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110" align="center">
          <template #default="{ row }">
            <el-button v-if="!row.is_read" link type="primary" size="small" @click="markRead(row)">
              标记已读
            </el-button>
            <span v-else class="read-done">—</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无告警，系统运行良好" :image-size="80" />
        </template>
      </el-table>
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        background
        class="table-pager"
        @current-change="load"
        @size-change="onSizeChange"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import http, { refreshAlertUnread } from '../api'
import { store } from '../store'
import { fmtTime, ALERT_LEVEL_LABELS, ALERT_LEVEL_TAG_TYPES } from '../utils'

const items = ref([])
const total = ref(0)
const loading = ref(false)
const readAllLoading = ref(false)

const page = ref(1)
const pageSize = ref(20)

// 未读数取全局徽标值（顶栏与后端统计一致），而非当前页数据
const unreadCount = computed(() => store.alertUnread)

function onSizeChange() {
  page.value = 1
  load()
}

function rowClass({ row }) {
  return row.is_read ? 'row-read' : ''
}

async function load() {
  loading.value = true
  try {
    const data = await http.get('/api/alerts', { params: { page: page.value, page_size: pageSize.value } })
    items.value = data.items || []
    total.value = data.total || 0
    refreshAlertUnread()
  } finally {
    loading.value = false
  }
}

async function markRead(row) {
  await http.post(`/api/alerts/${row.id}/read`)
  row.is_read = true
  store.alertUnread = Math.max(0, store.alertUnread - 1)
  ElMessage.success('已标记为已读')
}

async function readAll() {
  readAllLoading.value = true
  try {
    await http.post('/api/alerts/read-all')
    await load()
    refreshAlertUnread()
    ElMessage.success('全部告警已标记为已读')
  } finally {
    readAllLoading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.table-card {
  padding: var(--sp-3);
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.table-card > :deep(.el-table) {
  flex: 1;
  min-height: 0;
}

.unread-count {
  color: var(--app-danger);
  font-weight: 700;
}

.alert-title {
  color: var(--app-text-1);
  font-weight: 500;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.unread-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--app-danger);
  flex-shrink: 0;
}

.read-done {
  color: var(--app-text-3);
}

:deep(.row-read) {
  color: var(--app-text-3);
}
</style>
