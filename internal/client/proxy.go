package client

import (
	"fmt"
	"time"

	"lanrd/internal/proto"
	"lanrd/internal/proxy"
)

// EnableProxy 启用内网代理: 按 rules 启动静态端口映射
// (本机端口 -> 被控端内网地址)，流量经当前控制连接由被控端转发，
// 连接断开后自动停止。
func (c *Client) EnableProxy(rules []proxy.ForwardRule) error {
	c.mu.Lock()
	if c.proxySrv != nil {
		c.mu.Unlock()
		return fmt.Errorf("lanrd/client: 代理已启动")
	}
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("lanrd/client: 连接已断开")
	}
	c.proxySrv = proxy.NewClientProxy(c.Send, nil)
	srv := c.proxySrv
	c.mu.Unlock()

	if len(rules) > 0 {
		if err := srv.StartForward(rules); err != nil {
			c.stopProxy(srv)
			return err
		}
	}
	go c.watchDead(srv)
	return nil
}

// stopProxy 停止并摘除代理服务。
func (c *Client) stopProxy(srv *proxy.ClientProxy) {
	srv.Stop()
	c.mu.Lock()
	if c.proxySrv == srv {
		c.proxySrv = nil
	}
	c.mu.Unlock()
}

// ProxySummary 返回代理状态摘要，未启用返回空串(用于 HUD 展示)。
func (c *Client) ProxySummary() string {
	c.mu.Lock()
	p := c.proxySrv
	c.mu.Unlock()
	if p == nil {
		return ""
	}
	return p.Summary()
}

// watchDead 连接断开后停止代理(轮询检测，避免侵入 Client 关闭流程)。
func (c *Client) watchDead(p *proxy.ClientProxy) {
	for {
		time.Sleep(300 * time.Millisecond)
		if closed, _ := c.Closed(); closed {
			p.Stop()
			return
		}
	}
}

// dispatchProxy 把被控端回传的代理消息交给代理服务(未启用时忽略)。
func (c *Client) dispatchProxy(m *proto.Message) {
	c.mu.Lock()
	p := c.proxySrv
	c.mu.Unlock()
	if p != nil {
		p.HandleAgent(m)
	}
}
