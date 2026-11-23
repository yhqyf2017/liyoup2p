//go:build darwin

package main

// macOS 单实例：v1 简化为空实现（允许多开，互不干扰——Agent 由连接状态保护）。
// Windows 版的自研单实例（singleton.go）依赖 win32 消息机制，不适用于 macOS。

func setupSingleInstance(app *App) {}
