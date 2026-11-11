//go:build windows

// Package agent 实现被控端：屏幕采集推送、键鼠注入、抢占状态广播。
package agent

import (
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"lanrd/internal/capture"
	"lanrd/internal/codec"
	"lanrd/internal/config"
	"lanrd/internal/inject"
	"lanrd/internal/proto"
)

// keyframeInterval 关键帧全量刷新间隔。TCP 传输可靠、脏块不丢，
// 关键帧仅作兜底，长间隔可避免周期性全屏编码尖刺。
const keyframeInterval = 30 * time.Second

// statusInterval 抢占状态广播间隔。
const statusInterval = 500 * time.Millisecond

// session 一个控制端连接对应的会话。
type session struct {
	conn    net.Conn
	wr      chan *proto.Message
	tiler   *codec.Tiler
	done    chan struct{}
	lastKey time.Time
}

// Agent 被控端服务。
type Agent struct {
	cfg      *config.Config
	cap      *capture.Capturer
	mon      *inject.Monitor
	inj      *inject.Injector
	host     string
	mu       sync.Mutex
	cur      *session
	lastCli  atomic.Int64
	dragging atomic.Bool // 控制端是否按住左键(拖拽时移动也要立即刷新)
	log      *slog.Logger
	stopMon  func()
	wake     chan struct{} // 输入到达后立即唤醒抓屏

	ln       net.Listener
	done     chan struct{} // Stop 时关闭，通知各工作协程退出
	capMu    sync.Mutex    // 保护 GDI 资源的关闭与抓屏互斥
	stopOnce sync.Once
}

// New 创建被控端服务。
func New(cfg *config.Config, log *slog.Logger) (*Agent, error) {
	host, _ := os.Hostname()
	cp, err := capture.New()
	if err != nil {
		return nil, err
	}
	mon, stopMon, err := inject.StartMonitor()
	if err != nil {
		cp.Close()
		return nil, err
	}
	a := &Agent{
		cfg:  cfg,
		cap:  cp,
		mon:  mon,
		inj:  inject.New(),
		host: host,
		log:  log,
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	a.mon.SetBlocking(cfg.Input.BlockLocal)
	a.stopMon = stopMon
	return a, nil
}

// Close 释放被控端资源(等价于 Stop)。
func (a *Agent) Close() { a.Stop() }

// Stop 停止服务并释放资源(幂等): 关闭监听、断开当前会话、
// 结束抓屏/状态协程、释放 GDI 资源并卸载钩子。停止后不可再 Run。
func (a *Agent) Stop() {
	a.stopOnce.Do(func() {
		close(a.done)
		if a.ln != nil {
			a.ln.Close() // 解除 Accept 阻塞
		}
		a.mu.Lock()
		s := a.cur
		a.cur = nil
		a.mu.Unlock()
		if s != nil {
			a.closeSession(s)
		}
		a.capMu.Lock()
		a.cap.Close()
		a.capMu.Unlock()
		a.stopMon()
		a.log.Info("服务端已停止")
	})
}

// Run 启动监听与各工作循环，阻塞直至出错或被 Stop。
func (a *Agent) Run() error {
	ln, err := net.Listen("tcp", a.cfg.Server.Addr)
	if err != nil {
		return err
	}
	a.ln = ln
	defer ln.Close()
	w, h := a.cap.Size()
	a.log.Info("被控端就绪", "addr", a.cfg.Server.Addr,
		"screen", fmt.Sprintf("%dx%d", w, h), "token", a.cfg.Server.Token != "")
	printLANIPs(a.log)

	go a.captureLoop()
	go a.statusLoop()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-a.done:
				return nil // 主动停止
			default:
				return err
			}
		}
		go a.handleConn(conn)
	}
}

// handleConn 处理一个控制端连接：握手鉴权、会话接管、消息循环。
func (a *Agent) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	m, err := proto.Read(conn)
	if err != nil || m.Type != proto.MsgHello {
		a.log.Warn("握手失败", "remote", conn.RemoteAddr(), "err", err)
		return
	}
	hello, err := proto.DecodeHello(m.Body)
	if err != nil || hello.Version != proto.ProtocolVersion {
		a.log.Warn("协议版本不匹配", "remote", conn.RemoteAddr())
		return
	}
	if a.cfg.Server.Token != "" &&
		subtle.ConstantTimeCompare([]byte(hello.Token), []byte(a.cfg.Server.Token)) != 1 {
		a.log.Warn("口令错误", "remote", conn.RemoteAddr())
		return
	}
	conn.SetReadDeadline(time.Time{})
	s := &session{conn: conn, wr: make(chan *proto.Message, 2048), done: make(chan struct{})}
	a.takeover(s)
	a.log.Info("控制端接入", "remote", conn.RemoteAddr(), "name", hello.Name)

	body, _ := proto.EncodeHelloAck(a.ack())
	if !a.send(s, &proto.Message{Type: proto.MsgHelloAck, Body: body}) {
		return
	}
	go a.writer(s)
	a.reader(s)
}

// ack 构造握手应答。
func (a *Agent) ack() *proto.HelloAck {
	w, h := a.cap.Size()
	return &proto.HelloAck{
		Version: proto.ProtocolVersion, Host: a.host,
		W: w, H: h, Tile: a.cfg.Capture.Tile,
	}
}

// takeover 将 s 设为当前会话，顶替旧会话。
func (a *Agent) takeover(s *session) {
	a.mu.Lock()
	old := a.cur
	a.cur = s
	w, h := a.cap.Size()
	s.tiler = codec.New(w, h, a.cfg.Capture.Tile)
	a.mu.Unlock()
	if old != nil {
		a.closeSession(old)
		a.log.Info("旧会话被顶替", "remote", old.conn.RemoteAddr())
	}
}

// send 投递消息到会话写队列(队列满时阻塞以形成背压)，会话已关闭返回 false。
func (a *Agent) send(s *session, m *proto.Message) bool {
	select {
	case <-s.done:
		return false
	case s.wr <- m:
		return true
	}
}

// appendMessage 将一条消息追加到发送批缓冲区。
func appendMessage(buf []byte, m *proto.Message) []byte {
	var hdr [5]byte
	hdr[0] = byte(m.Type)
	binary.LittleEndian.PutUint32(hdr[1:5], uint32(len(m.Body)))
	buf = append(buf, hdr[:]...)
	return append(buf, m.Body...)
}

// writer 会话写协程：将队列中的消息尽量合并为少量 TCP 写，减少系统调用与小包。
func (a *Agent) writer(s *session) {
	const maxBatch = 512 << 10
	buf := make([]byte, 0, 64<<10)
	for {
		select {
		case <-s.done:
			return
		case m := <-s.wr:
			buf = appendMessage(buf[:0], m)
		}
	drain:
		for len(buf) < maxBatch {
			select {
			case m := <-s.wr:
				buf = appendMessage(buf, m)
			default:
				break drain
			}
		}
		s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := s.conn.Write(buf); err != nil {
			a.closeSession(s)
			return
		}
	}
}

// reader 会话读协程：分发键鼠/锁定/心跳消息。
func (a *Agent) reader(s *session) {
	defer a.closeSession(s)
	vx, vy := a.cap.Origin()
	for {
		m, err := proto.Read(s.conn)
		if err != nil {
			return
		}
		switch m.Type {
		case proto.MsgMouseMove:
			if x, y, e := proto.DecodeMouseMove(m.Body); e == nil {
				a.inj.MouseMove(vx, vy, int(x), int(y))
				a.lastCli.Store(time.Now().UnixNano())
				if a.dragging.Load() {
					a.pokeCapture() // 拖拽中移动会改变画面，立即刷新
				}
			}
		case proto.MsgMouseBtn:
			if btn, down, x, y, e := proto.DecodeMouseBtn(m.Body); e == nil {
				if btn == proto.BtnLeft {
					a.dragging.Store(down)
				}
				a.inj.MouseMove(vx, vy, int(x), int(y))
				a.inj.MouseButton(btn, down)
				a.lastCli.Store(time.Now().UnixNano())
				a.pokeCapture()
			}
		case proto.MsgWheel:
			if d, x, y, e := proto.DecodeWheel(m.Body); e == nil {
				a.inj.MouseMove(vx, vy, int(x), int(y))
				a.inj.Wheel(d)
				a.lastCli.Store(time.Now().UnixNano())
				a.pokeCapture()
			}
		case proto.MsgKey:
			if scan, down, e := proto.DecodeKey(m.Body); e == nil {
				a.inj.Key(scan, down)
				a.lastCli.Store(time.Now().UnixNano())
				a.pokeCapture()
			}
		case proto.MsgInputLock:
			if on, e := proto.DecodeInputLock(m.Body); e == nil {
				a.mon.SetBlocking(on)
				a.log.Info("远端物理键鼠锁定状态变更", "locked", on)
				a.pushStatus()
			}
		case proto.MsgPing:
			ts, _ := proto.DecodePing(m.Body)
			a.send(s, &proto.Message{Type: proto.MsgPong, Body: proto.EncodePing(ts)})
		}
	}
}

// closeSession 关闭会话并从当前会话中摘除。
func (a *Agent) closeSession(s *session) {
	a.mu.Lock()
	if a.cur == s {
		a.cur = nil
	}
	a.mu.Unlock()
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.conn.Close()
	a.log.Info("会话结束", "remote", s.conn.RemoteAddr())
}
