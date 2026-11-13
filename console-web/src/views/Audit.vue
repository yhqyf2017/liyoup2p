<template>
  <div class="page-view">
    <div class="page-header">
      <div>
        <h2 class="ph-title">访问审计</h2>
        <p class="ph-desc">平台登录与连接行为的完整审计追溯</p>
      </div>
    </div>

    <div class="filter-card">
      <el-select v-model="action" placeholder="全部动作" clearable style="width: 150px" @change="reload">
        <el-option label="连接" value="connect" />
        <el-option label="关闭" value="close" />
        <el-option label="拦截" value="blocked" />
        <el-option label="登录" value="login" />
      </el-select>
      <div class="f-spacer"></div>
      <el-button :icon="Refresh" circle text @click="reload" />
    </div>

    <div class="page-card table-card">

      <el-table v-loading="loading" :data="items" height="100%" style="width: 100%">
        <el-table-column label="ID" prop="id" width="70" align="center">
          <template #default="{ row }"><span class="num">{{ row.id }}</span></template>
        </el-table-column>
        <el-table-column label="时间" prop="created_at" sortable width="165">
          <template #default="{ row }"><span class="num">{{ fmtTime(row.created_at) }}</span></template>
        </el-table-column>
        <el-table-column label="动作" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="AUDIT_ACTION_TAG_TYPES[row.action] || 'info'" size="small" effect="light">
              {{ AUDIT_ACTION_LABELS[row.action] || row.action }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作者" prop="actor" width="130" show-overflow-tooltip />
        <el-table-column label="租户" prop="tenant_name" min-width="110" show-overflow-tooltip>
          <template #default="{ row }">{{ row.tenant_name || '—' }}</template>
        </el-table-column>
        <el-table-column label="详情" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ row.detail || '—' }}</template>
        </el-table-column>
        <el-table-column label="来源 IP" width="130">
          <template #default="{ row }">
            <span class="mono">{{ row.client_ip || '—' }}</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无审计记录" :image-size="80" />
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
import { onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import http from '../api'
import { fmtTime, AUDIT_ACTION_LABELS, AUDIT_ACTION_TAG_TYPES } from '../utils'

const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const action = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const data = await http.get('/api/audit', {
      params: { page: page.value, page_size: pageSize.value, action: action.value || undefined },
    })
    items.value = data.items || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function onSizeChange() {
  page.value = 1
  load()
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

.mono {
  font-family: 'Consolas', 'Courier New', monospace;
  font-size: 12.5px;
  color: var(--app-text-2);
}
</style>
