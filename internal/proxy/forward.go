package proxy

import (
	"fmt"
	"strings"

	"lanrd/internal/proto"
)

// 支持的转发协议。
const (
	ProtoTCP = "tcp"
	ProtoUDP = "udp"
)

// ForwardRule 一条静态端口转发规则:
// 本机 Local 监听，连接/数据报经被控端转发到其内网 Remote。
type ForwardRule struct {
	Proto  string // "tcp" / "udp"
	Local  string // 本地监听地址，如 127.0.0.1:33890
	Remote string // 被控端内网目标，如 192.168.1.8:3389
}

// String 返回规则的显示格式。
func (r ForwardRule) String() string {
	return fmt.Sprintf("%s:%s=%s", r.Proto, r.Local, r.Remote)
}

// ParseForward 解析转发规则字符串，格式:
//
//	tcp:127.0.0.1:33890=192.168.1.8:3389
//	udp:127.0.0.1:5353=192.168.1.1:53
//
// 本地地址可省略主机部分，如 "tcp:33890=192.168.1.8:3389" 视为 127.0.0.1:33890。
func ParseForward(s string) (ForwardRule, error) {
	left, remote, ok := strings.Cut(s, "=")
	if !ok {
		return ForwardRule{}, fmt.Errorf("lanrd/proxy: 规则缺少 '=': %q", s)
	}
	pr, local, ok := strings.Cut(left, ":")
	if !ok {
		return ForwardRule{}, fmt.Errorf("lanrd/proxy: 规则缺少协议前缀: %q", s)
	}
	pr = strings.ToLower(strings.TrimSpace(pr))
	if pr != ProtoTCP && pr != ProtoUDP {
		return ForwardRule{}, fmt.Errorf("lanrd/proxy: 协议仅支持 tcp/udp: %q", pr)
	}
	if !strings.Contains(local, ":") {
		local = "127.0.0.1:" + local
	}
	r := ForwardRule{Proto: pr, Local: local, Remote: remote}
	if err := checkPort(r.Local); err != nil {
		return ForwardRule{}, fmt.Errorf("lanrd/proxy: 本地地址无效 %q: %w", r.Local, err)
	}
	if err := checkPort(r.Remote); err != nil {
		return ForwardRule{}, fmt.Errorf("lanrd/proxy: 远端地址无效 %q: %w", r.Remote, err)
	}
	return r, nil
}

// checkPort 校验 host:port 且端口在 1~65535。
func checkPort(addr string) error {
	_, p, err := proto.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if p == 0 {
		return fmt.Errorf("端口不能为 0")
	}
	return nil
}
