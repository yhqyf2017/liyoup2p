//go:build !windows

package ws

import "runtime"

// osInfo 非 Windows 平台的系统描述（如 "linux amd64"）。
var osInfo = runtime.GOOS + " " + runtime.GOARCH
