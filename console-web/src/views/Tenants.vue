<template>
  <div class="page-view">
    <div class="page-header">
      <div>
        <h2 class="ph-title">租户管理</h2>
        <p class="ph-desc">多租户开通、套餐关联与状态管理</p>
      </div>
      <div class="ph-actions">
        <el-button type="primary" :icon="Plus" @click="openCreate">新建租户</el-button>
      </div>
    </div>

    <div class="filter-card">
      <el-input
        v-model="keyword"
        placeholder="搜索租户名称 / 编码"
        clearable
        :prefix-icon="Search"
        style="width: 220px"
      />
      <div class="f-spacer"></div>
      <el-button :icon="Refresh" circle text @click="load" />
    </div>

    <div class="page-card table-card">

      <el-table v-loading="loading" :data="list" height="100%" style="width: 100%">
        <el-table-column label="ID" prop="id" sortable width="90" align="center">
          <template #default="{ row }"><span class="num">{{ row.id }}</span></template>
        </el-table-column>
        <el-table-column label="租户名称" prop="name" sortable min-width="160" show-overflow-tooltip />
        <el-table-column label="租户编码" min-width="130">
          <template #default="{ row }"><span class="mono">{{ row.code }}</span></template>
        </el-table-column>
        <el-table-column label="套餐" width="120" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.plan_id" type="primary" size="small" effect="light">{{ row.plan_name || `套餐 #${row.plan_id}` }}</el-tag>
            <span v-else class="none-text">未设置</span>
          </template>
        </el-table-column>
        <el-table-column label="设备数" prop="device_count" sortable width="95" align="center">
          <template #default="{ row }"><span class="num">{{ row.device_count }}</span></template>
        </el-table-column>
        <el-table-column label="成员数" prop="user_count" sortable width="95" align="center">
          <template #default="{ row }"><span class="num">{{ row.user_count }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'" size="small" effect="light">
              {{ row.status === 'active' ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" prop="created_at" sortable width="155">
          <template #default="{ row }"><span class="num">{{ fmtTime(row.created_at) }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right" align="center">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="askDelete(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无租户" :image-size="80" />
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

    <!-- 新建 / 编辑 -->
    <el-dialog v-model="editVisible" :title="editingId ? '编辑租户' : '新建租户'" width="460px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="租户名称" required>
          <el-input v-model.trim="editForm.name" placeholder="企业或团队名称" />
        </el-form-item>
        <el-form-item v-if="!editingId" label="租户编码" required>
          <el-input v-model.trim="editForm.code" placeholder="登录时使用的租户编码，创建后不可修改" />
        </el-form-item>
        <el-form-item label="套餐 ID">
          <el-input-number v-model="editForm.plan_id" :min="0" :step="1" style="width: 180px" />
          <span class="plan-tip">0 表示未关联套餐</span>
        </el-form-item>
        <el-form-item v-if="editingId" label="状态">
          <el-radio-group v-model="editForm.status">
            <el-radio value="active">启用</el-radio>
            <el-radio value="disabled">停用</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ editingId ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import http from '../api'
import { fmtTime } from '../utils'

const list = ref([])
const total = ref(0)
const loading = ref(false)
const keyword = ref('')

const page = ref(1)
const pageSize = ref(20)

// 关键词变化：回到第 1 页并重新请求后端
watch(keyword, () => {
  page.value = 1
  load()
})

function onSizeChange() {
  page.value = 1
  load()
}

async function load() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value }
    if (keyword.value.trim()) params.keyword = keyword.value.trim()
    const data = await http.get('/api/tenants', { params })
    list.value = data.items || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

const editVisible = ref(false)
const saving = ref(false)
const editingId = ref(0)
const editForm = ref({ name: '', code: '', plan_id: 0, status: 'active' })

function openCreate() {
  editingId.value = 0
  editForm.value = { name: '', code: '', plan_id: 0, status: 'active' }
  editVisible.value = true
}

function openEdit(row) {
  editingId.value = row.id
  editForm.value = { name: row.name, plan_id: row.plan_id || 0, status: row.status }
  editVisible.value = true
}

async function save() {
  if (!editForm.value.name || (!editingId.value && !editForm.value.code)) {
    ElMessage.warning('租户名称与编码不能为空')
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await http.put(`/api/tenants/${editingId.value}`, {
        name: editForm.value.name,
        status: editForm.value.status,
        plan_id: editForm.value.plan_id,
      })
      ElMessage.success('租户信息已更新')
    } else {
      await http.post('/api/tenants', {
        name: editForm.value.name,
        code: editForm.value.code,
        plan_id: editForm.value.plan_id || 0,
      })
      ElMessage.success('租户创建成功')
    }
    editVisible.value = false
    load()
  } finally {
    saving.value = false
  }
}

function askDelete(row) {
  ElMessageBox.confirm(
    `确定要删除租户「${row.name}」（${row.code}）吗？其下 ${row.device_count} 台设备、${row.user_count} 名成员的关联数据将受到影响，操作不可恢复。`,
    '删除租户',
    { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning', confirmButtonClass: 'el-button--danger' }
  )
    .then(async () => {
      await http.delete(`/api/tenants/${row.id}`)
      ElMessage.success('租户已删除')
      load()
    })
    .catch(() => {})
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

.none-text {
  color: var(--app-text-3);
  font-size: 12.5px;
}

.plan-tip {
  margin-left: 10px;
  font-size: 12px;
  color: var(--app-text-3);
}
</style>
