<template>
  <div class="page-view">
    <div class="page-header">
      <div>
        <h2 class="ph-title">成员管理</h2>
        <p class="ph-desc">管理平台登录账号与角色权限</p>
      </div>
      <div class="ph-actions">
        <el-button v-if="canWriteUsers()" type="primary" :icon="Plus" @click="openCreate">
          新建成员
        </el-button>
      </div>
    </div>

    <div class="filter-card">
      <el-input
        v-model="keyword"
        placeholder="搜索用户名 / 所属租户"
        clearable
        :prefix-icon="Search"
        style="width: 220px"
      />
      <div class="f-spacer"></div>
      <el-button :icon="Refresh" circle text @click="load" />
    </div>

    <div class="page-card table-card">

      <!-- 后端 UserView 无 JSON tag，字段为 PascalCase：ID / TenantID{Int64,Valid} / TenantName / Username / Role / Status / CreatedAt / LastLoginAt{Time,Valid} -->
      <el-table v-loading="loading" :data="filteredList" style="width: 100%" :span-method="tenantSpanMethod">
        <el-table-column label="ID" width="70" align="center">
          <template #default="{ row }"><span class="num">{{ row.ID }}</span></template>
        </el-table-column>
        <el-table-column label="用户名" prop="Username" min-width="130">
          <template #default="{ row }">
            <span class="username">{{ row.Username }}</span>
            <el-tag v-if="isSelf(row)" size="small" effect="plain" class="self-tag">当前账号</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="角色" width="120" align="center">
          <template #default="{ row }">
            <el-tag :type="ROLE_TAG_TYPES[row.Role] || 'info'" size="small" effect="light">
              {{ ROLE_LABELS[row.Role] || row.Role }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="所属租户" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.TenantName || '平台' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.Status === 'active' ? 'success' : 'danger'" size="small" effect="light">
              {{ row.Status === 'active' ? '正常' : '已禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="150">
          <template #default="{ row }"><span class="num">{{ fmtTime(row.CreatedAt) }}</span></template>
        </el-table-column>
        <el-table-column label="最近登录" width="150">
          <template #default="{ row }"><span class="num">{{ fmtTime(row.LastLoginAt) }}</span></template>
        </el-table-column>
        <el-table-column v-if="canWriteUsers()" label="操作" width="200" fixed="right" align="center">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="primary" size="small" @click="openResetPwd(row)">重置密码</el-button>
            <el-button link type="danger" size="small" :disabled="isSelf(row)" @click="askDelete(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无成员账号" :image-size="80" />
        </template>
      </el-table>
      <div class="table-foot num">共 {{ filteredList.length }} 名成员</div>
    </div>

    <!-- 新建 / 编辑 -->
    <el-dialog v-model="editVisible" :title="editingId ? '编辑成员' : '新建成员'" width="480px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item v-if="!editingId" label="用户名" required>
          <el-input v-model.trim="editForm.username" placeholder="登录账号，创建后不可修改" />
        </el-form-item>
        <el-form-item v-if="!editingId" label="初始密码" required>
          <el-input v-model="editForm.password" type="password" show-password placeholder="至少 6 位" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="editForm.role" style="width: 100%">
            <el-option
              v-for="r in roleOptions"
              :key="r.value"
              :label="r.label"
              :value="r.value"
              :disabled="editingId && isSelfById() && r.value !== (currentRow && currentRow.Role)"
            />
          </el-select>
        </el-form-item>
        <el-form-item v-if="isSuper() && !editingId" label="所属租户">
          <el-select
            v-model="editForm.tenant_id"
            style="width: 100%"
            :disabled="editForm.role === 'super_admin'"
            placeholder="超级管理员固定归属平台"
          >
            <el-option label="平台（无租户）" :value="0" />
            <el-option v-for="t in tenants" :key="t.id" :label="`${t.name}（${t.code}）`" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="editingId" label="状态">
          <el-radio-group v-model="editForm.status">
            <el-radio value="active">正常</el-radio>
            <el-radio value="disabled">禁用</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ editingId ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <!-- 重置密码 -->
    <el-dialog v-model="pwdVisible" :title="`重置密码 - ${pwdRow && pwdRow.Username}`" width="420px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="新密码" required>
          <el-input v-model="pwdForm.password" type="password" show-password placeholder="至少 6 位" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pwdVisible = false">取消</el-button>
        <el-button type="primary" :loading="pwdSaving" @click="doResetPwd">确认重置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import http from '../api'
import { store, isSuper, canWriteUsers, ROLE_LABELS, ROLE_TAG_TYPES } from '../store'
import { fmtTime } from '../utils'

const list = ref([])
const loading = ref(false)
const keyword = ref('')
const tenants = ref([])

const currentUserId = computed(() => (store.user ? store.user.id : 0))
const isSelf = (row) => row.ID === currentUserId.value
const isSelfById = () => currentRow.value && currentRow.value.ID === currentUserId.value

// 按租户聚合展示：同租户成员相邻，租户名为空（平台账号）排最后
const filteredList = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  const arr = kw
    ? list.value.filter((u) => `${u.Username} ${u.TenantName || ''}`.toLowerCase().includes(kw))
    : list.value.slice()
  return arr.sort((a, b) => {
    const an = a.TenantName || ''
    const bn = b.TenantName || ''
    if (!an && bn) return 1
    if (an && !bn) return -1
    return an.localeCompare(bn)
  })
})

// 「所属租户」列（第 4 列）合并同租户单元格；仅一组（单租户视角）时正常整列合并显示
const TENANT_COLUMN_INDEX = 3

function tenantSpanMethod({ row, rowIndex, columnIndex }) {
  if (columnIndex !== TENANT_COLUMN_INDEX) return
  const rows = filteredList.value
  const name = row.TenantName || ''
  if (rowIndex > 0 && (rows[rowIndex - 1].TenantName || '') === name) {
    return { rowspan: 0, colspan: 0 }
  }
  let rowspan = 1
  for (let i = rowIndex + 1; i < rows.length; i++) {
    if ((rows[i].TenantName || '') !== name) break
    rowspan++
  }
  return { rowspan, colspan: 1 }
}

const roleOptions = computed(() => {
  if (isSuper()) {
    return [
      { value: 'super_admin', label: '超级管理员' },
      { value: 'tenant_admin', label: '租户管理员' },
      { value: 'operator', label: '操作员' },
      { value: 'auditor', label: '审计员' },
    ]
  }
  return [
    { value: 'tenant_admin', label: '租户管理员' },
    { value: 'operator', label: '操作员' },
    { value: 'auditor', label: '审计员' },
  ]
})

async function load() {
  loading.value = true
  try {
    const data = await http.get('/api/users')
    list.value = data.items || []
  } finally {
    loading.value = false
  }
}

async function ensureTenants() {
  if (isSuper() && tenants.value.length === 0) {
    try {
      const data = await http.get('/api/tenants', { silent: true })
      tenants.value = data.items || []
    } catch {
      tenants.value = []
    }
  }
}

// 新建 / 编辑
const editVisible = ref(false)
const saving = ref(false)
const editingId = ref(0)
const currentRow = ref(null)
const editForm = ref({ username: '', password: '', role: 'operator', status: 'active', tenant_id: 0 })

async function openCreate() {
  editingId.value = 0
  currentRow.value = null
  editForm.value = { username: '', password: '', role: 'operator', status: 'active', tenant_id: 0 }
  await ensureTenants()
  editVisible.value = true
}

function openEdit(row) {
  editingId.value = row.ID
  currentRow.value = row
  editForm.value = { role: row.Role, status: row.Status }
  editVisible.value = true
}

async function save() {
  saving.value = true
  try {
    if (editingId.value) {
      await http.put(`/api/users/${editingId.value}`, {
        role: editForm.value.role,
        status: editForm.value.status,
      })
      ElMessage.success('成员信息已更新')
    } else {
      if (!editForm.value.username) {
        ElMessage.warning('请输入用户名')
        return
      }
      if (!editForm.value.password || editForm.value.password.length < 6) {
        ElMessage.warning('初始密码至少 6 位')
        return
      }
      const payload = {
        username: editForm.value.username,
        password: editForm.value.password,
        role: editForm.value.role,
      }
      if (isSuper() && editForm.value.role !== 'super_admin') {
        payload.tenant_id = editForm.value.tenant_id || 0
      }
      await http.post('/api/users', payload)
      ElMessage.success('成员创建成功')
    }
    editVisible.value = false
    load()
  } finally {
    saving.value = false
  }
}

// 重置密码
const pwdVisible = ref(false)
const pwdSaving = ref(false)
const pwdRow = ref(null)
const pwdForm = ref({ password: '' })

function openResetPwd(row) {
  pwdRow.value = row
  pwdForm.value = { password: '' }
  pwdVisible.value = true
}

async function doResetPwd() {
  if (!pwdForm.value.password || pwdForm.value.password.length < 6) {
    ElMessage.warning('新密码至少 6 位')
    return
  }
  pwdSaving.value = true
  try {
    await http.post(`/api/users/${pwdRow.value.ID}/reset-password`, { password: pwdForm.value.password })
    ElMessage.success('密码已重置')
    pwdVisible.value = false
  } finally {
    pwdSaving.value = false
  }
}

// 删除
function askDelete(row) {
  ElMessageBox.confirm(
    `确定要删除成员「${row.Username}」吗？删除后该账号将无法登录，操作不可恢复。`,
    '删除成员',
    { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning', confirmButtonClass: 'el-button--danger' }
  )
    .then(async () => {
      await http.delete(`/api/users/${row.ID}`)
      ElMessage.success('成员已删除')
      load()
    })
    .catch(() => {})
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

.username {
  color: #16233C;
  font-weight: 500;
}

.self-tag {
  margin-left: 8px;
}
</style>
