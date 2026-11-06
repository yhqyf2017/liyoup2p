// Package tunnel 隧道帧编解码与 AES-256-GCM 加解密。
// 帧格式：[1B 类型][4B 大端长度][payload]，payload = 12B nonce + GCM 密文。
// 类型：0=data 1=close。适用于 TCP 中继（流式）与 UDP P2P（每报文一帧）。
package tunnel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

// 帧类型常量。
const (
	FrameData  byte = 0
	FrameClose byte = 1
)

// MaxPayload 最大载荷 1MB。
const MaxPayload = 1 << 20

// ErrDecrypt 解密失败（UDP 模式下多为打洞残留探测包，调用方应跳过而非断链）。
var ErrDecrypt = errors.New("解密失败")

// ErrNotFrame 报文不是合法隧道帧（UDP 模式下多为打洞残留探测包，调用方应跳过而非断链）。
var ErrNotFrame = errors.New("非隧道帧")

// UDPChunk UDP 模式单帧数据上限（留出头部与密文余量）。
const UDPChunk = 30000

// Seal 用会话密钥加密，返回 nonce||密文。
func Seal(key [32]byte, plain []byte) []byte {
	gcm := mustGCM(key)
	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)
	return gcm.Seal(nonce, nonce, plain, nil)
}

// Open 解密 nonce||密文。
func Open(key [32]byte, sealed []byte) ([]byte, error) {
	gcm := mustGCM(key)
	ns := gcm.NonceSize()
	if len(sealed) < ns {
		return nil, errors.New("密文过短")
	}
	return gcm.Open(nil, sealed[:ns], sealed[ns:], nil)
}

// gcmCache 按 key 缓存 AEAD：aes.NewCipher+NewGCM 含完整 key schedule，
// 逐包重建在高速率下成为 CPU 瓶颈，故每个会话密钥只构建一次。
var gcmCache sync.Map // [32]byte -> cipher.AEAD

func mustGCM(key [32]byte) cipher.AEAD {
	if v, ok := gcmCache.Load(key); ok {
		return v.(cipher.AEAD)
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	gcmCache.Store(key, gcm)
	return gcm
}

// UDPBuffer 数据面 UDP 收发缓冲目标值：默认 64KB 小于可靠层在途窗口突发
// （128 段×~1.2KB×报文余量≈153KB），高速率下易在内核层丢包。
// 实测 Windows 上调到 4MB 与高吞吐组合触发接收侧死锁，取保守值 256KB。
const UDPBuffer = 256 << 10

// SetUDPBuffer 调大 UDP 套接字收发缓冲；失败仅记日志不致命（各 OS 上限不同）。
func SetUDPBuffer(conn *net.UDPConn, logf func(format string, v ...any)) {
	for _, dir := range []struct {
		name string
		fn   func(int) error
	}{
		{"接收", conn.SetReadBuffer},
		{"发送", conn.SetWriteBuffer},
	} {
		if err := dir.fn(UDPBuffer); err != nil {
			if logf != nil {
				logf("UDP %s缓冲调至 %d 失败（不影响功能）：%v", dir.name, UDPBuffer, err)
			}
		}
	}
}

// WriteFrame 写一帧（TCP 流式）。
func WriteFrame(w io.Writer, t byte, payload []byte, key [32]byte) error {
	sealed := Seal(key, payload)
	hdr := [5]byte{t}
	binary.BigEndian.PutUint32(hdr[1:5], uint32(len(sealed)))
	buf := make([]byte, 0, 5+len(sealed))
	buf = append(buf, hdr[:]...)
	buf = append(buf, sealed...)
	_, err := w.Write(buf)
	return err
}

// ReadFrame 读一帧（TCP 流式）。
func ReadFrame(r io.Reader, key [32]byte) (byte, []byte, error) {
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return 0, nil, err
	}
	t := hdr[0]
	n := binary.BigEndian.Uint32(hdr[1:5])
	if n > MaxPayload {
		return 0, nil, fmt.Errorf("帧长 %d 超限", n)
	}
	sealed := make([]byte, n)
	if _, err := io.ReadFull(r, sealed); err != nil {
		return 0, nil, err
	}
	plain, err := Open(key, sealed)
	if err != nil {
		return 0, nil, fmt.Errorf("解密失败: %w", err)
	}
	return t, plain, nil
}

// WriteUDPFrame 发送一帧（单个 UDP 报文）。
func WriteUDPFrame(conn *net.UDPConn, addr *net.UDPAddr, t byte, payload []byte, key [32]byte) error {
	if addr == nil {
		return errors.New("对端地址未知")
	}
	sealed := Seal(key, payload)
	hdr := [5]byte{t}
	binary.BigEndian.PutUint32(hdr[1:5], uint32(len(sealed)))
	buf := make([]byte, 0, 5+len(sealed))
	buf = append(buf, hdr[:]...)
	buf = append(buf, sealed...)
	_, err := conn.WriteToUDP(buf, addr)
	return err
}

// ReadUDPFrame 接收一帧（单个 UDP 报文）。
func ReadUDPFrame(conn *net.UDPConn, key [32]byte) (byte, []byte, *net.UDPAddr, error) {
	buf := make([]byte, MaxPayload+5)
	n, from, err := conn.ReadFromUDP(buf)
	if err != nil {
		return 0, nil, nil, err
	}
	if n < 5 {
		return 0, nil, nil, fmt.Errorf("%w: 报文过短", ErrNotFrame)
	}
	t := buf[0]
	size := binary.BigEndian.Uint32(buf[1:5])
	if int(size) != n-5 {
		return 0, nil, nil, fmt.Errorf("%w: 报文长度不符", ErrNotFrame)
	}
	plain, err := Open(key, buf[5:n])
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%w: %v", ErrDecrypt, err)
	}
	return t, plain, from, nil
}
