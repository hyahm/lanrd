# lanrd — 内网 Windows 远程桌面工具

通过内网 IP 直连 Windows 桌面的轻量远程工具。Go 实现、单二进制、无第三方服务端，
支持**双方(远端机器前的用户与控制端)同时抢占键盘鼠标操作**。

## 功能

- 内网直连: `被控IP:7080`，无需公网中转
- 多显示器: 采集整个虚拟桌面(所有屏幕合并)
- 高效传输: 分块脏检测 + JPEG 编码，仅推送变化区域
- 双方抢占: 控制端与远端用户随时输入，实时显示"对方正在操作"状态
- 锁定对方: 控制端按 `F9` 锁定/解锁远端物理键鼠(独占操作)
- 口令认证 / 心跳测延迟 / 分辨率变化自适应 / 全屏(`F11`)

## 编译

要求: Go 1.21+，Windows(被控端必须 64 位 amd64/arm64)

```bat
cd D:\data\todesk
go build -o lanrd.exe .
```

## 使用

### 图形界面(推荐)

直接双击 `lanrd.exe`(或运行 `lanrd ui`):

- **启动服务端(被控端)**: 点击启动，按钮变为"停止服务端"，再次点击即可停止
  (断开监听与在线控制端)；停止后可再次启动，状态区实时显示监听地址、内网 IP、接入/下线事件
- **开机自动启动服务端**: 勾选后写入当前用户注册表 Run 键(HKCU)，
  登录 Windows 时以 `-quiet` 模式后台静默运行被控端；
  被控端必须在已登录的桌面会话中运行(抓屏/键鼠注入需要)，因此不使用 Windows 服务方式
- **连接(控制端)**: 填写被控端 IP、端口(默认 7080)、口令(未配置则留空)，点击按钮拉起控制端窗口；
  支持 TAB 键切换输入框、回车直接连接、Esc 关闭
- **安全防护**: 连接目标为本机地址(localhost/环回/本机网卡 IP)时会被阻止——
  本机运行着服务端，客户端连自己会造成画面无限自嵌套死循环；IP 输入框默认为空

### 命令行

#### 被控端(要被远程的电脑)

在**已登录的桌面会话**中运行(不要以 Windows 服务方式运行，服务会话无法抓屏):

```bat
lanrd.exe agent
```

启动后会打印本机内网 IP。首次运行需放行防火墙(管理员):

```bat
netsh advfirewall firewall add rule name="lanrd" dir=in action=allow protocol=TCP localport=7080
```

配置文件 `configs/config.yaml`(可选，不存在时使用默认值)，支持环境变量覆盖:
`LANRD_ADDR` `LANRD_TOKEN` `LANRD_FPS` `LANRD_TILE` `LANRD_QUALITY` `LANRD_BLOCK_LOCAL`。

### 控制端

```bat
lanrd.exe client 192.168.1.100:7080
lanrd.exe client 192.168.1.100:7080 -token mysecret
```

热键(本地生效，不转发远端):

| 热键 | 功能 |
|------|------|
| `F9`  | 锁定/解锁远端物理键鼠 |
| `F11` | 全屏/窗口切换 |

画面按窗口自适应缩放居中，鼠标坐标自动映射回远端分辨率。

## 抢占键鼠说明

- 默认模式(`blockLocal: false`): 双方随时抢占，输入同时生效，左上角实时显示
  `* Remote user is operating *`(对方正在操作)
- `F9` 锁定: 屏蔽远端物理键鼠，实现控制端独占；再按 `F9` 解锁

## 性能

- **空闲自动降频**: 画面无变化时采集从 30fps 降到 10fps(约 0.3s 起)、再到 4fps(约 6s 起)，
  避免弱机(虚拟机)上 GDI BitBlt 空转吃满 CPU；无观看者时完全不抓屏
- 收到按键/点击/滚轮或拖拽中的移动后**立即唤醒抓屏**，不等下一个节拍
- 脏块 JPEG **多核并行编码** + 编码缓冲池化(全屏关键帧 3840x1080 ≈ 1020 块，实测 <20ms)
- 关键帧间隔 30s(TCP 可靠传输下仅作兜底)，避免周期性全屏编码尖刺造成规律性卡顿
- 发送端**消息合并**、接收端 4 协程**并行解码** + 上屏零分配
- 控制端默认窗口 **1:1 原分辨率**(放不下本机屏幕时缩到 90%)，1:1 用最近邻采样保证文字锐利

## 平台支持

- **控制端(client)**: 跨平台，Windows / macOS / Linux 均可编译运行(需在目标系统本地编译，
  ebiten 依赖本地图形库)
- **被控端(agent)**: 目前仅 Windows(GDI 抓屏 + SendInput 注入)。
  扩展方向: Linux/X11 用 XShm 抓屏 + XTest 注入；macOS 用 ScreenCaptureKit + CGEvent
  (需辅助功能/屏幕录制授权)。代码已按 `internal/capture` / `internal/inject` 分包隔离，
  按平台新增实现即可

## 已知限制

- 键盘以扫描码转发，跟随**远端**键盘布局；本地 `Alt+Tab`/`Win` 等系统快捷键会被本机拦截
- `Ctrl+Alt+Del`、UAC 安全桌面属于 Windows 安全机制，无法注入/采集
- UIPI 隔离: 非管理员运行的 agent 无法操作**管理员权限**的窗口(如任务管理器)，需要时请以管理员身份运行 agent
- 传输未加密(内网定位)，如需跨网段请自行套 TLS/VPN
- 单控制会话: 新连接会顶替旧连接
- 剪贴板同步、文件传输规划中

## 项目结构

```
lanrd/
├── main.go                    # 入口: 图形界面 / agent / client 子命令
├── configs/config.yaml        # 被控端配置
├── docs/protocol.md           # 通信协议文档
└── internal/
    ├── proto/                 # 二进制消息协议
    ├── config/                # 配置加载(yaml + 环境变量)
    ├── winapi/                # user32/gdi32 系统调用封装
    ├── capture/               # GDI 虚拟桌面抓屏
    ├── codec/                 # 分块脏检测 + JPEG 编码
    ├── inject/                # SendInput 键鼠注入 + 低级钩子(抢占感知/锁定)
    ├── agent/                 # 被控端服务
    ├── client/                # 控制端(网络 + ebiten 界面)
    └── launcher/              # 图形启动界面(原生 Win32)
```

## 测试

```bat
go test ./...
```
