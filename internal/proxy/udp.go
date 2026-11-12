package proxy

import (
	"encoding/binary"
	"net"
	"sync"
	"time"

	"lanrd/internal/proto"
)

// UDP 转发参数。
const (
	udpMaxDatagram = 65535            // 单个数据报上限(u16 长度前缀可表示)
	udpIdleTimeout = 60 * time.Second // 对端无流量的会话回收时间
)

// udpFrame 给数据报加 u16 长度前缀，使 UDP 报文在字节流通道内定界。
func udpFrame(dgram []byte) []byte {
	b := make([]byte, 2+len(dgram))
	binary.LittleEndian.PutUint16(b[0:2], uint16(len(dgram)))
	copy(b[2:], dgram)
	return b
}

// frameReader 从字节流中拆出完整数据报。
type frameReader struct{ buf []byte }

// feed 追加数据并返回其中完整的数据报(切片引用内部缓冲，消费后方可再次 feed)。
func (f *frameReader) feed(b []byte) [][]byte {
	f.buf = append(f.buf, b...)
	var out [][]byte
	for len(f.buf) >= 2 {
		n := int(binary.LittleEndian.Uint16(f.buf[0:2]))
		if n == 0 || len(f.buf) < 2+n {
			break
		}
		out = append(out, f.buf[2:2+n])
		f.buf = f.buf[2+n:]
	}
	return out
}

// udpPeer 一个 UDP 会话: 本地对端地址与通道一一对应。
type udpPeer struct {
	ch   *clientChan
	addr *net.UDPAddr
	last time.Time
}

// serveUDPForward UDP 静态转发: 同一本地对端地址复用一条通道，
// 数据报经 u16 长度前缀在通道内定界，空闲超过 udpIdleTimeout 回收。
func (p *ClientProxy) serveUDPForward(pc *net.UDPConn, target string) {
	var pmu sync.Mutex
	peers := make(map[string]*udpPeer)
	go p.sweepUDPPeers(pc, &pmu, peers)

	buf := make([]byte, udpMaxDatagram)
	for {
		n, addr, err := pc.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pmu.Lock()
		pr := peers[addr.String()]
		if pr == nil {
			ch, oerr := p.open(ProtoUDP, target)
			if oerr != nil {
				pmu.Unlock()
				continue
			}
			pr = &udpPeer{ch: ch, addr: addr, last: time.Now()}
			peers[addr.String()] = pr
			go p.udpPump(pc, pr)
		}
		pr.last = time.Now()
		ch := pr.ch
		pmu.Unlock()
		if err := p.send(proto.MsgProxyData, proto.EncodeProxyData(ch.id, udpFrame(buf[:n]))); err != nil {
			return
		}
	}
}

// sweepUDPPeers 周期回收空闲 UDP 会话。
func (p *ClientProxy) sweepUDPPeers(pc *net.UDPConn, pmu *sync.Mutex, peers map[string]*udpPeer) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		if closed := func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return p.closed
		}(); closed {
			return
		}
		now := time.Now()
		pmu.Lock()
		for k, pr := range peers {
			if now.Sub(pr.last) > udpIdleTimeout {
				delete(peers, k)
				go p.close(pr.ch, true)
			}
		}
		pmu.Unlock()
	}
}

// udpPump 通道 -> 本地对端: 拆数据报帧并回写。
func (p *ClientProxy) udpPump(pc *net.UDPConn, pr *udpPeer) {
	var fr frameReader
	for {
		select {
		case data := <-pr.ch.recv:
			for _, dgram := range fr.feed(data) {
				if _, err := pc.WriteToUDP(dgram, pr.addr); err != nil {
					p.close(pr.ch, true)
					return
				}
			}
		case <-pr.ch.dead:
			return
		}
	}
}
