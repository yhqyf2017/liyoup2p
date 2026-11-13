<template>
  <div class="page-view">
    <div class="page-header">
      <div>
        <h2 class="ph-title">版本发布</h2>
        <p class="ph-desc">PC / 安卓 / Win7 三通道安装包发布与版本管理</p>
      </div>
      <div class="ph-actions">
        <el-button :icon="Refresh" circle text @click="loadAll" />
      </div>
    </div>

    <!-- 三通道当前版本 -->
    <div class="channel-grid">
      <div v-for="ch in ['pc', 'android', 'win7']" :key="ch" class="page-card channel-card">
        <div class="ch-head">
          <div class="ch-name">
            <el-icon :size="18" class="ch-icon"><Monitor v-if="ch === 'pc'" /><Iphone v-else-if="ch === 'android'" /><Cpu v-else /></el-icon>
            <span>{{ channelName(ch) }}</span>
          </div>
          <el-tag v-if="channels[ch] && channels[ch].version" type="success" size="small" effect="light">已发布</el-tag>
          <el-tag v-else type="info" size="small" effect="light">未发布</el-tag>
        </div>
        <template v-if="channels[ch] && channels[ch].version">
          <div class="ch-version">{{ channels[ch].version }}</div>
          <div class="ch-row"><span class="ch-label">包地址</span><span class="mono ch-url">{{ channels[ch].url }}</span></div>
          <div class="ch-row"><span class="ch-label">SHA256</span><span class="mono">{{ shortSha(channels[ch].sha256) }}</span></div>
          <div class="ch-row ch-notes"><span class="ch-label">更新说明</span><span class="ch-notes-text">{{ channels[ch].notes || '—' }}</span></div>
          <div class="ch-row"><span class="ch-label">发布时间</span><span class="num">{{ fmtTime(channels[ch].releasedAt) }}</span></div>
        </template>
        <el-empty v-else description="该通道尚未发布任何版本" :image-size="60" />
      </div>
    </div>

    <!-- 上传发布 -->
    <div class="page-card upload-card">
      <div class="uc-title">发布新版本</div>
      <el-form label-position="top" class="uc-form">
        <div class="uc-grid">
          <el-form-item label="发布渠道" required>
            <el-radio-group v-model="form.channel">
              <el-radio-button value="pc">PC 客户端</el-radio-button>
              <el-radio-button value="android">安卓客户端</el-radio-button>
              <el-radio-button value="win7">Win7 客户端</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="版本号" required>
            <el-input v-model.trim="form.version" placeholder="如 1.0.2" style="width: 200px" />
          </el-form-item>
        </div>
        <el-form-item label="更新说明">
          <el-input v-model="form.notes" type="textarea" :rows="3" placeholder="面向用户展示的更新内容说明" />
        </el-form-item>
        <el-form-item label="安装包" required>
          <el-upload
            :auto-upload="false"
            :limit="1"
            :on-change="onFileChange"
            :on-remove="() => (file = null)"
            :file-list="file ? [{ name: file.name }] : []"
            drag
            class="uc-upload"
          >
            <el-icon :size="36" class="up-icon"><UploadFilled /></el-icon>
            <div class="el-upload__text">拖拽文件到此处，或<em>点击选择</em></div>
            <template #tip>
              <div class="el-upload__tip">支持 .exe / .zip / .apk，单个不超过 200MB</div>
            </template>
          </el-upload>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="uploading" :disabled="!canSubmit" @click="submit">
            {{ uploading ? '上传发布中…' : '发布版本' }}
          </el-button>
        </el-form-item>
      </el-form>

      <el-progress
        v-if="uploading || progress > 0"
        :percentage="progress"
        :status="progress >= 100 && !uploading ? 'success' : undefined"
        :stroke-width="10"
      />

      <el-alert v-if="result" :type="result.ok ? 'success' : 'error'" :closable="false" class="uc-result">
        <template #title>
          {{ result.ok ? `发布成功：${result.version} 已上线${channelName(result.channel)}通道` : '发布失败' }}
        </template>
        <div v-if="result.ok" class="uc-result-body">
          <div>新包 SHA256：<span class="mono">{{ result.sha256 }}</span></div>
          <div>包地址：<span class="mono">{{ result.url }}</span></div>
        </div>
        <div v-else>{{ result.msg }}</div>
      </el-alert>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Monitor, Iphone, Cpu, Refresh, UploadFilled } from '@element-plus/icons-vue'
import http from '../api'

const CHANNEL_NAMES = { pc: 'PC 客户端', android: '安卓客户端', win7: 'Win7 客户端' }
const channels = reactive({ pc: null, android: null, win7: null })
const form = reactive({ channel: 'pc', version: '', notes: '' })
const file = ref(null)
const uploading = ref(false)
const progress = ref(0)
const result = ref(null)

const ACCEPT = { pc: ['.exe', '.zip'], android: ['.apk', '.zip'], win7: ['.exe', '.zip'] }
const MAX_SIZE = 200 * 1024 * 1024

const canSubmit = computed(
  () => !!(form.channel && form.version && file.value) && !uploading.value
)

function channelName(ch) {
  return CHANNEL_NAMES[ch] || ch
}

function shortSha(s) {
  return s ? String(s).slice(0, 12) : '—'
}

function fmtTime(s) {
  if (!s) return '—'
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}

function onFileChange(upFile) {
  result.value = null
  if (!upFile || !upFile.raw) {
    file.value = null
    return
  }
  const name = upFile.name || ''
  const ext = name.slice(name.lastIndexOf('.')).toLowerCase()
  const allowed = ACCEPT[form.channel] || []
  if (!allowed.includes(ext)) {
    ElMessage.error(`渠道不匹配：${channelName(form.channel)}仅支持 ${allowed.join(' / ')}`)
    file.value = null
    upFile.raw = null
    return
  }
  if (upFile.size > MAX_SIZE) {
    ElMessage.error('文件超过 200MB 上限')
    file.value = null
    upFile.raw = null
    return
  }
  file.value = upFile.raw
}

function loadAll() {
  http.get('/api/update/latest', { params: { ch: 'pc' }, silent: true })
    .then((d) => { channels.pc = d && d.version ? d : null })
    .catch(() => { channels.pc = null })
  http.get('/api/update/latest?ch=android', { silent: true })
    .then((d) => { channels.android = d && d.version ? d : null })
    .catch(() => { channels.android = null })
  http.get('/api/update/latest?ch=win7', { silent: true })
    .then((d) => { channels.win7 = d && d.version ? d : null })
    .catch(() => { channels.win7 = null })
}

function submit() {
  if (!file.value) {
    ElMessage.warning('请先选择安装包')
    return
  }
  if (!/^[\w.\-]+$/.test(form.version)) {
    ElMessage.warning('版本号仅支持字母、数字、点、下划线、连字符')
    return
  }
  uploading.value = true
  progress.value = 0
  result.value = null
  const fd = new FormData()
  fd.append('channel', form.channel)
  fd.append('version', form.version)
  fd.append('notes', form.notes)
  fd.append('file', file.value)
  http
    .post('/api/platform/updates', fd, {
      timeout: 0,
      onUploadProgress: (e) => {
        if (e.total) progress.value = Math.min(99, Math.round((e.loaded / e.total) * 100))
      },
    })
    .then((d) => {
      progress.value = 100
      result.value = {
        ok: true,
        channel: form.channel,
        version: (d && d.version) || form.version,
        sha256: (d && d.sha256) || '',
        url: (d && d.url) || '',
      }
      ElMessage.success('发布成功')
      loadAll()
      // 清空表单避免误重复发布
      file.value = null
      form.version = ''
      form.notes = ''
    })
    .catch((e) => {
      result.value = { ok: false, msg: (e && e.message) || '上传失败' }
    })
    .finally(() => {
      uploading.value = false
    })
}

onMounted(loadAll)
</script>

<style scoped>
.channel-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}

@media (max-width: 1200px) {
  .channel-grid {
    grid-template-columns: 1fr;
  }
}

.channel-card {
  padding: 18px 20px;
}

.ch-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ch-name {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  font-size: 14px;
  font-weight: 600;
  color: #16233c;
}

.ch-icon {
  color: #1667c8;
}

.ch-version {
  margin: 10px 0 6px;
  font-size: 26px;
  font-weight: 700;
  color: #1667c8;
  letter-spacing: 0.5px;
}

.ch-row {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin-top: 8px;
  font-size: 13px;
  line-height: 1.5;
}

.ch-label {
  flex-shrink: 0;
  width: 60px;
  color: #8c99ae;
}

.ch-url {
  word-break: break-all;
}

.ch-notes-text {
  color: #57677f;
}

.upload-card {
  padding: 18px 20px;
}

.uc-title {
  font-size: 14px;
  font-weight: 600;
  color: #16233c;
  margin-bottom: 12px;
}

.uc-grid {
  display: flex;
  gap: 28px;
  flex-wrap: wrap;
}

.uc-upload {
  width: 100%;
}

.up-icon {
  color: #a8b6c9;
  margin-bottom: 8px;
}

.uc-result {
  margin-top: 14px;
}

.uc-result-body {
  font-size: 12.5px;
  line-height: 1.7;
}
</style>
