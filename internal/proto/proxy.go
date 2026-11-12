// 内网代理扩展消息：控制端在本机开启静态端口映射，目标连接经
// 远程控制通道复用转发，由被控端在其所在内网发起真实连接。
// 帧格式与 docs/protocol.md 一致，此文件为纯增量，不影响既有消息。
package proto

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"strconv"
)

// 代理消息类型。0x01~0x2F 为控制端->被控端，0x80~0xAF 为被控端->控制端。
const (
	MsgProxyOpen   Type = 0x15 // C->S 请求打开代理通道(JSON ProxyOpen)
	MsgProxyData   Type = 0x16 // C->S 代理通道数据
	MsgProxyClose  Type = 0x17 // C->S 关闭代理通道
	MsgProxyAck    Type = 0x91 // S->C 打开结果
	MsgProxyDataS  Type = 0x92 // S->C 代理通道数据
	MsgProxyCloseS Type = 0x93 // S->C 关闭代理通道
)

// ProxyOpen 打开代理通道请求。
type ProxyOpen struct {
	ID    uint32 `json:"id"`              // 通道号，控制端分配
	Addr  string `json:"addr"`            // 目标地址 host:port，由被控端内网发起连接
	Proto string `json:"proto,omitempty"` // 传输协议 "tcp"(默认)/"udp"
}

// EncodeProxyOpen 编码打开代理通道请求。
func EncodeProxyOpen(p *ProxyOpen) ([]byte, error) { return json.Marshal(p) }

// DecodeProxyOpen 解码打开代理通道请求。
func DecodeProxyOpen(b []byte) (*ProxyOpen, error) {
	p := &ProxyOpen{}
	if err := json.Unmarshal(b, p); err != nil {
		return nil, err
	}
	return p, nil
}

// EncodeProxyData 编码代理通道数据: [u32 id][data]。data 为空时仍合法(视作零长度)。
func EncodeProxyData(id uint32, data []byte) []byte {
	b := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(b[0:4], id)
	copy(b[4:], data)
	return b
}

// DecodeProxyData 解码代理通道数据，返回通道号与数据。
func DecodeProxyData(b []byte) (id uint32, data []byte, err error) {
	if len(b) < 4 {
		return 0, nil, ErrShortPayload
	}
	return binary.LittleEndian.Uint32(b[0:4]), b[4:], nil
}

// EncodeProxyClose 编码关闭代理通道: [u32 id]。
func EncodeProxyClose(id uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b[0:4], id)
	return b
}

// DecodeProxyClose 解码关闭代理通道。
func DecodeProxyClose(b []byte) (id uint32, err error) {
	if len(b) < 4 {
		return 0, ErrShortPayload
	}
	return binary.LittleEndian.Uint32(b[0:4]), nil
}

// EncodeProxyAck 编码打开结果: [u32 id][u8 ok]。
func EncodeProxyAck(id uint32, ok bool) []byte {
	b := make([]byte, 5)
	binary.LittleEndian.PutUint32(b[0:4], id)
	if ok {
		b[4] = 1
	}
	return b
}

// DecodeProxyAck 解码打开结果。
func DecodeProxyAck(b []byte) (id uint32, ok bool, err error) {
	if len(b) < 5 {
		return 0, false, ErrShortPayload
	}
	return binary.LittleEndian.Uint32(b[0:4]), b[4] == 1, nil
}

// SplitHostPort 拆分 host:port，port 非法时返回错误。代理通道统一使用该入口校验。
func SplitHostPort(addr string) (string, uint16, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	n, err := strconv.ParseUint(p, 10, 16)
	if err != nil {
		return "", 0, err
	}
	return h, uint16(n), nil
}
