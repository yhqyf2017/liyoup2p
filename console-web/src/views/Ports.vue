<template>
  <div class="page-view">
    <div class="page-header">
      <div>
        <h2 class="ph-title">服务端口</h2>
        <p class="ph-desc">设备自动上报的服务端口清单，随设备状态实时更新</p>
      </div>
    </div>

    <div class="filter-card">
      <el-input
        v-model="keyword"
        placeholder="搜索设备 UID / 名称 / 端口"
        clearable
        :prefix-icon="Search"
        style="width: 220px"
      />
      <div class="f-spacer"></div>
      <el-button :icon="Refresh" circle text @click="load" />
    </div>

    <div class="page-card table-card">

      <el-table v-loading="loading" :data="filteredList" style="width: 100%">
        <el-table-column label="设备 UID" min-width="160">
          <template #default="{ row }"><span class="mono">{{ row.DeviceUID }}</span></template>
        </el-table-column>
        <el-table-column label="设备名称" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">{{ row.DeviceName || '—' }}</template>
        </el-table-column>
        <el-table-column label="租户" min-width="110" show-overflow-tooltip>
          <template #default="{ row }">{{ row.TenantName || '—' }}</template>
        </el-table-column>
        <el-table-column label="账号" min-width="110" show-overflow-tooltip>
          <template #default="{ row }">{{ row.Owner || '—' }}</template>
        </el-table-column>
        <el-table-column label="角色" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.Role === 'server' ? 'primary' : 'success'" size="small" effect="light">
              {{ DEVICE_ROLE_LABELS[row.Role] || row.Role }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="设备状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="DEVICE_STATUS_TAG_TYPES[row.Status] || 'info'" size="small" effect="light">
              {{ DEVICE_STATUS_LABELS[row.Status] || row.Status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="端口" width="90" align="center">
          <template #default="{ row }"><span class="num port-num">{{ row.Port }}</span></template>
        </el-table-column>
        <el-table-column label="服务标签" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.Label || '未标注' }}</template>
        </el-table-column>
        <el-table-column label="上报类型" width="110" align="center">
          <template #default="{ row }">
            <el-tag :type="row.Declared ? 'primary' : 'info'" size="small" effect="light">
              {{ row.Declared ? '已声明' : '自动探测' }}
            </el-tag>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无服务端口，设备接入并上报后在此展示" :image-size="80" />
        </template>
      </el-table>
      <div class="table-foot num">共 {{ filteredList.length }} 条端口记录</div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import http from '../api'
import {
  DEVICE_ROLE_LABELS,
  DEVICE_STATUS_LABELS,
  DEVICE_STATUS_TAG_TYPES,
} from '../utils'

const list = ref([])
const loading = ref(false)
const keyword = ref('')

// 后端 PortView 结构体无 JSON tag，字段为 PascalCase：ID / DeviceUID / DeviceName / Role / Status / Port / Label / Declared / TenantName / Owner
const filteredList = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return list.value
  return list.value.filter(
    (p) =>
      `${p.DeviceUID} ${p.DeviceName} ${p.Port} ${p.Label || ''}`.toLowerCase().includes(kw)
  )
})

async function load() {
  loading.value = true
  try {
    const data = await http.get('/api/ports')
    list.value = data.items || []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.table-card {
  padding: var(--sp-3);
}

.table-foot {
  padding: var(--sp-2) var(--sp-2) 0;
  font-size: 12.5px;
  color: #8C99AE;
}

.mono {
  font-family: 'Consolas', 'Courier New', monospace;
  font-size: 12.5px;
  color: #57677F;
}

.port-num {
  font-weight: 600;
  color: #16233C;
}
</style>
