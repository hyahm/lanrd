package proxy

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"lanrd/internal/proto"
)

// dialTimeout 被控端拨号目标地址的超时。
const dialTimeout = 10 * time.Second

// AgentProxy 被控端内网代理：接收控制端的通道打开请求，
// 在被控端所在网络拨号目标地址并双向转发(TCP 流式 / UDP 数据报定界)。
type AgentProxy struct {
	send    func(t proto.Type, body []byte) bool
	dial    func(ctx context.Context, network, addr string) (net.Conn, error)
	enabled bool
	log     *slog.Logger

	mu     sync.Mutex
	chans  map[uint32]*agentChan
	closed bool
}

// agentChan 单条代理通道的被控端状态。
type agentChan struct {
	id        uint32
	conn      net.Conn
	proto     string
	wr        chan []byte // 控制端回传数据队列
	dead      chan struct{}
	closeOnce sync.Once
}

// NewAgentProxy 创建被控端代理。enabled 为 false 时拒绝新通道
// (已有消息不受影响)。send 用于向控制端发送协议消息。
func NewAgentProxy(send func(t proto.Type, body []byte) bool, enabled bool, log *slog.Logger) *AgentProxy {
	if log == nil {
		log = slog.Default()
	}
	return &AgentProxy{
		send:    send,
		dial:    defaultDial,
		enabled: enabled,
		log:     log,
		chans:   make(map[uint32]*agentChan),
	}
}

// defaultDial 默认拨号实现。
func defaultDial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// HandleClient 处理控制端发来的代理消息(OPEN/DATA/CLOSE)。
// DATA/CLOSE 在会话读协程中调用，入队满时关闭通道而非阻塞。
func (p *AgentProxy) HandleClient(m *proto.Message) {
	switch m.Type {
	case proto.MsgProxyOpen:
		if req, err := proto.DecodeProxyOpen(m.Body); err == nil {
			go p.openChan(req.ID, req.Addr, req.Proto)
		}
	case proto.MsgProxyData:
		if id, data, err := proto.DecodeProxyData(m.Body); err == nil {
			p.enqueue(id, data)
		}
	case proto.MsgProxyClose:
		if id, err := proto.DecodeProxyClose(m.Body); err == nil {
			if ch := p.lookup(id); ch != nil {
				p.close(ch, false)
			}
		}
	}
}

// Close 关闭全部通道(会话结束时调用)。
func (p *AgentProxy) Close() {
	p.mu.Lock()
	p.closed = true
	chans := p.chans
	p.chans = make(map[uint32]*agentChan)
	p.mu.Unlock()
	for _, ch := range chans {
		p.kill(ch)
		ch.conn.Close()
	}
}

// openChan 在被控端网络拨号并应答结果，成功后启动双向搬运。
func (p *AgentProxy) openChan(id uint32, addr, pr string) {
	if !p.enabled {
		p.send(proto.MsgProxyAck, proto.EncodeProxyAck(id, false))
		return
	}
	if pr == "" {
		pr = ProtoTCP
	}
	if pr != ProtoTCP && pr != ProtoUDP {
		p.send(proto.MsgProxyAck, proto.EncodeProxyAck(id, false))
		return
	}
	if _, _, err := proto.SplitHostPort(addr); err != nil {
		p.send(proto.MsgProxyAck, proto.EncodeProxyAck(id, false))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := p.dial(ctx, pr, addr)
	if err != nil {
		p.log.Info("代理拨号失败", "target", addr, "proto", pr, "err", err)
		p.send(proto.MsgProxyAck, proto.EncodeProxyAck(id, false))
		return
	}
	ch := &agentChan{
		id: id, conn: conn, proto: pr,
		wr:   make(chan []byte, recvQueue),
		dead: make(chan struct{}),
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		conn.Close()
		p.send(proto.MsgProxyAck, proto.EncodeProxyAck(id, false))
		return
	}
	if _, exists := p.chans[id]; exists { // 通道号冲突，拒绝
		p.mu.Unlock()
		conn.Close()
		p.send(proto.MsgProxyAck, proto.EncodeProxyAck(id, false))
		return
	}
	p.chans[id] = ch
	p.mu.Unlock()
	if !p.send(proto.MsgProxyAck, proto.EncodeProxyAck(id, true)) {
		p.kill(ch)
		conn.Close()
		return
	}
	p.log.Info("代理通道打开", "target", addr, "proto", pr, "id", id)
	go p.upstream(ch)
	go p.downstream(ch)
}

// upstream 目标连接 -> 控制端。
func (p *AgentProxy) upstream(ch *agentChan) {
	defer p.close(ch, true)
	buf := make([]byte, readSize(ch.proto))
	for {
		n, err := ch.conn.Read(buf)
		if n > 0 {
			data := buf[:n]
			if ch.proto == ProtoUDP {
				data = udpFrame(data) // UDP: 按数据报定界
			}
			if !p.send(proto.MsgProxyDataS, proto.EncodeProxyData(ch.id, data)) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// downstream 控制端 -> 目标连接。
func (p *AgentProxy) downstream(ch *agentChan) {
	var fr frameReader // UDP: 拆数据报帧
	for {
		select {
		case data := <-ch.wr:
			if ch.proto == ProtoUDP {
				for _, dgram := range fr.feed(data) {
					if _, err := ch.conn.Write(dgram); err != nil {
						p.close(ch, true)
						return
					}
				}
				continue
			}
			if _, err := ch.conn.Write(data); err != nil {
				p.close(ch, true)
				return
			}
		case <-ch.dead:
			return
		}
	}
}

// readSize 按协议返回单次读取缓冲大小(UDP 需容纳最大数据报)。
func readSize(pr string) int {
	if pr == ProtoUDP {
		return udpMaxDatagram
	}
	return chunkSize
}

// enqueue 将控制端数据放入写队列，队列满或通道已死则关闭通道。
func (p *AgentProxy) enqueue(id uint32, data []byte) {
	ch := p.lookup(id)
	if ch == nil {
		return
	}
	select {
	case ch.wr <- data:
	case <-ch.dead:
	default: // 目标写入过慢，关闭通道防止内存膨胀
		p.close(ch, true)
	}
}

// lookup 查找通道。
func (p *AgentProxy) lookup(id uint32) *agentChan {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chans[id]
}

// close 关闭通道: 关信号、摘除注册、断开目标连接，按需通知控制端。
func (p *AgentProxy) close(ch *agentChan, notify bool) {
	p.kill(ch)
	ch.conn.Close()
	if notify {
		p.send(proto.MsgProxyCloseS, proto.EncodeProxyClose(ch.id))
	}
}

// kill 关闭通道并摘除注册(幂等)。
func (p *AgentProxy) kill(ch *agentChan) {
	ch.closeOnce.Do(func() {
		p.mu.Lock()
		if cur, ok := p.chans[ch.id]; ok && cur == ch {
			delete(p.chans, ch.id)
		}
		p.mu.Unlock()
		close(ch.dead)
	})
}
