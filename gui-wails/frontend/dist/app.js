/* 励联 LiLink GUI 前端逻辑：Vue 3 + Element Plus（本地 vendor，无 npm） */
const { createApp } = Vue;

const app = createApp({
  data() {
    return {
      page: 'login',
      isRelease: false, // 正式包：隐藏服务器地址输入并固定默认地址（BuildMode 注入）
      appVersion: '', // 当前版本号（GetVersion 注入，顶栏展示）
      loading: false,
      switching: false,
      running: false,
      form: {
        platform: 'http://127.0.0.1:8900',
        tenant: 'liyou-demo',
        user: '',
        pass: '',
        devName: '',
        role: 'client',
      },
      session: { user: {}, device_role: 'client' },
      rules: [],
      logs: [],
      declaredPorts: [],
      serverHosts: ['127.0.0.1'], // 服务端允许访问的内网主机（首项为默认本机，不可删）
      newHost: '',
      hostBusy: false,
      removingHost: '',
      _pollTimer: null,
    };
  },

  async mounted() {
      // 构建模式：正式包隐藏服务器地址输入，登录固定默认地址（后端 Login/StartAgent 双重兜底）
      try {
        this.isRelease = (await window.go.main.App.BuildMode()) === 'release';
        if (this.isRelease) this.form.platform = 'http://jyly8888.gnway.vip:58894';
      } catch (e) { /* 后端未提供时按 dev 处理 */ }
      // 实时日志（后端 EventsE("log", entry)）
      if (window.runtime && window.runtime.EventsOn) {
        window.runtime.EventsOn('log', (entry) => this.onLog(entry));
        // 托盘「启动/停止连接」后推送状态，主窗口按钮与映射行同步
        window.runtime.EventsOn('agent-state', (running) => this.onAgentState(!!running));
        // 托盘「检查更新」发现新版：主窗口弹出确认
        window.runtime.EventsOn('update-available', (info) => this.promptUpdate(info));
      }
      // 当前版本号
      try { this.appVersion = await window.go.main.App.GetVersion(); } catch (e) { /* 忽略 */ }
      // 恢复已保存的登录态
      try {
        const s = await window.go.main.App.RestoreSession();
        if (s && s.device_token !== undefined) {
          this.session = s;
          this.rules = (s.mappings && s.mappings.length)
            ? s.mappings.map((m) => this.toRule(m))
            : [this.draftRule()];
          if (s.device_role === 'server') {
            this.serverHosts = ['127.0.0.1', ...(s.hosts || [])];
          }
          this.page = 'work';
        }
      } catch (e) { /* 无历史会话 */ }
      this.startPoll();
    },

  beforeUnmount() { this.stopPoll(); },

  computed: {
    // 渲染层封顶：DOM 最多保留最近 150 行（数据缓冲仍 300），抑制高频日志下的节点规模
    shownLogs() {
      return this.logs.length > 150 ? this.logs.slice(-150) : this.logs;
    },
  },

  methods: {
    // 统一构造映射行：origin 记录已生效的旧值（含目标主机），用于编辑后再生效时替换
    toRule(m) {
      const host = m.host || '';
      return { remote: m.remote, local: String(m.local), host, applied: false, busy: false, origin: { remote: m.remote, local: String(m.local), host } };
    },
    draftRule() {
      return { remote: 8080, local: '18081', host: '', applied: false, busy: false, origin: null };
    },

    startPoll() {
      this._pollTimer = setInterval(async () => {
        if (this.page !== 'work') return;
        try { this.running = await window.go.main.App.IsRunning(); } catch (e) { /* 忽略 */ }
      }, 1500);
    },
    stopPoll() { if (this._pollTimer) clearInterval(this._pollTimer); },

    onLog(payload) {
      // 后端按批推送（数组）；单条事件向下兼容。
      // 每条分配稳定递增 _seq 作为渲染 key：窗口只增量追加新行，
      // 避免 index key 在滑动窗口下整列补丁导致的渲染风暴卡顿。
      if (this._logSeq === undefined) this._logSeq = 0;
      const batch = Array.isArray(payload) ? payload : [payload];
      for (const e of batch) {
        e._seq = ++this._logSeq;
        if (this.session.device_role === 'server') this.parseDeclared(e.text);
      }
      this.logs.push(...batch);
      if (this.logs.length > 300) this.logs = this.logs.slice(-300);
      this.scheduleLogScroll();
    },

    // 日志滚动合并到动画帧：同一帧多批只布局一次；用户上翻阅读时不再强制拉底
    scheduleLogScroll() {
      if (this._scrollPending) return;
      this._scrollPending = true;
      requestAnimationFrame(() => {
        this._scrollPending = false;
        const box = this.$refs.logBox;
        if (!box) return;
        const nearBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 80;
        if (nearBottom) box.scrollTop = box.scrollHeight;
      });
    },

    // 托盘启停连接后与主窗口状态同步：按钮文案 + 映射行生效标记
    onAgentState(running) {
      this.running = running;
      if (running) {
        // 启动时全部有效映射已随 --map 生效，行状态与 origin 同步为当前值
        this.rules.forEach((r) => {
          if (Number(r.remote) > 0 && String(r.local || '').trim() !== '') {
            r.applied = true;
            r.origin = { remote: Number(r.remote), local: String(r.local).trim(), host: String(r.host || '').trim() };
          }
        });
      } else {
        this.rules.forEach((r) => { r.applied = false; });
      }
    },

    // 从日志中解析服务端申报的可穿透端口（如 "…申报可穿透端口: 8080(用友 T+（ERP）)"）
    parseDeclared(text) {
      if (!/可穿透端口/.test(text)) return;
      const re = /(\d+)\(((?:[^()]|\([^()]*\))*)\)/g;
      const found = [];
      let m;
      while ((m = re.exec(text)) !== null) {
        found.push({ port: m[1], label: m[2] });
      }
      if (!found.length) return;
      const map = new Map(this.declaredPorts.map((p) => [p.port, p]));
      found.forEach((p) => map.set(p.port, p));
      this.declaredPorts = Array.from(map.values());
    },

    async doLogin() {
      if (this.loading) return;
      this.loading = true;
      try {
        const mappings = this.form.role === 'client'
          ? [{ remote: 8080, local: '18081' }]
          : [];
        const r = await window.go.main.App.Login(
          this.form.platform, this.form.tenant, this.form.user, this.form.pass,
          this.form.devName, this.form.role, mappings,
        );
        this.session = r;
        this.rules = (r.mappings && r.mappings.length)
          ? r.mappings.map((m) => this.toRule(m))
          : [this.draftRule()];
        if (r.device_role === 'server') {
          this.serverHosts = ['127.0.0.1', ...(r.hosts || [])];
        }
        this.logs = [];
        this.declaredPorts = [];
        this.newHost = '';
        this.page = 'work';
        ElementPlus.ElMessage.success('登录成功，设备已注册');
      } catch (e) {
        ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
      } finally {
        this.loading = false;
      }
    },

    async toggleAgent() {
      if (this.switching) return;
      this.switching = true;
      try {
        if (this.running) {
          await window.go.main.App.StopAgent();
          this.running = false;
          this.rules.forEach((r) => { r.applied = false; });
        } else {
          const rules = this.rules
            .filter((r) => Number(r.remote) > 0 && String(r.local || '').trim() !== '')
            .map((r) => ({ remote: Number(r.remote), local: String(r.local).trim(), host: String(r.host || '').trim() }));
          if (this.session.device_role === 'client' && !rules.length) {
            ElementPlus.ElMessage.warning('请先添加至少一条端口映射');
            return;
          }
          // 服务端：允许访问的内网主机（默认本机恒可用，仅传用户添加的）
          const hosts = this.session.device_role === 'server'
            ? this.serverHosts.filter((h) => h && h !== '127.0.0.1')
            : [];
          await window.go.main.App.StartAgent(
            this.session.device_role, rules, hosts, this.session.device_token, this.session.platform_ws,
          );
          this.running = true;
          // 启动时全部有效映射已随 --map 生效，行状态与 origin 同步为当前值
          this.rules.forEach((r) => {
            if (Number(r.remote) > 0 && String(r.local || '').trim() !== '') {
              r.applied = true;
              r.origin = { remote: Number(r.remote), local: String(r.local).trim(), host: String(r.host || '').trim() };
            }
          });
        }
      } catch (e) {
        ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
      } finally {
        this.switching = false;
      }
    },

    async doLogout() {
      try { await window.go.main.App.Logout(); } catch (e) { /* 忽略 */ }
      this.session = { user: {}, device_role: 'client' };
      this.rules = [];
      this.logs = [];
      this.declaredPorts = [];
      this.serverHosts = ['127.0.0.1'];
      this.newHost = '';
      this.running = false;
      this.page = 'login';
    },

    // ---------- 检查更新 ----------
    async checkUpdate() {
      try {
        const info = await window.go.main.App.CheckUpdate();
        if (!info.has_update) {
          ElementPlus.ElMessage.success('当前已是最新版本 v' + (info.current_version || this.appVersion));
          return;
        }
        this.promptUpdate(info);
      } catch (e) {
        ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
      }
    },

    // 新版确认弹窗（工作页按钮与托盘检查更新共用）
    promptUpdate(info) {
      const notes = info.notes ? '，更新说明：' + info.notes : '';
      ElementPlus.ElMessageBox.confirm(
        '发现新版本 v' + info.version + '（当前 v' + info.current_version + notes + '）。升级期间连接会短暂中断，是否立即升级？',
        '检查更新',
        { confirmButtonText: '立即升级', cancelButtonText: '稍后', type: 'info' },
      ).then(async () => {
        ElementPlus.ElMessage.info('开始下载升级包，进度见下方日志…');
        try { await window.go.main.App.ApplyUpdate(); } catch (e) {
          ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
        }
      }).catch(() => {});
    },

    addRule() { this.rules.push({ remote: '', local: '', host: '', applied: false, busy: false, origin: null }); },

    // 新行生效：运行中走管理通道热生效（隧道不断开），未运行仅保存配置；
    // 编辑过的旧规则行先从配置移除旧值再保存新值（即时写盘）
    async applyRule(i) {
      const r = this.rules[i];
      const remote = Number(r.remote), local = String(r.local || '').trim();
      const host = String(r.host || '').trim();
      if (!(remote > 0) || !local) {
        ElementPlus.ElMessage.warning('请先填写远端端口和本地端口');
        return;
      }
      r.busy = true;
      try {
        if (r.origin && (Number(r.origin.remote) !== remote || r.origin.local !== local || (r.origin.host || '') !== host)) {
          try { await window.go.main.App.RemoveMapping(r.origin.host || '', r.origin.remote, r.origin.local); } catch (e) { /* 旧值不存在则忽略 */ }
        }
        const res = await window.go.main.App.AddMapping(host, remote, local);
        r.origin = { remote, local, host };
        r.applied = res.applied;
        ElementPlus.ElMessage.success(res.applied ? '映射已生效，连接保持不断开' : '已保存，启动连接后生效');
      } catch (e) {
        ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
      } finally {
        r.busy = false;
      }
    },

    // 删除行：运行中热删除监听（隧道不断开），同时清理配置；
    // 已编辑未生效的行还需清理旧值，避免配置残留
    async removeRule(i) {
      const r = this.rules[i];
      const remote = Number(r.remote), local = String(r.local || '').trim();
      if (!(remote > 0) || !local) {
        this.rules.splice(i, 1); // 空草稿行直接移除
        return;
      }
      r.busy = true;
      try {
        if (r.origin && (Number(r.origin.remote) !== remote || r.origin.local !== local || (r.origin.host || '') !== String(r.host || '').trim())) {
          try { await window.go.main.App.RemoveMapping(r.origin.host || '', r.origin.remote, r.origin.local); } catch (e) { /* 旧值不存在则忽略 */ }
        }
        await window.go.main.App.RemoveMapping(String(r.host || '').trim(), remote, local);
        this.rules.splice(i, 1);
      } catch (e) {
        ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
      } finally {
        r.busy = false;
      }
    },

    // ---------- 服务端：允许访问的内网主机 ----------
    async addHost() {
      if (this.hostBusy) return;
      const h = String(this.newHost || '').trim();
      if (!h) {
        ElementPlus.ElMessage.warning('请输入要允许访问的内网主机 IP 或名称');
        return;
      }
      if (this.serverHosts.some((x) => x.toLowerCase() === h.toLowerCase())) {
        ElementPlus.ElMessage.warning('该主机已在列表中');
        return;
      }
      this.hostBusy = true;
      try {
        const res = await window.go.main.App.AddHost(h);
        if (!this.serverHosts.some((x) => x.toLowerCase() === res.host.toLowerCase())) {
          this.serverHosts.push(res.host);
        }
        this.newHost = '';
        ElementPlus.ElMessage.success(res.applied ? '已添加并即时生效，连接保持不断开' : '已保存，启动连接后生效');
      } catch (e) {
        ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
      } finally {
        this.hostBusy = false;
      }
    },

    async removeHost(h) {
      if (h === '127.0.0.1' || this.removingHost) return;
      this.removingHost = h;
      try {
        const res = await window.go.main.App.RemoveHost(h);
        this.serverHosts = this.serverHosts.filter((x) => x !== h);
        ElementPlus.ElMessage.success(res.applied ? '已移除，连接保持不断开' : '已移除');
      } catch (e) {
        ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
      } finally {
        this.removingHost = '';
      }
    },

    async openPage() {
      const r = this.rules.find((x) => Number(x.remote) > 0 && String(x.local).trim() !== '');
      if (!r) { ElementPlus.ElMessage.warning('暂无可打开的映射'); return; }
      const local = String(r.local).trim();
      const port = local.includes(':') ? Number(local.split(':').pop()) : Number(local);
      try {
        await window.go.main.App.OpenPage(port);
      } catch (e) {
        ElementPlus.ElMessage.error(String(e).replace(/^\s*Error:\s*/, ''));
      }
    },

    // ---------- 自定义标题栏窗口控制 ----------
    winMin() { window.runtime && window.runtime.WindowMinimise(); },
    winClose() {
      // 走 Wails 关闭链路 → OnBeforeClose → 隐藏到托盘（Agent 后台保持运行）；
      // 真正退出经托盘菜单「退出」，会先停 Agent 再结束进程
      const rt = window.runtime;
      if (rt.WindowClose) { rt.WindowClose(); } else { rt.Quit(); }
    },
  },
});

app.use(ElementPlus);
app.mount('#app');
