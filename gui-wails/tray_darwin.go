//go:build darwin

package main

// macOS 托盘 no-op：v1 无托盘常驻，跨平台代码中的托盘调用在此退化为空操作。

// traySetRunning macOS 无托盘，无需同步连接状态。
func traySetRunning(running bool) {}

// trayShutdown macOS 无托盘，无需清理。
func trayShutdown() {}
