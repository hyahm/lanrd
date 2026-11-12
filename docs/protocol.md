# lanrd 协议说明

控制端(C)与被控端(S)之间使用 TCP 长连接，二进制消息格式：

```
+--------+------------------+----------------+
| type 1B | length 4B (LE)  | payload lengthB |
+--------+------------------+----------------+
```

- 多字节整数均为小端序
- 单条消息负载上限 4MB
- 心跳: C 周期发送 `PING`(携带毫秒时间戳)，S 原样回 `PONG`，用于测量 RTT

## 消息类型

| 类型 | 值 | 方向 | 负载格式 |
|------|----|------|----------|
| HELLO | 0x01 | C->S | JSON `{version, token, name}` |
| HELLO_ACK | 0x81 | S->C | JSON `{version, host, w, h, tile}`，分辨率变化时重发 |
| MOUSE_MOVE | 0x10 | C->S | `[u16 x][u16 y]` 远端虚拟桌面像素坐标 |
| MOUSE_BTN | 0x11 | C->S | `[u8 button][u8 down][u16 x][u16 y]` button: 0左键 1右键 2中键 |
| WHEEL | 0x12 | C->S | `[i32 delta][u16 x][u16 y]` delta 单位 1/120 槽位，正值向上 |
| KEY | 0x13 | C->S | `[u16 scancode][u8 down]` scancode 为 Windows set-1 扫描码，0x0100 位表示扩展键(E0) |
| INPUT_LOCK | 0x14 | C->S | `[u8 on]` 锁定(屏蔽)远端物理键鼠 |
| PROXY_OPEN | 0x15 | C->S | JSON `{id, addr, proto}` 请求被控端在其实际网络拨号 addr 并建立代理通道；proto 为 `tcp`(默认)/`udp` |
| PROXY_DATA | 0x16 | C->S | `[u32 id][data]` 通道上行数据(控制端 -> 被控端 -> 目标) |
| PROXY_CLOSE | 0x17 | C->S | `[u32 id]` 关闭通道(本地连接结束或队列溢出) |
| TILE | 0x82 | S->C | `[u16 x][u16 y][u16 w][u16 h][jpeg]` 图像分块 |
| PROXY_ACK | 0x91 | S->C | `[u32 id][u8 ok]` 拨号结果；被控端配置 `proxy.enabled=false` 时恒为失败 |
| PROXY_DATA2 | 0x92 | S->C | `[u32 id][data]` 通道下行数据(目标 -> 被控端 -> 控制端) |
| PROXY_CLOSE2 | 0x93 | S->C | `[u32 id]` 被控端侧通道关闭(拨号断开等) |
| STATUS | 0x90 | S->C | `[u8 lastInput][u64 sinceUnixMs][u8 locked]` lastInput: 0无 1远端物理用户 2控制端 |
| PING | 0x2F | C->S | `[u64 tsUnixMs]` |
| PONG | 0x8F | S->C | `[u64 tsUnixMs]` 原样回显 |

## 连接流程

1. C 建立 TCP 连接，发送 `HELLO`
2. S 校验版本与口令，通过后发送 `HELLO_ACK`(含屏幕尺寸与分块大小)，随后推送全量分块(关键帧)
3. S 以配置帧率持续抓屏，仅推送脏分块；每 5 秒强制一次关键帧
4. C 转发键鼠事件，S 通过 `SendInput` 注入
5. S 每 500ms 广播 `STATUS`，双方可感知对方正在操作(抢占提示)
6. 任一侧断开，会话结束；新连接会顶替旧会话

## 抢占语义

- 默认双方输入同时有效：控制端事件被注入，远端物理键鼠天然有效，Windows 自动合并
- 远端通过低级钩子(WH_KEYBOARD_LL / WH_MOUSE_LL，过滤注入事件)感知物理输入，
  并在 `STATUS.lastInput=1` 时告知控制端"对方正在操作"
- 控制端 `INPUT_LOCK`(F9) 可屏蔽远端物理键鼠实现独占

## 内网代理通道

控制端提供静态端口映射入口，复用控制连接按通道传输:

- **静态端口映射**(`-fwd tcp:本机端口=内网ip:端口`，可重复): 本机端口固定映射到
  被控端内网地址，支持 tcp/udp

流程:

1. 控制端分配通道号 id，发送 `PROXY_OPEN{id, addr, proto}`
2. 被控端在自身网络按 proto 拨号 addr(10s 超时)，回 `PROXY_ACK{id, ok}`；
   拨号失败或被控端配置 `proxy.enabled: false` 时 ok=0
3. 成功后双向转发: tcp 通道按字节流直接搬运(`PROXY_DATA` / `PROXY_DATA2`，
   分片上限 32KB)；udp 通道内每个数据报加 `u16 长度`前缀定界，同一本地对端
   地址复用一条通道，空闲 60s 回收
4. 任一侧结束发送 `PROXY_CLOSE` / `PROXY_CLOSE2`；单通道队列上限约 2MB，
   目标过慢时直接关闭该通道(有界内存，不影响桌面与其他通道)
5. 会话断开时双方各自清理全部通道；通道数据与桌面分块共用一条 TCP，
   代理大流量时可能挤占画面带宽，属预期行为
