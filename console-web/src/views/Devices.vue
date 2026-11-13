<template>
  <div class="page-view">
    <div class="page-header">
      <div>
        <h2 class="ph-title">设备管理</h2>
        <p class="ph-desc">管理接入平台的内网设备，注册后签发设备令牌供 Agent 接入</p>
      </div>
      <div class="ph-actions">
        <el-button v-if="canOperateDevices()" type="primary" :icon="Plus" @click="openRegister">
          注册设备
        </el-button>
      </div>
    </div>

    <div class="filter-card">
      <el-select
        v-if="isSuper()"
        v-model="filterTenant"
        clearable
        placeholder="全部租户"
        style="width: 180px"
      >
        <el-option label="平台设备" :value="-1" />
        <el-option v-for="t in tenants" :key="t.id" :label="`${t.name}（${t.code}）`" :value="t.id" />
      </el-select>
      <el-select v-model="filterRole" placeholder="设备角色" clearable style="width: 150px">
        <el-option label="服务端" value="server" />
        <el-option label="客户端" value="client" />
      </el-select>
      <el-select v-model="filterStatus" placeholder="设备状态" clearable style="width: 150px">
        <el-option label="在线" value="online" />
        <el-option label="离线" value="offline" />
        <el-option label="已吊销" value="revoked" />
      </el-select>
      <el-input
        v-model="keyword"
        placeholder="搜索设备 UID / 名称 / 账号 / 租户"
        clearable
        :prefix-icon="Search"
        style="width: 240px"
      />
      <div class="f-spacer"></div>
      <el-button :icon="Refresh" circle text @click="load" />
    </div>

    <div class="page-card table-card">

      <el-table v-loading="loading" :data="list" height="100%" style="width: 100%">
        <el-table-column label="设备 UID" prop="device_uid" min-width="160">
          <template #default="{ row }"><span class="mono">{{ row.device_uid }}</span></template>
        </el-table-column>
        <el-table-column label="设备名称" prop="name" sortable min-width="150" show-overflow-tooltip />
        <el-table-column label="角色" width="95" align="center" sortable sort-by="role">
          <template #default="{ row }">
            <el-tag :type="row.role === 'server' ? 'primary' : 'success'" size="small" effect="light">
              {{ DEVICE_ROLE_LABELS[row.role] || row.role }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="95" align="center" sortable sort-by="status">
          <template #default="{ row }">
            <el-tag :type="DEVICE_STATUS_TAG_TYPES[row.status] || 'info'" size="small" effect="light">
              {{ DEVICE_STATUS_LABELS[row.status] || row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="所属租户" prop="tenant_name" min-width="120" show-overflow-tooltip />
        <el-table-column label="账号" prop="owner" min-width="110" show-overflow-tooltip>
          <template #default="{ row }">{{ row.owner || '—' }}</template>
        </el-table-column>
        <el-table-column label="操作系统" prop="os_info" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.os_info || '—' }}</template>
        </el-table-column>
        <el-table-column label="端口数" width="80" align="center">
          <template #default="{ row }"><span class="num">{{ row.port_count }}</span></template>
        </el-table-column>
        <el-table-column label="最近在线" prop="last_online_at" sortable width="155">
          <template #default="{ row }"><span class="num">{{ fmtTime(row.last_online_at) }}</span></template>
        </el-table-column>
        <el-table-column label="注册时间" prop="created_at" sortable width="155">
          <template #default="{ row }"><span class="num">{{ fmtTime(row.created_at) }}</span></template>
        </el-table-column>
        <el-table-column v-if="canOperateDevices()" label="操作" width="90" fixed="right" align="center">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="askRevoke(row)">吊销</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无设备，点击右上角「注册设备」开始接入" :image-size="80" />
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

    <!-- 注册设备 -->
    <el-dialog v-model="regVisible" title="注册设备" width="480px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="设备 UID">
          <el-input v-model.trim="regForm.device_uid" placeholder="留空自动生成" />
        </el-form-item>
        <el-form-item label="设备名称">
          <el-input v-model.trim="regForm.name" placeholder="例如：财务部应用服务器" />
        </el-form-item>
        <el-form-item label="设备角色" required>
          <el-radio-group v-model="regForm.role">
            <el-radio value="server">服务端</el-radio>
            <el-radio value="client">客户端</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="isSuper()" label="所属租户">
          <el-select v-model="regForm.tenant_id" placeholder="选择租户" style="width: 100%">
            <el-option v-for="t in tenants" :key="t.id" :label="`${t.name}（${t.code}）`" :value="t.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="regVisible = false">取消</el-button>
        <el-button type="primary" :loading="regLoading" @click="doRegister">注册并签发令牌</el-button>
      </template>
    </el-dialog>

    <!-- 令牌签发结果（仅展示一次） -->
    <el-dialog v-model="tokenVisible" title="设备令牌签发成功" width="520px" :close-on-click-modal="false">
      <el-alert
        title="设备令牌仅本次展示，请立即复制并保存到设备配置中"
        type="warning"
        show-icon
        :closable="false"
        class="token-alert"
      />
      <el-descriptions :column="1" border>
        <el-descriptions-item label="设备 UID">
          <span class="mono">{{ tokenResult.device_uid }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="设备角色">{{ DEVICE_ROLE_LABELS[tokenResult.role] || tokenResult.role }}</el-descriptions-item>
        <el-descriptions-item label="有效期">{{ tokenResult.expires_in }}</el-descriptions-item>
        <el-descriptions-item label="设备令牌">
          <div class="token-row">
            <el-input :model-value="tokenResult.device_token" readonly type="textarea" :rows="3" class="token-input" />
            <el-button type="primary" :icon="CopyDocument" @click="copyToken">复制</el-button>
          </div>
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button type="primary" @click="tokenVisible = false">我已保存</el-button>
      </template>
    </el-dialog>

    <!-- 吊销确认 -->
    <el-dialog v-model="revokeVisible" title="吊销设备确认" width="440px">
      <div class="revoke-body">
        <el-icon class="revoke-icon" :size="26"><WarningFilled /></el-icon>
        <div>
          <p class="revoke-line">
            确定要吊销设备 <b>{{ revokeRow && revokeRow.name }}</b>
            <span class="mono">（{{ revokeRow && revokeRow.device_uid }}）</span> 吗？
          </p>
          <p class="revoke-sub">吊销后设备令牌立即失效、设备将被强制下线，该操作不可恢复。</p>
        </div>
      </div>
      <template #footer>
        <el-button @click="revokeVisible = false">取消</el-button>
        <el-button type="danger" :loading="revokeLoading" @click="doRevoke">确认吊销</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search, CopyDocument } from '@element-plus/icons-vue'
import http from '../api'
import { isSuper, canOperateDevices } from '../store'
import {
  fmtTime,
  DEVICE_ROLE_LABELS,
  DEVICE_STATUS_LABELS,
  DEVICE_STATUS_TAG_TYPES,
} from '../utils'

const list = ref([])
const total = ref(0)
const loading = ref(false)
const filterRole = ref('')
const filterStatus = ref('')
const filterTenant = ref(null) // null=全部（显示占位符），-1=平台设备，>0=租户 ID
const keyword = ref('')
const tenants = ref([])

const page = ref(1)
const pageSize = ref(20)

// 筛选条件变化：回到第 1 页并重新请求后端
watch([keyword, filterTenant, filterRole, filterStatus], () => {
  page.value = 1
  load()
})

function onSizeChange() {
  page.value = 1
  load()
}

async function ensureTenants() {
  if (isSuper() && tenants.value.length === 0) {
    try {
      const data = await http.get('/api/tenants', { params: { page_size: 100 }, silent: true })
      tenants.value = data.items || []
    } catch {
      tenants.value = []
    }
  }
}

async function load() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value }
    if (keyword.value.trim()) params.keyword = keyword.value.trim()
    if (filterTenant.value) params.tenant_id = filterTenant.value
    if (filterRole.value) params.role = filterRole.value
    if (filterStatus.value) params.status = filterStatus.value
    const data = await http.get('/api/devices', { params })
    list.value = data.items || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

// 注册
const regVisible = ref(false)
const regLoading = ref(false)
const regForm = ref({ device_uid: '', name: '', role: 'server', tenant_id: null })

async function openRegister() {
  regForm.value = { device_uid: '', name: '', role: 'server', tenant_id: null }
  if (isSuper()) await ensureTenants()
  regVisible.value = true
}

async function doRegister() {
  if (!regForm.value.role) {
    ElMessage.warning('请选择设备角色')
    return
  }
  if (isSuper() && !regForm.value.tenant_id) {
    ElMessage.warning('请选择所属租户')
    return
  }
  regLoading.value = true
  try {
    const payload = {
      device_uid: regForm.value.device_uid || undefined,
      name: regForm.value.name || regForm.value.device_uid || '未命名设备',
      role: regForm.value.role,
    }
    if (isSuper()) payload.tenant_id = regForm.value.tenant_id
    const data = await http.post('/api/devices/register', payload)
    regVisible.value = false
    tokenResult.value = data
    tokenVisible.value = true
    load()
  } finally {
    regLoading.value = false
  }
}

// 令牌结果展示
const tokenVisible = ref(false)
const tokenResult = ref({})

function copyToken() {
  navigator.clipboard
    .writeText(tokenResult.value.device_token || '')
    .then(() => ElMessage.success('令牌已复制到剪贴板'))
    .catch(() => ElMessage.error('复制失败，请手动选择复制'))
}

// 吊销（el-dialog 确认）
const revokeVisible = ref(false)
const revokeLoading = ref(false)
const revokeRow = ref(null)

function askRevoke(row) {
  revokeRow.value = row
  revokeVisible.value = true
}

async function doRevoke() {
  if (!revokeRow.value) return
  revokeLoading.value = true
  try {
    await http.delete(`/api/devices/${revokeRow.value.id}`)
    ElMessage.success('设备已吊销')
    revokeVisible.value = false
    load()
  } finally {
    revokeLoading.value = false
  }
}

onMounted(() => {
  load()
  ensureTenants()
})
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

.token-alert {
  margin-bottom: 14px;
}

.token-row {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}

.token-input {
  flex: 1;
}

.revoke-body {
  display: flex;
  gap: 14px;
  align-items: flex-start;
}

.revoke-icon {
  color: var(--app-danger);
  flex-shrink: 0;
  margin-top: 2px;
}

.revoke-line {
  margin: 0 0 8px;
  color: var(--app-text-1);
  font-size: 14px;
}

.revoke-sub {
  margin: 0;
  color: var(--app-text-3);
  font-size: 12.5px;
  line-height: 1.6;
}
</style>
