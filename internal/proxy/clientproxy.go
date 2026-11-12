// Package proxy 实现内网代理：控制端在本机开启静态端口映射，
// 目标连接按通道复用后经远程控制连接转发，由被控端在其所在
// 内网发起真实连接，从而让控制端可访问被控端的内网资源。
package proxy

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lanrd/internal/proto"
)

// 通道传输参数。
const (
	chunkSize  = 32 << 10         // 单条数据消息的最大负载(TCP)
	recvQueue  = 64               // 每通道接收队列深度(TCP 上限约 2MB)
	ackTimeout = 15 * time.Second // 等待被控端拨号结果的超时
)

// ErrProxyClosed 代理已停止。
var ErrProxyClosed = errors.New("lanrd/proxy: proxy closed")

// ClientProxy 控制端内网代理：多路复用器本体，
// 承载静态端口转发入口，通道经远程控制连接由被控端代为拨号。
type ClientProxy struct {
	send func(t proto.Type, body []byte) error
	log  *slog.Logger

	mu       sync.Mutex
	nextID   uint32
	chans    map[uint32]*clientChan
	closed   bool
	stoppers []func() // 各监听器的清理函数

	fwdCount  atomic.Int32 // 静态转发规则数
	active    atomic.Int32 // 活跃通道数
}

// clientChan 单条代理通道的控制端状态。
type clientChan struct {
	id        uint32
	recv      chan []byte // 被控端回传数据队列
	ack       chan bool   // 拨号结果
	dead      chan struct{}
	closeOnce sync.Once
}

// NewClientProxy 创建控制端代理复用器，send 用于向被控端发送协议消息。
func NewClientProxy(send func(t proto.Type, body []byte) error, log *slog.Logger) *ClientProxy {
	if log == nil {
		log = slog.Default()
	}
	return &ClientProxy{
		send:  send,
		log:   log,
		chans: make(map[uint32]*clientChan),
	}
}

// StartForward 按 rules 启动静态端口转发(本机端口 -> 被控端内网地址)。
func (p *ClientProxy) StartForward(rules []ForwardRule) error {
	for i, r := range rules {
		if err := p.startOneForward(r); err != nil {
			return fmt.Errorf("lanrd/proxy: 规则[%d] %s: %w", i, r, err)
		}
		p.fwdCount.Add(1)
	}
	return nil
}

// startOneForward 启动单条转发规则的监听。
func (p *ClientProxy) startOneForward(r ForwardRule) error {
	switch r.Proto {
	case ProtoTCP:
		ln, err := net.Listen("tcp", r.Local)
		if err != nil {
			return err
		}
		p.addStopper(func() { ln.Close() })
		p.log.Info("端口映射已启动", "rule", r.String())
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					p.Stop()
					return
				}
				go p.serveForward(conn, r.Remote)
			}
		}()
		return nil
	case ProtoUDP:
		ua, err := net.ResolveUDPAddr("udp", r.Local)
		if err != nil {
			return err
		}
		pc, err := net.ListenUDP("udp", ua)
		if err != nil {
			return err
		}
		p.addStopper(func() { pc.Close() })
		p.log.Info("端口映射已启动", "rule", r.String())
		go p.serveUDPForward(pc, r.Remote)
		return nil
	}
	return fmt.Errorf("lanrd/proxy: 不支持的协议 %q", r.Proto)
}

// addStopper 登记监听器清理函数。
func (p *ClientProxy) addStopper(f func()) {
	p.mu.Lock()
	p.stoppers = append(p.stoppers, f)
	p.mu.Unlock()
}

// Stop 停止代理: 关闭全部监听器与通道。
func (p *ClientProxy) Stop() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	stoppers := p.stoppers
	p.stoppers = nil
	chans := p.chans
	p.chans = make(map[uint32]*clientChan)
	p.mu.Unlock()
	for _, stop := range stoppers {
		stop()
	}
	for _, ch := range chans {
		p.kill(ch)
	}
}

// Active 返回当前活跃通道数。
func (p *ClientProxy) Active() int { return int(p.active.Load()) }

// Summary 返回代理状态摘要(HUD 展示用)，全部未启用返回空串。
func (p *ClientProxy) Summary() string {
	var parts []string
	if n := int(p.fwdCount.Load()); n > 0 {
		parts = append(parts, fmt.Sprintf("fwd=%d", n))
	}
	if n := p.Active(); n > 0 {
		parts = append(parts, fmt.Sprintf("ch=%d", n))
	}
	return strings.Join(parts, " ")
}

// HandleAgent 处理被控端回传的代理消息(ACK/DATA/CLOSE)，须非阻塞。
func (p *ClientProxy) HandleAgent(m *proto.Message) {
	switch m.Type {
	case proto.MsgProxyAck:
		if id, ok, err := proto.DecodeProxyAck(m.Body); err == nil {
			if ch := p.lookup(id); ch != nil {
				select {
				case ch.ack <- ok:
				default:
				}
			}
		}
	case proto.MsgProxyDataS:
		if id, data, err := proto.DecodeProxyData(m.Body); err == nil {
			p.enqueue(id, data)
		}
	case proto.MsgProxyCloseS:
		if id, err := proto.DecodeProxyClose(m.Body); err == nil {
			if ch := p.lookup(id); ch != nil {
				p.kill(ch)
			}
		}
	}
}

// serveForward 处理静态转发的一条本地 TCP 连接。
func (p *ClientProxy) serveForward(conn net.Conn, target string) {
	defer conn.Close()
	ch, err := p.open(ProtoTCP, target)
	if err != nil {
		return
	}
	defer p.close(ch, true)
	p.pump(ch, conn)
}

// open 分配通道并向被控端请求拨号，等待结果。
func (p *ClientProxy) open(pr string, target string) (*clientChan, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrProxyClosed
	}
	ch := &clientChan{
		id:   p.nextID,
		recv: make(chan []byte, recvQueue),
		ack:  make(chan bool, 1),
		dead: make(chan struct{}),
	}
	p.nextID++
	p.chans[ch.id] = ch
	p.mu.Unlock()
	p.active.Add(1)

	body, err := proto.EncodeProxyOpen(&proto.ProxyOpen{ID: ch.id, Addr: target, Proto: pr})
	if err != nil {
		p.close(ch, false)
		return nil, err
	}
	if err := p.send(proto.MsgProxyOpen, body); err != nil {
		p.close(ch, false)
		return nil, err
	}
	select {
	case ok := <-ch.ack:
		if !ok {
			p.close(ch, false)
			return nil, errors.New("lanrd/proxy: agent dial failed")
		}
		return ch, nil
	case <-ch.dead:
		return nil, ErrProxyClosed
	case <-time.After(ackTimeout):
		p.close(ch, true)
		return nil, errors.New("lanrd/proxy: open timeout")
	}
}

// pump TCP 双向搬运: 本地连接 -> 通道(独立协程)，通道 -> 本地连接(当前协程)。
func (p *ClientProxy) pump(ch *clientChan, conn net.Conn) {
	go func() {
		buf := make([]byte, chunkSize)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if e := p.send(proto.MsgProxyData, proto.EncodeProxyData(ch.id, buf[:n])); e != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		p.close(ch, true)
	}()
	for {
		select {
		case data := <-ch.recv:
			if _, err := conn.Write(data); err != nil {
				return
			}
		case <-ch.dead:
			return
		}
	}
}

// enqueue 将被控端回传数据放入通道队列，队列满或通道已死则关闭通道。
func (p *ClientProxy) enqueue(id uint32, data []byte) {
	ch := p.lookup(id)
	if ch == nil {
		return
	}
	select {
	case ch.recv <- data:
	case <-ch.dead:
	default: // 目标读取过慢，关闭通道防止内存膨胀
		p.close(ch, true)
	}
}

// lookup 查找通道。
func (p *ClientProxy) lookup(id uint32) *clientChan {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chans[id]
}

// close 关闭通道: 关信号、摘除注册、按需通知被控端。
func (p *ClientProxy) close(ch *clientChan, notify bool) {
	p.kill(ch)
	if notify {
		p.send(proto.MsgProxyClose, proto.EncodeProxyClose(ch.id))
	}
}

// kill 关闭通道并摘除注册(幂等)。
func (p *ClientProxy) kill(ch *clientChan) {
	ch.closeOnce.Do(func() {
		p.mu.Lock()
		if cur, ok := p.chans[ch.id]; ok && cur == ch {
			delete(p.chans, ch.id)
			p.active.Add(-1)
		}
		p.mu.Unlock()
		close(ch.dead)
	})
}
