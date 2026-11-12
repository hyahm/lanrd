// 直连端口转发: 本机按规则监听，接受连接/数据报后直接拨号规则目标
// (不经被控端转发)，双向搬运数据。适合本机可直接访问目标的场景，
// 与经控制连接转发的 ClientProxy 互为补充。
package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// DirectForwarder 直连端口转发器: 每条规则一个监听器，
// 连接/数据报直达目标地址。
type DirectForwarder struct {
	log *slog.Logger

	mu    sync.Mutex
	rules []ForwardRule // 已启动规则，Local 为实际监听地址
	stops []func()
	wg    sync.WaitGroup
}

// StartDirect 按 rules 启动直连转发，任一规则监听失败即整体失败并回收
// 已启动的监听。规则 Local 端口为 0 时由内核分配，可用 Local 回读。
func StartDirect(rules []ForwardRule, log *slog.Logger) (*DirectForwarder, error) {
	if log == nil {
		log = slog.Default()
	}
	d := &DirectForwarder{log: log}
	for i, r := range rules {
		var stop func()
		var local string
		var err error
		switch r.Proto {
		case ProtoTCP:
			stop, local, err = d.listenTCP(r)
		case ProtoUDP:
			stop, local, err = d.listenUDP(r)
		default:
			err = fmt.Errorf("不支持的协议 %q", r.Proto)
		}
		if err != nil {
			d.Stop()
			return nil, fmt.Errorf("lanrd/proxy: 规则[%d] %s: %w", i, r, err)
		}
		r.Local = local
		d.mu.Lock()
		d.rules = append(d.rules, r)
		d.stops = append(d.stops, stop)
		d.mu.Unlock()
	}
	return d, nil
}

// Local 返回第 i 条规则的实际监听地址，越界返回空串。
func (d *DirectForwarder) Local(i int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if i < 0 || i >= len(d.rules) {
		return ""
	}
	return d.rules[i].Local
}

// RunDirect 启动直连转发并阻塞，直到所有监听器退出。
func RunDirect(rules []ForwardRule) error {
	d, err := StartDirect(rules, nil)
	if err != nil {
		return err
	}
	d.wg.Wait()
	return nil
}

// Stop 关闭全部监听器(活跃连接随进程/拨号对端关闭而结束)。
func (d *DirectForwarder) Stop() {
	d.mu.Lock()
	stops := d.stops
	d.stops = nil
	d.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

// listenTCP 监听单条 TCP 规则，返回清理函数与实际监听地址。
func (d *DirectForwarder) listenTCP(r ForwardRule) (func(), string, error) {
	ln, err := net.Listen("tcp", r.Local)
	if err != nil {
		return nil, "", err
	}
	d.log.Info("直连映射已启动", "rule", r.String())
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				d.pipe(conn, r.Remote)
			}()
		}
	}()
	return func() { ln.Close() }, ln.Addr().String(), nil
}

// pipe 拨号目标并与本地连接对拷，任一方向结束即断开。
func (d *DirectForwarder) pipe(local net.Conn, target string) {
	defer local.Close()
	remote, err := net.Dial("tcp", target)
	if err != nil {
		d.log.Warn("直连拨号失败", "target", target, "err", err)
		return
	}
	defer remote.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(remote, local); done <- struct{}{} }()
	go func() { io.Copy(local, remote); done <- struct{}{} }()
	<-done
}

// listenUDP 监听单条 UDP 规则，返回清理函数与实际监听地址。
func (d *DirectForwarder) listenUDP(r ForwardRule) (func(), string, error) {
	ua, err := net.ResolveUDPAddr("udp", r.Local)
	if err != nil {
		return nil, "", err
	}
	pc, err := net.ListenUDP("udp", ua)
	if err != nil {
		return nil, "", err
	}
	d.log.Info("直连映射已启动", "rule", r.String())
	d.wg.Add(1)
	go d.serveUDP(pc, r.Remote)
	return func() { pc.Close() }, pc.LocalAddr().String(), nil
}

// directUDPPeer 一个 UDP 会话: 本地对端地址 <-> 到目标的数据报 socket。
type directUDPPeer struct {
	pc   *net.UDPConn // 到目标的连接
	addr *net.UDPAddr // 本地对端
	last time.Time
}

// serveUDP 直连 UDP 转发: 同一本地对端地址复用一个到目标的 socket，
// 空闲超过 udpIdleTimeout 回收。
func (d *DirectForwarder) serveUDP(pc *net.UDPConn, target string) {
	ta, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		d.log.Warn("直连目标无效", "target", target, "err", err)
		return
	}
	var pmu sync.Mutex
	peers := make(map[string]*directUDPPeer)
	stop := make(chan struct{})
	defer close(stop)
	go d.sweepUDP(pc, &pmu, peers, stop)

	buf := make([]byte, udpMaxDatagram)
	for {
		n, addr, err := pc.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pmu.Lock()
		pr := peers[addr.String()]
		if pr == nil {
			rc, derr := net.DialUDP("udp", nil, ta)
			if derr != nil {
				pmu.Unlock()
				continue
			}
			pr = &directUDPPeer{pc: rc, addr: addr, last: time.Now()}
			peers[addr.String()] = pr
			go d.udpBack(pc, pr, &pmu)
		}
		pr.last = time.Now()
		uplink := pr.pc
		pmu.Unlock()
		if _, err := uplink.Write(buf[:n]); err != nil {
			return
		}
	}
}

// udpBack 目标 -> 本地对端: 回写响应数据报。
func (d *DirectForwarder) udpBack(pc *net.UDPConn, pr *directUDPPeer, pmu *sync.Mutex) {
	buf := make([]byte, udpMaxDatagram)
	for {
		n, _, err := pr.pc.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pmu.Lock()
		pr.last = time.Now()
		pmu.Unlock()
		if _, err := pc.WriteToUDP(buf[:n], pr.addr); err != nil {
			return
		}
	}
}

// sweepUDP 周期回收空闲 UDP 会话。
func (d *DirectForwarder) sweepUDP(pc *net.UDPConn, pmu *sync.Mutex, peers map[string]*directUDPPeer, stop <-chan struct{}) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			now := time.Now()
			pmu.Lock()
			for k, pr := range peers {
				if now.Sub(pr.last) > udpIdleTimeout {
					delete(peers, k)
					pr.pc.Close()
				}
			}
			pmu.Unlock()
		}
	}
}
