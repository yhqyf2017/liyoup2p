package main

// 主窗口（walk declarative 两页式）：登录页 ↔ 工作页，Composite 显隐切换（walk 无内置页切换）。
// 线程模型：后台 goroutine 的 UI 更新一律经 mw.Synchronize 投递主线程（替代 wails EventsEmit）。
// 关窗行为对齐主 GUI：点关闭/Alt+F4 → 隐藏到托盘；托盘「退出」→ 停 Agent + 保存会话 + 真退出。

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	. "github.com/lxn/walk/declarative"
)

// uiState 主窗口及其控件引用（Create 后由 declarative AssignTo 填充）。
type uiState struct {
	app *App
	mw  *walk.MainWindow
	ni  *walk.NotifyIcon // 托盘（tray.go 填充）

	// 登录页
	loginPage  *walk.Composite
	rbClient   *walk.RadioButton
	rbServer   *walk.RadioButton
	edTenant   *walk.LineEdit
	edUser     *walk.LineEdit
	edPass     *walk.LineEdit
	edDev      *walk.LineEdit
	edPlatform *walk.LineEdit // 仅开发模式创建
	btnLogin   *walk.PushButton

	// 工作页
	workPage    *walk.Composite
	banner      *walk.Composite // 品牌横幅（背景色在窗口创建后设置）
	bannerBrush *walk.SolidColorBrush // 横幅背景画刷（须持有引用防 GC 回收）
	lblDot      *walk.Label // 状态红绿灯（品牌横幅内）
	lblStatus   *walk.Label // 横幅内白色状态文字
	btnLogout   *walk.PushButton
	btnUpdate   *walk.PushButton
	grpRules    *walk.GroupBox // 客户端：映射区
	grpHosts    *walk.GroupBox // 服务端：白名单区
	table       *walk.TableView
	lbHosts     *walk.ListBox
	edRuleHost  *walk.LineEdit
	edRuleRemote *walk.LineEdit
	edRuleLocal *walk.LineEdit
	edHost      *walk.LineEdit
	btnToggle   *walk.PushButton
	chkAuto     *walk.CheckBox
	tvLog       *walk.TextEdit

	mm          *mapModel
	hm          *hostModel
	logLines    []string
	balloonOnce sync.Once
	trayToggle  *walk.Action // 托盘启停菜单项（随状态换文案）
}

// mapModel TableView 数据源：客户端端口映射列表。
type mapModel struct {
	walk.TableModelBase
	items []MapRule
}

func (m *mapModel) RowCount() int { return len(m.items) }

func (m *mapModel) Value(row, col int) any {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	r := m.items[row]
	switch col {
	case 0:
		if r.Host == "" {
			return "（服务器本机）"
		}
		return r.Host
	case 1:
		return r.Remote
	case 2:
		return r.Local
	}
	return ""
}

// hostModel ListBox 数据源：服务端白名单（walk.ListModel 带事件发布器）。
type hostModel struct {
	walk.ListModelBase
	items []string
}

func (m *hostModel) ItemCount() int { return len(m.items) }

func (m *hostModel) Value(i int) any {
	if i < 0 || i >= len(m.items) {
		return ""
	}
	return m.items[i]
}

// runUI 构建主窗口、注册 App 回调、跑消息循环（进程主 goroutine 调用，直到退出才返回）。
func runUI(a *App) error {
	ui := &uiState{app: a, mm: &mapModel{}, hm: &hostModel{}}
	devMode := !releaseMode()

	// 登录记忆预填
	saved := a.GetSavedLogin()

	appIcon, err := appIcon()
	if err != nil {
		return fmt.Errorf("加载图标失败：%w", err)
	}

	// 登录页表单（Grid 两列：标签 | 输入框，先组装，再按开发模式追加平台地址行）
	formChildren := []Widget{
		Label{Text: "角色："},
		Composite{
			Layout: HBox{Margins: Margins{}, Spacing: 16},
			Children: []Widget{
				RadioButton{AssignTo: &ui.rbClient, Text: "客户端（映射服务器端口到本机）"},
				RadioButton{AssignTo: &ui.rbServer, Text: "服务端（供他人访问内网）"},
			},
		},
		Label{Text: "租户："},
		LineEdit{AssignTo: &ui.edTenant, CueBanner: "租户标识（可留空）", Text: saved.Tenant},
		Label{Text: "账号："},
		LineEdit{AssignTo: &ui.edUser, CueBanner: "用户名", Text: saved.User},
		Label{Text: "密码："},
		LineEdit{AssignTo: &ui.edPass, CueBanner: "密码", Text: saved.Pass},
		Label{Text: "设备名："},
		LineEdit{AssignTo: &ui.edDev, CueBanner: "留空使用本机名", Text: saved.DevName},
	}
	if devMode {
		formChildren = append(formChildren,
			Label{Text: "平台地址："},
			LineEdit{AssignTo: &ui.edPlatform, CueBanner: "http://…（开发调试用）", Text: saved.Platform},
		)
	}
	// 登录按钮独立成行并水平居中（HSpacer 两端夹持）
	loginBtnRow := Composite{
		ColumnSpan: 2,
		Layout:     HBox{Margins: Margins{}, Spacing: 0},
		Children: []Widget{
			HSpacer{},
			PushButton{AssignTo: &ui.btnLogin, Text: "登  录", MinSize: Size{Width: 160, Height: 32}, OnClicked: func() { ui.onLogin() }},
			HSpacer{},
		},
	}
	// 检查更新入口独立成行并水平居中（登录页也能手动触发更新检查）
	updateBtnRow := Composite{
		ColumnSpan: 2,
		Layout:     HBox{Margins: Margins{}, Spacing: 0},
		Children: []Widget{
			HSpacer{},
			PushButton{Text: "检查更新", MinSize: Size{Width: 160, Height: 28},
				OnClicked: func() { go ui.CheckUpdateManual() }},
			HSpacer{},
		},
	}
	formChildren = append(formChildren, Label{Text: ""}, loginBtnRow, updateBtnRow)

	// 按屏幕可用区域（不含任务栏）夹紧窗口尺寸：
	// 低分辨率（1024×768）或 125%/150% 缩放下，固定 680 高度会超出屏幕导致底部截断
	// （SPI_GETWORKAREA = 0x0048，旧版 lxn/win 未定义该常量）
	sizeW, sizeH, minH := 560, 680, 600
	var wa win.RECT
	if win.SystemParametersInfo(0x0048, 0, unsafe.Pointer(&wa), 0) {
		availW := int(wa.Right-wa.Left) - 24
		availH := int(wa.Bottom-wa.Top) - 16
		if availW > 0 && sizeW > availW {
			sizeW = availW
		}
		if availH > 0 {
			if sizeH > availH {
				sizeH = availH
			}
			if minH > sizeH {
				minH = sizeH
			}
		}
	}

	mainWindow := MainWindow{
		AssignTo: &ui.mw,
		Title:    siWndTitle,
		Icon:     appIcon,
		Font:     Font{Family: "微软雅黑", PointSize: 10},
		MinSize:  Size{Width: 480, Height: minH},
		Size:     Size{Width: sizeW, Height: sizeH},
		Visible:  !bootAutostart, // 开机自启拉起：窗口不显示，仅托盘
		Layout:   VBox{Margins: Margins{Left: 14, Top: 12, Right: 14, Bottom: 12}, Spacing: 10},
		Children: []Widget{
			// ---------- 登录页：品牌区 + 表单，整体垂直居中 ----------
			Composite{
				AssignTo:      &ui.loginPage,
				StretchFactor: 1,
				Layout:        VBox{Margins: Margins{Left: 24, Top: 8, Right: 24, Bottom: 8}, Spacing: 14},
				Children: []Widget{
					VSpacer{StretchFactor: 2},
					// 品牌区：图标 + 产品名 + 副标题（HSpacer 夹持水平居中）
					Composite{
						Layout: HBox{Margins: Margins{}},
						Children: []Widget{
							HSpacer{},
							ImageView{Image: appIcon, MinSize: Size{Width: 64, Height: 64}},
							HSpacer{},
						},
					},
					Composite{
						Layout: HBox{Margins: Margins{}},
						Children: []Widget{
							HSpacer{},
							Label{Text: "励联 LiLink", Font: Font{PointSize: 16, Bold: true}},
							HSpacer{},
						},
					},
					Composite{
						Layout: HBox{Margins: Margins{}},
						Children: []Widget{
							HSpacer{},
							Label{Text: "内网互联 · 端口映射", TextColor: walk.RGB(110, 110, 110)},
							HSpacer{},
						},
					},
					Composite{
						Layout: Grid{Columns: 2, Spacing: 12, Margins: Margins{}},
						Children: append(append([]Widget{}, formChildren[:len(formChildren)-3]...),
							Label{Text: ""}, loginBtnRow, updateBtnRow),
					},
					VSpacer{StretchFactor: 3},
				},
			},
			// ---------- 工作页：品牌横幅 + 分组面板；弹性空间只给映射区(5)和日志区(3) ----------
			Composite{
				AssignTo:      &ui.workPage,
				Visible:       false,
				StretchFactor: 1,
				Layout:        VBox{Margins: Margins{}, Spacing: 8},
				Children: []Widget{
				// 品牌横幅：深蓝底、白字状态 + 红绿灯、右侧注销（背景色在窗口创建后设置）
				Composite{
					AssignTo: &ui.banner,
					MinSize:  Size{Height: 46},
					Layout:   HBox{Margins: Margins{Left: 12, Top: 8, Right: 8, Bottom: 8}, Spacing: 8},
						Children: []Widget{
							Label{AssignTo: &ui.lblDot, Text: "●", TextColor: walk.RGB(235, 90, 90)},
							Label{AssignTo: &ui.lblStatus, Text: "状态：未连接",
								TextColor: walk.RGB(255, 255, 255), StretchFactor: 1},
							PushButton{AssignTo: &ui.btnUpdate, Text: "检查更新", MinSize: Size{Width: 80},
								OnClicked: func() { go ui.CheckUpdateManual() }},
							PushButton{AssignTo: &ui.btnLogout, Text: "注销", MinSize: Size{Width: 80},
								OnClicked: func() { ui.onLogout() }},
						},
					},
					// 客户端：端口映射
					GroupBox{
						AssignTo:      &ui.grpRules,
						Title:         "端口映射",
						StretchFactor: 5,
						Layout:        VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 10}, Spacing: 6},
						Children: []Widget{
							// 末列吃掉表格剩余宽度；表格纵向吃掉映射区多余高度
							TableView{
								AssignTo:            &ui.table,
								StretchFactor:       1,
								LastColumnStretched: true,
								Columns: []TableViewColumn{
									{Title: "目标主机", Width: 160},
									{Title: "远端端口", Width: 90},
									{Title: "本地监听"},
								},
								Model: ui.mm,
							},
							// 标签与输入框成对排列（Grid 6 列恰好一行），杜绝错行
							Composite{
								Layout: Grid{Columns: 6, Spacing: 6, Margins: Margins{}},
								Children: []Widget{
									Label{Text: "目标主机"},
									LineEdit{AssignTo: &ui.edRuleHost, CueBanner: "留空=服务器本机"},
									Label{Text: "远端端口"},
									LineEdit{AssignTo: &ui.edRuleRemote, CueBanner: "如 9123"},
									Label{Text: "本地端口"},
									LineEdit{AssignTo: &ui.edRuleLocal, CueBanner: "如 18080"},
								},
							},
							Composite{
								Layout: HBox{Margins: Margins{}, Spacing: 6},
								Children: []Widget{
									PushButton{Text: "添加", MinSize: Size{Width: 88}, OnClicked: func() { ui.onAddRule() }},
									PushButton{Text: "删除选中", MinSize: Size{Width: 88}, OnClicked: func() { ui.onDelRule() }},
									HSpacer{},
								},
							},
						},
					},
					// 服务端：白名单
					GroupBox{
						AssignTo:      &ui.grpHosts,
						Title:         "访问白名单",
						Visible:       false,
						StretchFactor: 5,
						Layout:        VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 10}, Spacing: 6},
						Children: []Widget{
							ListBox{AssignTo: &ui.lbHosts, Model: ui.hm},
							Composite{
								Layout: Grid{Columns: 3, Spacing: 6, Margins: Margins{}},
								Children: []Widget{
									Label{Text: "允许访问的内网主机（本机 127.0.0.1 默认已允许）", ColumnSpan: 3},
									LineEdit{AssignTo: &ui.edHost, CueBanner: "如 192.168.5.20 或 NAS 主机名"},
									PushButton{Text: "添加", MinSize: Size{Width: 88}, OnClicked: func() { ui.onAddHost() }},
									PushButton{Text: "删除选中", MinSize: Size{Width: 88}, OnClicked: func() { ui.onDelHost() }},
								},
							},
						},
					},
					PushButton{
						AssignTo:  &ui.btnToggle,
						Text:      "启动连接",
						MinSize:   Size{Height: 40},
						Font:      Font{PointSize: 12},
						OnClicked: func() { go ui.app.toggleConnection() },
					},
					// 自启开关靠左（右侧 HSpacer 防止被居中/拉伸）
					Composite{
						Layout: HBox{Margins: Margins{}},
						Children: []Widget{
							CheckBox{
								AssignTo: &ui.chkAuto,
								Text:     "开机自动启动并连接",
								OnClicked: func() {
									on := ui.chkAuto.Checked()
									go func() {
										if err := ui.app.SetAutoStart(on); err != nil {
											ui.app.logf("warn", "开机自启设置失败：%v", err)
										}
									}()
								},
							},
							HSpacer{},
						},
					},
					GroupBox{
						Title:         "运行日志",
						StretchFactor: 3,
						Layout:        VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 10}},
						Children: []Widget{
							TextEdit{
								AssignTo: &ui.tvLog,
								ReadOnly: true,
								VScroll:  true,
							},
						},
					},
				},
			},
		},
	}

	if err := mainWindow.Create(); err != nil {
		return fmt.Errorf("创建主窗口失败：%w", err)
	}
	// 品牌横幅深蓝底（declarative 无 BackgroundColor 字段，创建后设置）
	ui.bannerBrush, _ = walk.NewSolidColorBrush(walk.RGB(31, 78, 121))
	ui.banner.SetBackground(ui.bannerBrush)
	if saved.Role == "server" {
		ui.rbServer.SetChecked(true)
	} else {
		ui.rbClient.SetChecked(true)
	}
	ui.edPass.SetPasswordMode(true)

	// App 回调注册（任意线程可调用，内部投递主线程）
	a.showUI = func() { ui.mw.Synchronize(func() { ui.bringToFront() }) }
	a.hideUI = func() {
		ui.mw.Synchronize(func() {
			ui.mw.Hide()
			ui.balloonOnce.Do(func() {
				if ui.ni != nil {
					ui.ni.ShowInfo("励联 LiLink", "已最小化到托盘，后台保持连接")
				}
			})
		})
	}
	a.onLogs = func(batch []LogEntry) { ui.mw.Synchronize(func() { ui.appendLogs(batch) }) }
	a.onState = func(running bool) {
		ui.mw.Synchronize(func() {
			ui.syncState(running)
			if ui.trayToggle != nil {
				if running {
					ui.trayToggle.SetText("停止连接")
				} else {
					ui.trayToggle.SetText("启动连接")
				}
			}
		})
	}

	// 关闭按钮/Alt+F4 → 隐藏到托盘（托盘「退出」走 exitApp，isExiting 后放行真关闭）
	ui.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !a.isExiting() {
			*canceled = true
			a.hideToTray()
		}
	})

	if err := setupTray(ui); err != nil {
		return fmt.Errorf("托盘初始化失败：%w", err)
	}

	// 自动更新（win7 通道）：注册更新安装后的退出回调并启动后台检查循环
	a.onExitRequest = func() { ui.mw.Synchronize(ui.exitApp) }
	go a.updateLoop()

	// 历史日志回填（环形缓冲里已有启动前的日志）
	if logs := a.GetLogs(); len(logs) > 0 {
		ui.appendLogs(logs)
	}

	// 已有登录态：直接进工作页（开机自启拉起时窗口隐藏，同样进工作页待命）
	if s, _ := a.RestoreSession(); s != nil {
		ui.enterWorkPage()
	}
	// 开机自启拉起：窗口保持隐藏并自动恢复连接
	if bootAutostart {
		go a.autostartBoot()
	}

	ui.mw.Run()
	if ui.ni != nil {
		ui.ni.Dispose()
	}
	return nil
}

// ---------- 页面切换与状态 ----------

// enterWorkPage 登录成功/已有会话：切到工作页并刷新全部状态。
func (ui *uiState) enterWorkPage() {
	s := ui.app.currentSession()
	ui.loginPage.SetVisible(false)
	ui.workPage.SetVisible(true)
	client := s.DeviceRole != "server"
	ui.grpRules.SetVisible(client)
	ui.grpHosts.SetVisible(!client)
	ui.chkAuto.SetChecked(ui.app.GetAutoStart())
	ui.refreshRules()
	ui.refreshHosts()
	ui.syncState(ui.app.IsRunning())
}

// syncState 连接状态 → 状态行 + 启停按钮文案。
func (ui *uiState) syncState(running bool) {
	s := ui.app.currentSession()
	role := "客户端"
	if s.DeviceRole == "server" {
		role = "服务端"
	}
	state := "未连接"
	if running {
		state = "连接中"
	}
	if running {
		ui.lblDot.SetTextColor(walk.RGB(120, 220, 130)) // 绿灯
	} else {
		ui.lblDot.SetTextColor(walk.RGB(235, 90, 90)) // 红灯
	}
	ui.lblStatus.SetText(fmt.Sprintf("状态：%s　账号：%s　设备：%s（%s）", state, s.User.Username, s.DeviceName, role))
	if running {
		ui.btnToggle.SetText("停止连接")
	} else {
		ui.btnToggle.SetText("启动连接")
	}
}

// refreshRules 从会话刷新映射表。
func (ui *uiState) refreshRules() {
	ui.mm.items = append([]MapRule(nil), ui.app.currentSession().Mappings...)
	ui.mm.PublishRowsReset()
	ui.table.SetCurrentIndex(-1)
}

// refreshHosts 从会话刷新白名单列表。
func (ui *uiState) refreshHosts() {
	ui.hm.items = append([]string(nil), ui.app.currentSession().Hosts...)
	ui.hm.PublishItemsReset()
	ui.lbHosts.SetCurrentIndex(-1)
}

// ---------- 动作 ----------

// onLogout 注销：停 Agent、清登录态（登录记忆保留），回登录页。
func (ui *uiState) onLogout() {
	a := ui.app
	go func() {
		a.Logout()
		ui.mw.Synchronize(func() {
			ui.syncState(false)
			ui.enterLoginPage()
		})
	}()
}

// enterLoginPage 回登录页（凭据已由登录记忆预填机制持久化）。
func (ui *uiState) enterLoginPage() {
	ui.workPage.SetVisible(false)
	ui.loginPage.SetVisible(true)
}

// onLogin 登录按钮：后台调用 Login（网络 IO），完成后切工作页或弹错。
func (ui *uiState) onLogin() {
	role := "client"
	if ui.rbServer.Checked() {
		role = "server"
	}
	platform := ""
	if ui.edPlatform != nil {
		platform = ui.edPlatform.Text()
	}
	tenant, user, pass, dev := ui.edTenant.Text(), ui.edUser.Text(), ui.edPass.Text(), ui.edDev.Text()
	a := ui.app
	ui.btnLogin.SetEnabled(false)
	go func() {
		_, err := a.Login(platform, tenant, user, pass, dev, role, a.GetSavedRules().Rules)
		ui.mw.Synchronize(func() {
			ui.btnLogin.SetEnabled(true)
			if err != nil {
				a.logf("error", "登录失败：%v", err)
				walk.MsgBox(ui.mw, "登录失败", err.Error(), walk.MsgBoxIconError)
				return
			}
			ui.enterWorkPage()
		})
	}()
}

// onAddRule 添加映射（后台执行：Agent 运行中会走管理通道热生效）。
func (ui *uiState) onAddRule() {
	host := strings.TrimSpace(ui.edRuleHost.Text())
	local := strings.TrimSpace(ui.edRuleLocal.Text())
	remoteStr := strings.TrimSpace(ui.edRuleRemote.Text())
	remote, err := strconv.Atoi(remoteStr)
	if err != nil {
		ui.app.logf("error", "远端端口须为数字（1-65535）")
		return
	}
	a := ui.app
	go func() {
		if _, err := a.AddMapping(host, remote, local); err != nil {
			a.logf("error", "添加映射失败：%v", err)
			return
		}
		ui.mw.Synchronize(func() {
			ui.edRuleHost.SetText("")
			ui.edRuleLocal.SetText("")
			ui.refreshRules()
		})
	}()
}

// onDelRule 删除选中映射。
func (ui *uiState) onDelRule() {
	idx := ui.table.CurrentIndex()
	if idx < 0 || idx >= len(ui.mm.items) {
		ui.app.logf("warn", "请先在列表中选中要删除的映射")
		return
	}
	r := ui.mm.items[idx]
	a := ui.app
	go func() {
		if _, err := a.RemoveMapping(r.Host, r.Remote, r.Local); err != nil {
			a.logf("error", "删除映射失败：%v", err)
			return
		}
		ui.mw.Synchronize(func() { ui.refreshRules() })
	}()
}

// onAddHost 添加白名单主机。
func (ui *uiState) onAddHost() {
	host := ui.edHost.Text()
	a := ui.app
	go func() {
		if _, err := a.AddHost(host); err != nil {
			a.logf("error", "添加内网主机失败：%v", err)
			return
		}
		ui.mw.Synchronize(func() {
			ui.edHost.SetText("")
			ui.refreshHosts()
		})
	}()
}

// onDelHost 删除选中白名单主机。
func (ui *uiState) onDelHost() {
	idx := ui.lbHosts.CurrentIndex()
	if idx < 0 {
		ui.app.logf("warn", "请先在列表中选中要删除的主机")
		return
	}
	hm := ui.hm
	if idx >= len(hm.items) {
		return
	}
	host := hm.items[idx]
	a := ui.app
	go func() {
		if _, err := a.RemoveHost(host); err != nil {
			a.logf("error", "删除内网主机失败：%v", err)
			return
		}
		ui.mw.Synchronize(func() { ui.refreshHosts() })
	}()
}

// appendLogs 追加日志批次到只读日志面板并滚动到底部。
func (ui *uiState) appendLogs(batch []LogEntry) {
	for _, e := range batch {
		tag := "信息"
		switch e.Level {
		case "warn":
			tag = "警告"
		case "error":
			tag = "错误"
		}
		ui.logLines = append(ui.logLines, fmt.Sprintf("%s [%s] %s", e.Time, tag, e.Text))
	}
	if len(ui.logLines) > logBufSize {
		ui.logLines = append([]string(nil), ui.logLines[len(ui.logLines)-logBufSize:]...)
	}
	text := strings.Join(ui.logLines, "\r\n")
	ui.tvLog.SetText(text)
	// 滚动到底部：光标移到文本末尾
	n := len(utf16.Encode([]rune(text)))
	ui.tvLog.SetTextSelection(n, n)
	ui.tvLog.ScrollToCaret()
}

// bringToFront 托盘唤回：还原并前置主窗口。
func (ui *uiState) bringToFront() {
	win.ShowWindow(ui.mw.Handle(), win.SW_RESTORE)
	ui.mw.Show()
	win.SetForegroundWindow(ui.mw.Handle())
}

// exitApp 托盘「退出」/真关闭：停 Agent、保存会话、结束消息循环（须在 UI 线程调用）。
func (ui *uiState) exitApp() {
	a := ui.app
	a.mu.Lock()
	a.exiting = true
	s := a.session
	a.mu.Unlock()
	a.StopAgent()
	if s != nil {
		if err := a.saveSession(s); err != nil {
			a.logf("warn", "退出前保存配置失败：%v", err)
		}
	}
	walk.App().Exit(0)
}
