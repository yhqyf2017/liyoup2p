//go:build windows

package ws

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// osInfo 操作系统描述（如 "windows 10.0 amd64"），平台设备管理展示用。
var osInfo = buildOSInfo()

func buildOSInfo() string {
	maj, min, build, ok := windowsVersion()
	if ok {
		return fmt.Sprintf("windows %d.%d build %d %s", maj, min, build, runtime.GOARCH)
	}
	return "windows " + runtime.GOARCH
}

// windowsVersion 经 ntdll!RtlGetVersion 取真实系统版本
// （Win7 上 GetVersionEx 受 manifest 兼容性声明影响会谎报为 6.2，RtlGetVersion 不会）。
func windowsVersion() (maj, min, build uint32, ok bool) {
	rtl := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	type osVersionInfoW struct {
		Size              uint32
		Major             uint32
		Minor             uint32
		Build             uint32
		PlatformID        uint32
		CSDVersion        [128]uint16
		ServicePackMajor  uint16
		ServicePackMinor  uint16
		SuiteMask         uint16
		ProductType       uint8
		Reserved          uint8
	}
	var vi osVersionInfoW
	vi.Size = uint32(unsafe.Sizeof(vi))
	r1, _, _ := rtl.Call(uintptr(unsafe.Pointer(&vi)))
	if r1 != 0 {
		return 0, 0, 0, false
	}
	return vi.Major, vi.Minor, vi.Build, true
}
