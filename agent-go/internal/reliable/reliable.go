// Package reliable 在 UDP 之上实现 AES-256-GCM 加密的可靠有序字节流，作为持久 mux 会话的承载层。
// 每个报文带序号与累积确认，丢失重传、乱序重组；打洞残留探测等无法解密的报文静默丢弃。
package reliable

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"liyou-agent/internal/logx"
	"liyou-agent/internal/tunnel"
)

const (
	headerSize  = 12 // 4B seq + 2B flags + 2B 载荷长度 + 4B 累积确认
	flagFIN     = 0x0001
	segmentSize = 1200            // 单段载荷上限（含密文余量后低于常见 MTU，避免 IP 分片）
	sendWindowMin  = 128          // 发送窗口（≈153KB 在途）：内网实测够跑满；公网靠 rto/快速重传自适应而非扩窗
	sendWindowMax  = 128          // 与下限相同（恒窗）。实测扩窗在环回上触发队列延迟正反馈并伴生死锁，故回退
	probeBDPWindow = 128          // 保留常量供未来重启 BDP 探测
	reorderCap  = sendWindowMax + 128 // 乱序缓冲上限（必须大于发送窗口上限，否则高窗口下乱序段被弃收→死锁）
	recvBufMax  = 8 << 20         // 接收缓冲上限（须大于发送窗口在途字节量，否则满弃→死锁）
	rtoBase     = 300 * time.Millisecond
	rtoMax      = 3 * time.Second
	maxTries    = 25 // 单段最大重传次数（指数退避下约 30s 判死）
	deadAfter   = 30 * time.Second // 有在途段但毫无确认进展的判死时长
	retransTick = 80 * time.Millisecond
	fastResendMax = 64 // 快速重传单轮批量补段上限
	rttAlphaNum, rttAlphaDen = 1, 8 // RTT 平滑系数 1/8（EWMA）
	rttMin     = 1 * time.Millisecond
	rttMax     = 2 * time.Second
)

// ErrClosed 可靠通道已关闭。
var ErrClosed = errors.New("可靠通道已关闭")

// udpSock 底层 UDP 套接字的最小接口（便于测试注入丢包/乱序）。
type udpSock interface {
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
	Close() error
	SetReadDeadline(t time.Time) error
}

// segment 一个已发送未确认（或待重组）的报文段。
type segment struct {
	seq      uint32
	payload  []byte
	fin      bool
	tries    int
	lastSend time.Time
}

// Conn 可靠有序字节流（net.Conn 兼容，供 smux 等复用协议承载）。
type Conn struct {
	sock udpSock
	key  [32]byte

	peer atomic.Pointer[net.UDPAddr]

	die      chan struct{}
	dieOnce  sync.Once
	finOnce  sync.Once
	closeErr atomic.Pointer[error]

	// 接收侧
	rmu          sync.Mutex
	wantSeq      uint32
	reorder      map[uint32]*segment
	recvBuf      []byte
	finSeen      bool
	finSeq       uint32
	readErr      error
	readDeadline atomic.Value // time.Time

	// 发送侧
	wmu           sync.Mutex
	sendSeq       uint32
	outbuf        map[uint32]*segment
	lastAck       uint32
	dupAcks       uint32
	rto           time.Duration
	lastProgress  time.Time
	srtt          time.Duration // 平滑 RTT（EWMA），驱动发送窗口自适应
	win           int           // 当前发送窗口（在途段上限），随 RTT 动态调整
	sentAt        map[uint32]time.Time // 段首次发送时刻（RTT 测量，ACK 覆盖后清理）
	lastFastResend time.Time // 上次快速重传批量补段时间（按重传 tick 限速，防重复 ACK 风暴）
	writeDeadline atomic.Value // time.Time

	dataCh   chan struct{}
	windowCh chan struct{}
}

// New 在 sock 上建立可靠流。sock 的所有权移交本对象（Close 会关闭套接字）。
// peer 为对端地址（打洞结果），收到合法报文后跟随报文来源自动更新（容忍 NAT 重绑定）。
func New(sock udpSock, peer *net.UDPAddr, key [32]byte) *Conn {
	c := &Conn{
		sock:         sock,
		key:          key,
		die:          make(chan struct{}),
		reorder:      make(map[uint32]*segment),
		outbuf:       make(map[uint32]*segment),
		sentAt:       make(map[uint32]time.Time),
		rto:          rtoBase,
		win:          sendWindowMin,
		lastProgress: time.Now(),
		dataCh:       make(chan struct{}, 1),
		windowCh:     make(chan struct{}, 1),
	}
	c.peer.Store(peer)
	go c.recvLoop()
	go c.retransmitLoop()
	return c
}

// ---------- net.Conn 兼容 ----------

// Read 读取就绪字节；对端关闭且数据耗尽后返回 io.EOF。
func (c *Conn) Read(p []byte) (int, error) {
	for {
		c.rmu.Lock()
		if len(c.recvBuf) > 0 {
			n := copy(p, c.recvBuf)
			c.recvBuf = c.recvBuf[n:]
			c.rmu.Unlock()
			return n, nil
		}
		if c.readErr != nil {
			err := c.readErr
			c.rmu.Unlock()
			return 0, err
		}
		deadline, _ := c.readDeadline.Load().(time.Time)
		c.rmu.Unlock()

		var timeout <-chan time.Time
		var timer *time.Timer
		if !deadline.IsZero() {
			d := time.Until(deadline)
			if d <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(d)
			timeout = timer.C
		}
		select {
		case <-c.dataCh:
		case <-c.die:
			if timer != nil {
				timer.Stop()
			}
			return 0, c.closedErr()
		case <-timeout:
			return 0, os.ErrDeadlineExceeded
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// Write 发送字节（可靠送达；窗口满时阻塞形成背压）。
func (c *Conn) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if err := c.waitWindow(); err != nil {
			return total, err
		}
		n := len(p)
		if n > segmentSize {
			n = segmentSize
		}
		chunk := make([]byte, n)
		copy(chunk, p[:n])
		c.wmu.Lock()
		if c.isClosedLocked() {
			c.wmu.Unlock()
			return total, ErrClosed
		}
		s := &segment{seq: c.sendSeq, payload: chunk, tries: 1, lastSend: time.Now()}
		c.sendSeq++
		c.outbuf[s.seq] = s
		if _, ok := c.sentAt[s.seq]; !ok {
			c.sentAt[s.seq] = s.lastSend // 记录首发时刻供 RTT 测量（重传不刷新）
		}
		c.wmu.Unlock()
		c.transmit(s)
		p = p[n:]
		total += n
	}
	return total, nil
}

// Close 发送 FIN（尽力重传 2 秒）后关闭底层套接字；可安全重复调用。
func (c *Conn) Close() error {
	c.rmu.Lock()
	if c.readErr == nil {
		c.readErr = ErrClosed
	}
	c.rmu.Unlock()
	c.finOnce.Do(func() {
		c.wmu.Lock()
		closed := c.isClosedLocked()
		if !closed {
			s := &segment{seq: c.sendSeq, fin: true, tries: 1, lastSend: time.Now()}
			c.sendSeq++
			c.outbuf[s.seq] = s
			c.wmu.Unlock()
			c.transmit(s)
		} else {
			c.wmu.Unlock()
		}
		go func() {
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				c.wmu.Lock()
				n := len(c.outbuf)
				c.wmu.Unlock()
				if n == 0 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			c.shutdown(errors.New("本端关闭"))
		}()
	})
	return nil
}

// LocalAddr 返回底层套接字本地地址。
func (c *Conn) LocalAddr() net.Addr {
	if nc, ok := c.sock.(net.Conn); ok {
		return nc.LocalAddr()
	}
	return nil
}

// RemoteAddr 返回当前对端地址（跟随合法报文来源更新）。
func (c *Conn) RemoteAddr() net.Addr {
	return c.peer.Load()
}

// SetDeadline 同时设置读写截止时间。
func (c *Conn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

// SetReadDeadline 设置读截止时间。
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.readDeadline.Store(t)
	c.notify(c.dataCh)
	return nil
}

// SetWriteDeadline 设置写截止时间。
func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline.Store(t)
	c.notify(c.windowCh)
	return nil
}

// ---------- 内部：发送 ----------

func (c *Conn) waitWindow() error {
	for {
		c.wmu.Lock()
		full := len(c.outbuf) >= c.win
		c.wmu.Unlock()
		if !full {
			return nil
		}
		var timeout <-chan time.Time
		var timer *time.Timer
		if dl, _ := c.writeDeadline.Load().(time.Time); !dl.IsZero() {
			d := time.Until(dl)
			if d <= 0 {
				return os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(d)
			timeout = timer.C
		}
		select {
		case <-c.windowCh:
		case <-c.die:
			if timer != nil {
				timer.Stop()
			}
			return c.closedErr()
		case <-timeout:
			return os.ErrDeadlineExceeded
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// transmit 加密并发送一个报文段。
func (c *Conn) transmit(s *segment) {
	buf := make([]byte, headerSize, headerSize+len(s.payload))
	binary.BigEndian.PutUint32(buf[0:4], s.seq)
	var flags uint16
	if s.fin {
		flags |= flagFIN
	}
	binary.BigEndian.PutUint16(buf[4:6], flags)
	binary.BigEndian.PutUint16(buf[6:8], uint16(len(s.payload)))
	c.rmu.Lock()
	ack := c.wantSeq
	c.rmu.Unlock()
	binary.BigEndian.PutUint32(buf[8:12], ack)
	buf = append(buf, s.payload...)
	peer := c.peer.Load()
	if peer == nil {
		return
	}
	_, _ = c.sock.WriteToUDP(tunnel.Seal(c.key, buf), peer)
}

// sendAck 发送纯确认报文。
func (c *Conn) sendAck() {
	c.rmu.Lock()
	ack := c.wantSeq
	c.rmu.Unlock()
	buf := make([]byte, headerSize)
	binary.BigEndian.PutUint32(buf[8:12], ack)
	peer := c.peer.Load()
	if peer == nil {
		return
	}
	_, _ = c.sock.WriteToUDP(tunnel.Seal(c.key, buf), peer)
}

// retransmitLoop 周期重传超时在途段，并做判死检测。
func (c *Conn) retransmitLoop() {
	tick := time.NewTicker(retransTick)
	defer tick.Stop()
	for {
		select {
		case <-c.die:
			return
		case <-tick.C:
		}
		c.wmu.Lock()
		if len(c.outbuf) == 0 {
			c.wmu.Unlock()
			continue
		}
		now := time.Now()
		if now.Sub(c.lastProgress) > deadAfter {
			inflight := len(c.outbuf)
			w := c.win
			rtt := c.srtt
			var oldestSeq uint32
			var oldestAge time.Duration
			for _, s := range c.outbuf {
				if age := now.Sub(s.lastSend); oldestSeq == 0 || s.seq < oldestSeq {
					oldestSeq = s.seq
					oldestAge = age
				}
			}
			c.wmu.Unlock()
			logx.Errorf("可靠通道判死：在途 %d 段 win=%d srtt=%v 最老段 seq=%d 距上次发送 %v（%v 无确认进展）",
				inflight, w, rtt, oldestSeq, oldestAge, deadAfter)
			c.shutdown(errors.New("对端持续无确认，可靠通道判死"))
			return
		}
		resend := false
		dead := false
		var expired []*segment
		for _, s := range c.outbuf {
			if now.Sub(s.lastSend) < c.rto {
				continue
			}
			s.tries++
			s.lastSend = now
			if s.tries > maxTries {
				dead = true
				break
			}
			resend = true
			expired = append(expired, s)
		}
		if dead {
			c.wmu.Unlock()
			c.shutdown(errors.New("重传超限，可靠通道判死"))
			return
		}
		if resend {
			c.rto *= 2
			if c.rto > rtoMax {
				c.rto = rtoMax
			}
		}
		c.wmu.Unlock()
		for _, s := range expired {
			c.transmit(s)
		}
	}
}

// processAck 处理对端累积确认；按确认进展平滑 RTT 并动态调整 rto，
// 连续重复确认触发限速快速重传（每个重传 tick 至多批量补齐缺失窗口内
// 最老的连续区段一轮）。
func (c *Conn) processAck(ack uint32) {
	if ack == 0 {
		return
	}
	var fast []*segment
	var newestSeq uint32 // 本批 ACK 覆盖的最大序号（最新发出段）
	var rttSample time.Duration
	dead := false
	c.wmu.Lock()
	progressed := false
	now := time.Now()
	for seq := range c.outbuf {
		if seq < ack {
			if t, ok := c.sentAt[seq]; ok {
				if seq >= newestSeq {
					newestSeq = seq
					rttSample = now.Sub(t) // 仅取最新发出段的样本：旧段跨整批发送周期，样本失真
				}
				delete(c.sentAt, seq)
			}
			delete(c.outbuf, seq)
			progressed = true
		}
	}
	if progressed && rttSample > 0 {
		c.updateRTT(rttSample)
		// 自适应落地为 rto 动态化：rtt 大（公网抖动/丢包）时放宽超时，避免过早重传
		// 雪崩；发送窗口保持恒定（实测扩窗会触发队列延迟正反馈并死锁）。
		if nr := 2 * c.srtt; nr > rtoBase {
			if nr > rtoMax {
				nr = rtoMax
			}
			c.rto = nr
		} else {
			c.rto = rtoBase
		}
	}
	if progressed {
		c.lastProgress = time.Now()
		c.lastAck = ack
		c.dupAcks = 0
	} else if ack == c.lastAck {
		c.dupAcks++
		// 批量快速重传（限速）：缺失窗口内从最老段起的连续区段一次补齐，
		// 丢多段时不再逐轮各补 1 段；但每个重传 tick 至多补一轮——对端逐包
		// 确认，若每个重复 ACK 都触发批量重传会被指数放大成重传风暴
		// （实测：双端各烧 1.5 核、应用层零进展的无限活锁）。
		if c.dupAcks >= 3 && now.Sub(c.lastFastResend) >= retransTick {
			c.lastFastResend = now
			minSeq := uint32(0)
			found := false
			for seq := range c.outbuf {
				if !found || seq < minSeq {
					minSeq = seq
					found = true
				}
			}
			for seq := minSeq; found && len(fast) < fastResendMax; seq++ {
				s, ok := c.outbuf[seq]
				if !ok {
					break // 连续区段结束
				}
				s.tries++
				if s.tries > maxTries {
					dead = true
					break
				}
				s.lastSend = now
				fast = append(fast, s)
			}
		}
	}
	c.wmu.Unlock()
	if progressed {
		c.notify(c.windowCh)
	}
	if dead {
		c.shutdown(errors.New("重传超限，可靠通道判死"))
		return
	}
	for _, s := range fast {
		c.transmit(s)
	}
}

// updateRTT 平滑更新 RTT（EWMA 1/8），仅由 ACK 覆盖首发段时测量（Karn 算法）。
func (c *Conn) updateRTT(d time.Duration) {
	if d < rttMin {
		d = rttMin
	}
	if d > rttMax {
		d = rttMax
	}
	if c.srtt == 0 {
		c.srtt = d
		return
	}
	c.srtt += (d - c.srtt) * rttAlphaNum / rttAlphaDen
}

// adaptWindow 保守的自适应策略：
//   - RTT < 50ms（内网/近距）：保持窗口下限（≈153KB 在途），与历史版本行为一致，
//     实测更大窗口在环回上反而引入队列延迟正反馈（大窗→排队→RTT 样本虚高→更大窗）；
//   - RTT ≥ 50ms（公网高时延）：窗口按目标 4MB/s 折算 BDP，上限 sendWindowMax。
// 只增不减（重传退避期窗口不塌缩，避免抖动震荡）。
func (c *Conn) adaptWindow() {
	if c.srtt <= 0 {
		return
	}
	var bdp int
	if c.srtt < 50*time.Millisecond {
		bdp = sendWindowMin
	} else {
		bdp = int(probeBDPWindow * time.Duration(c.srtt) / (50 * time.Millisecond))
		if bdp < sendWindowMin {
			bdp = sendWindowMin
		}
		if bdp > sendWindowMax {
			bdp = sendWindowMax
		}
	}
	if bdp > c.win {
		c.win = bdp
	}
}

// ---------- 内部：接收 ----------

func (c *Conn) recvLoop() {
	buf := make([]byte, headerSize+segmentSize+32)
	for {
		n, from, err := c.sock.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-c.die:
			default:
				c.shutdown(errors.New("底层套接字读取结束"))
			}
			return
		}
		plain, derr := tunnel.Open(c.key, buf[:n])
		if derr != nil || len(plain) < headerSize {
			continue // 打洞残留探测等非法报文，静默丢弃
		}
		seq := binary.BigEndian.Uint32(plain[0:4])
		flags := binary.BigEndian.Uint16(plain[4:6])
		plen := int(binary.BigEndian.Uint16(plain[6:8]))
		ack := binary.BigEndian.Uint32(plain[8:12])
		if plen > len(plain)-headerSize {
			continue // 长度不符
		}
		if from != nil {
			c.peer.Store(from)
		}
		c.processAck(ack)
		// 仅对携带数据/FIN 的报文回确认，避免纯确认报文互相触发形成确认风暴
		if plen > 0 || flags&flagFIN != 0 {
			c.processSegment(seq, plain[headerSize:headerSize+plen], flags&flagFIN != 0)
			c.sendAck()
		}
	}
}

// processSegment 顺序递交付载，乱序段暂存重组。
func (c *Conn) processSegment(seq uint32, payload []byte, fin bool) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	if c.readErr != nil {
		return
	}
	if seq < c.wantSeq {
		return // 重复段
	}
	if seq != c.wantSeq {
		if _, ok := c.reorder[seq]; ok || len(c.reorder) >= reorderCap {
			return
		}
		cp := make([]byte, len(payload))
		copy(cp, payload)
		c.reorder[seq] = &segment{payload: cp, fin: fin}
		return
	}
	if !c.appendRecv(payload) {
		return // 缓冲已满，不确认，等待对端重传
	}
	if fin {
		c.finSeen = true
		c.finSeq = seq
	}
	c.wantSeq++
	for {
		s, ok := c.reorder[c.wantSeq]
		if !ok {
			break
		}
		delete(c.reorder, c.wantSeq)
		if !c.appendRecv(s.payload) {
			return
		}
		if s.fin {
			c.finSeen = true
			c.finSeq = c.wantSeq
		}
		c.wantSeq++
	}
	if c.finSeen && c.wantSeq == c.finSeq+1 && c.readErr == nil {
		c.readErr = io.EOF
	}
	c.notify(c.dataCh)
}

// appendRecv 追加数据到接收缓冲；超限返回 false。
func (c *Conn) appendRecv(payload []byte) bool {
	if len(c.recvBuf)+len(payload) > recvBufMax {
		return false
	}
	c.recvBuf = append(c.recvBuf, payload...)
	return true
}

// ---------- 内部：关闭与通知 ----------

func (c *Conn) shutdown(err error) {
	c.closeErr.Store(&err)
	c.dieOnce.Do(func() { close(c.die) })
	c.sock.Close()
}

func (c *Conn) isClosedLocked() bool {
	select {
	case <-c.die:
		return true
	default:
		return false
	}
}

func (c *Conn) closedErr() error {
	if p := c.closeErr.Load(); p != nil {
		return *p
	}
	return ErrClosed
}

func (c *Conn) notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
