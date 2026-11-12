//go:build windows

package agent

import (
	"lanrd/internal/proto"
	"lanrd/internal/proxy"
)

// proxyMsg 分发控制端发来的代理通道消息，按需为会话创建代理管理器。
// 配置 proxy.enabled=false 时由 AgentProxy 统一拒绝新通道。
func (a *Agent) proxyMsg(s *session, m *proto.Message) {
	s.pxMu.Lock()
	if s.px == nil {
		s.px = proxy.NewAgentProxy(
			func(t proto.Type, body []byte) bool {
				return a.send(s, &proto.Message{Type: t, Body: body})
			},
			a.cfg.Proxy.Enabled, a.log)
	}
	px := s.px
	s.pxMu.Unlock()
	px.HandleClient(m)
}

// takeProxy 取出会话的代理管理器(可为 nil)。
func (s *session) takeProxy() *proxy.AgentProxy {
	s.pxMu.Lock()
	defer s.pxMu.Unlock()
	return s.px
}
