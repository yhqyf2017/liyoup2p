// Package logx Agent 中文日志：[HH:mm:ss] [Agent·服务端/客户端] 内容。
package logx

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var label = "Agent"

// SetLabel 设置日志标签（如 "Agent·服务端"）。
func SetLabel(l string) { label = l }

func prefix() string {
	return fmt.Sprintf("[%s] [%s] ", time.Now().Format("15:04:05"), label)
}

// Infof 普通信息。
func Infof(format string, v ...any) { fmt.Println(prefix() + fmt.Sprintf(format, v...)) }

// Warnf 警告。
func Warnf(format string, v ...any) { fmt.Fprintln(os.Stderr, prefix()+"[警告] "+fmt.Sprintf(format, v...)) }

// Errorf 错误。
func Errorf(format string, v ...any) { fmt.Fprintln(os.Stderr, prefix()+"[错误] "+fmt.Sprintf(format, v...)) }

// ---------- 活动聚合（子流级日志降噪） ----------
//
// 浏览器一个网页会并发数十条连接，子流级日志逐条输出会形成日志风暴
// （下游 GUI 逐条经 IPC 推送渲染，易冲爆前端主线程导致窗口假死）。
// 子流开/关这类高频事件不逐条输出，改为按窗口聚合汇总。

const actWindow = 5 * time.Second

var (
	actMu   sync.Mutex
	actN    int
	actOnce sync.Once
)

// CountActivity 记一次子流级活动：不立即输出，每 5 秒聚合输出一条汇总。
func CountActivity() {
	actOnce.Do(func() {
		go func() {
			for {
				time.Sleep(actWindow)
				actMu.Lock()
				n := actN
				actN = 0
				actMu.Unlock()
				if n > 0 {
					Infof("近 %d 秒处理连接 %d 条", int(actWindow.Seconds()), n)
				}
			}
		}()
	})
	actMu.Lock()
	actN++
	actMu.Unlock()
}
