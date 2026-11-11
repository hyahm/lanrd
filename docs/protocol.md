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
| TILE | 0x82 | S->C | `[u16 x][u16 y][u16 w][u16 h][jpeg]` 图像分块 |
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
