//go:build windows

// 卡死取证看门狗：500ms 轮询主窗口 IsHungAppWindow；持续挂起超过阈值时，
// 把全部 goroutine 栈 + 最近窗口/托盘事件流水落盘到 %ProgramData%\LiYouP2P\hang-report.txt，
// 用于定位「来回点托盘/最小化偶发无响应」的真实卡死栈。
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"
)

const (
	hangCheckInterval = 500 * time.Millisecond
	hangDumpThreshold = 3 * time.Second  // 连续挂起超过此值才转储（避免动画短暂卡顿误报）
	hangDumpCooldown  = 90 * time.Second // 两次转储最小间隔
	hangTraceCap      = 300              // 事件流水环形容量
	hangStackBufSize  = 16 << 20         // goroutine 栈缓冲 16MB
)

var hangState struct {
	mu        sync.Mutex
	events    []string
	hangStart time.Time
	lastDump  time.Time
}

// winTrace 记录一条窗口/托盘事件流水（卡死转储时还原卡死前的操作序列）。
// 任何线程可调用，仅内存追加，永不阻塞。
func winTrace(format string, v ...any) {
	hangState.mu.Lock()
	defer hangState.mu.Unlock()
	if len(hangState.events) >= hangTraceCap {
		hangState.events = hangState.events[1:]
	}
	hangState.events = append(hangState.events,
		time.Now().Format("15:04:05.000")+" "+fmt.Sprintf(format, v...))
}

// startHangWatchdog 启动挂起检测 goroutine（纯取证，不影响业务流程）。
func startHangWatchdog() {
	go func() {
		h := mainHwndCached()
		if h == 0 {
			return // 30s 内未找到主窗口，放弃监控
		}
		mainHwnd.mu.Lock()
		if mainHwnd.h == 0 {
			mainHwnd.h = h
		}
		mainHwnd.mu.Unlock()
		for {
			time.Sleep(hangCheckInterval)
			hung, _, _ := procIsHungAppWindow.Call(h)
			now := time.Now()
			hangState.mu.Lock()
			if hung != 0 {
				if hangState.hangStart.IsZero() {
					hangState.hangStart = now
					winTrace("watchdog: 检测到主窗口开始挂起")
				}
				if now.Sub(hangState.hangStart) >= hangDumpThreshold && now.Sub(hangState.lastDump) >= hangDumpCooldown {
					hangState.lastDump = now
					hungFor := now.Sub(hangState.hangStart)
					events := append([]string(nil), hangState.events...)
					hangState.mu.Unlock()
					writeHangReport(hungFor, events)
					continue
				}
			} else if !hangState.hangStart.IsZero() {
				hangState.hangStart = time.Time{}
				winTrace("watchdog: 主窗口恢复响应")
			}
			hangState.mu.Unlock()
		}
	}()
}

// writeHangReport 落盘卡死报告。
func writeHangReport(hungFor time.Duration, events []string) {
	dir := os.Getenv("ProgramData")
	if dir == "" {
		return
	}
	path := filepath.Join(dir, "LiYouP2P", "hang-report.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "==== 主窗口挂起 %.1fs @ %s ====\n\n", hungFor.Seconds(), time.Now().Format("2006-01-02 15:04:05.000"))
	buf.WriteString("-- 事件流水（最近 300 条）--\n")
	for _, e := range events {
		buf.WriteString(e)
		buf.WriteByte('\n')
	}
	buf.WriteString("\n-- 全部 goroutine 栈 --\n")
	stack := make([]byte, hangStackBufSize)
	n := goruntime.Stack(stack, true)
	buf.Write(stack[:n])
	_ = os.WriteFile(path, buf.Bytes(), 0644)
}

// mainHwndCached 取主窗口句柄（复用 winhook 的查找逻辑）。
func mainHwndCached() uintptr {
	mainHwnd.mu.Lock()
	h := mainHwnd.h
	mainHwnd.mu.Unlock()
	if h != 0 {
		return h
	}
	return mainWindowHwnd()
}
