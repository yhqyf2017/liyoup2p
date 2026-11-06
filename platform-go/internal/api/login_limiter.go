package api

import (
	"sync"
	"time"
)

// 登录防爆破限速（内存实现，进程内共享）：
// 同一来源 IP 在 60 秒窗口内累计 10 次「用户名或密码错误」即锁定该 IP 15 分钟；
// 登录成功立即清零；锁定期间无论凭据是否正确一律拒绝。
const (
	loginFailWindow   = time.Minute
	loginFailMax      = 10
	loginLockDuration = 15 * time.Minute
)

// loginLimiter 登录失败记录与锁定状态。
type loginLimiter struct {
	mu     sync.Mutex
	fails  map[string][]time.Time // ip -> 窗口内的失败时刻
	locked map[string]time.Time   // ip -> 锁定到期时刻
}

var loginGuard = newLoginLimiter()

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{fails: make(map[string][]time.Time), locked: make(map[string]time.Time)}
}

// check 该 IP 是否处于锁定中及剩余时长。
func (l *loginLimiter) check(ip string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)
	if until, ok := l.locked[ip]; ok {
		if now.Before(until) {
			return true, until.Sub(now)
		}
		delete(l.locked, ip)
	}
	return false, 0
}

// recordFail 记录一次密码错误；窗口内累计达上限即锁定 15 分钟。
func (l *loginLimiter) recordFail(ip string) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if now.Sub(t) < loginFailWindow {
			keep = append(keep, t)
		}
	}
	keep = append(keep, now)
	if len(keep) >= loginFailMax {
		l.locked[ip] = now.Add(loginLockDuration)
		delete(l.fails, ip)
		return
	}
	l.fails[ip] = keep
}

// reset 登录成功后清空该 IP 的失败记录。
func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

func (l *loginLimiter) sweepLocked(now time.Time) {
	for ip, until := range l.locked {
		if now.After(until) {
			delete(l.locked, ip)
		}
	}
}
