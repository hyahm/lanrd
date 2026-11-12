// Package client 实现控制端：连接被控端、解码画面、捕获本地键鼠并转发。
package client

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"lanrd/internal/proto"
	"lanrd/internal/proxy"
)

// Status 被控端广播的抢占状态。
type Status struct {
	Last   uint8 // 最近输入来源
	Since  uint64
	Locked bool
}

// dirtyRect 已更新待上屏的区域。
type dirtyRect struct{ x, y, w, h int }

// Client 控制端网络会话。
type Client struct {
	conn  net.Conn
	token string

	mu       sync.Mutex
	canvas   *image.RGBA // 远端整屏画布(BGRA 即 RGBA)
	dirty    []dirtyRect
	status   Status
	closed   bool
	closeErr error

	rtt atomic.Int64
	wmu sync.Mutex // 写锁，串行化输入消息

	host string // 被控端主机名(握手应答)

	tiles      chan []byte // JPEG 分块解码任务队列
	decodeDone chan struct{}

	proxySrv *proxy.ClientProxy // 内网代理(EnableProxy 按需创建)
}

// decodeWorkers 并行解码工作协程数。
const decodeWorkers = 4

// Dial 连接被控端并完成握手，成功后启动读循环。
func Dial(addr, token string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, token: token}
	if err := c.handshake(timeout); err != nil {
		conn.Close()
		return nil, err
	}
	c.tiles = make(chan []byte, 256)
	c.decodeDone = make(chan struct{})
	for i := 0; i < decodeWorkers; i++ {
		go c.decodeWorker()
	}
	go c.readLoop()
	go c.pingLoop()
	return c, nil
}

// decodeWorker 并行解码分块并写入画布。
func (c *Client) decodeWorker() {
	for {
		select {
		case <-c.decodeDone:
			return
		case body := <-c.tiles:
			c.applyTile(body)
		}
	}
}

// handshake 完成握手并初始化画布。
func (c *Client) handshake(timeout time.Duration) error {
	c.conn.SetDeadline(time.Now().Add(timeout))
	hb, _ := proto.EncodeHello(&proto.Hello{Version: proto.ProtocolVersion, Token: c.token})
	if err := proto.Write(c.conn, proto.MsgHello, hb); err != nil {
		return err
	}
	m, err := proto.Read(c.conn)
	if err != nil {
		return err
	}
	if m.Type != proto.MsgHelloAck {
		return fmt.Errorf("lanrd/client: 握手应答类型错误: 0x%02X", m.Type)
	}
	ack, err := proto.DecodeHelloAck(m.Body)
	if err != nil {
		return err
	}
	if ack.Version != proto.ProtocolVersion {
		return fmt.Errorf("lanrd/client: 协议版本不匹配: %d", ack.Version)
	}
	c.conn.SetDeadline(time.Time{})
	c.mu.Lock()
	c.canvas = image.NewRGBA(image.Rect(0, 0, ack.W, ack.H))
	c.mu.Unlock()
	c.host = ack.Host
	return nil
}

// Host 返回被控端主机名。
func (c *Client) Host() string { return c.host }

// Size 返回远端屏幕尺寸，未就绪返回 0。
func (c *Client) Size() (w, h int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.canvas == nil {
		return 0, 0
	}
	return c.canvas.Bounds().Dx(), c.canvas.Bounds().Dy()
}

// Canvas 返回整屏画布与脏区列表，并清空脏区。
func (c *Client) TakeDirty() (*image.RGBA, []dirtyRect) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.dirty
	c.dirty = nil
	return c.canvas, d
}

// Status 返回最新抢占状态。
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// RTT 返回最近一次心跳往返毫秒。
func (c *Client) RTT() int64 { return c.rtt.Load() }

// Closed 返回连接是否断开及原因。
func (c *Client) Closed() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed, c.closeErr
}

// Close 主动断开。
func (c *Client) Close() { c.fail(nil) }

// fail 标记连接失败并关闭，同时停止解码协程。
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.closeErr = err
	c.mu.Unlock()
	c.conn.Close()
	if c.decodeDone != nil {
		close(c.decodeDone)
	}
}

// readLoop 接收并分发被控端消息。
func (c *Client) readLoop() {
	for {
		m, err := proto.Read(c.conn)
		if err != nil {
			c.fail(err)
			return
		}
		switch m.Type {
		case proto.MsgTile:
			select {
			case c.tiles <- m.Body:
			case <-c.decodeDone:
				return
			}
		case proto.MsgHelloAck:
			c.resize(m.Body)
		case proto.MsgStatus:
			c.applyStatus(m.Body)
		case proto.MsgPong:
			ts, _ := proto.DecodePing(m.Body)
			c.rtt.Store(time.Now().UnixMilli() - int64(ts))
		case proto.MsgProxyAck, proto.MsgProxyDataS, proto.MsgProxyCloseS:
			c.dispatchProxy(m)
		}
	}
}

// applyTile 解码 JPEG 分块写入画布。
func (c *Client) applyTile(body []byte) {
	x, y, w, h, data, err := proto.DecodeTile(body)
	if err != nil {
		return
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.canvas == nil {
		return
	}
	rect := image.Rect(int(x), int(y), int(x)+int(w), int(y)+int(h))
	if !rect.In(c.canvas.Rect) {
		return
	}
	draw.Draw(c.canvas, rect, img, image.Point{}, draw.Src)
	c.dirty = append(c.dirty, dirtyRect{int(x), int(y), int(w), int(h)})
}

// resize 处理握手应答/分辨率变化，必要时重建画布。
func (c *Client) resize(body []byte) {
	ack, err := proto.DecodeHelloAck(body)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.canvas != nil && c.canvas.Bounds().Dx() == ack.W && c.canvas.Bounds().Dy() == ack.H {
		return
	}
	c.canvas = image.NewRGBA(image.Rect(0, 0, ack.W, ack.H))
	c.dirty = nil
}

// applyStatus 更新抢占状态。
func (c *Client) applyStatus(body []byte) {
	last, since, locked, err := proto.DecodeStatus(body)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.status = Status{Last: last, Since: since, Locked: locked}
	c.mu.Unlock()
}

// pingLoop 周期心跳测延迟。
func (c *Client) pingLoop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for range t.C {
		if closed, _ := c.Closed(); closed {
			return
		}
		c.Send(proto.MsgPing, proto.EncodePing(uint64(time.Now().UnixMilli())))
	}
}

// Send 发送一条消息(带写锁)。
func (c *Client) Send(t proto.Type, body []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return proto.Write(c.conn, t, body)
}

// SendKey 发送键盘事件。
func (c *Client) SendKey(scan uint16, down bool) error {
	return c.Send(proto.MsgKey, proto.EncodeKey(scan, down))
}

// SendMouseMove 发送鼠标移动。
func (c *Client) SendMouseMove(x, y uint16) error {
	return c.Send(proto.MsgMouseMove, proto.EncodeMouseMove(x, y))
}

// SendMouseBtn 发送鼠标按键。
func (c *Client) SendMouseBtn(btn uint8, down bool, x, y uint16) error {
	return c.Send(proto.MsgMouseBtn, proto.EncodeMouseBtn(btn, down, x, y))
}

// SendWheel 发送滚轮。
func (c *Client) SendWheel(delta int32, x, y uint16) error {
	return c.Send(proto.MsgWheel, proto.EncodeWheel(delta, x, y))
}

// SendInputLock 锁定/解锁远端物理键鼠。
func (c *Client) SendInputLock(on bool) error {
	return c.Send(proto.MsgInputLock, proto.EncodeInputLock(on))
}
