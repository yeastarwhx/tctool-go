# tctool-go - SIP Tunnel Client

多分机SIP隧道客户端,支持将本地PBX的多个分机通过加密隧道连接到远程SIP服务器。

## 特性

- ✅ **纯Go实现**: 无C依赖,使用sipgo库处理SIP协议
- ✅ **多分机支持**: 支持最多5000个分机并发连接
- ✅ **加密通信**: 使用AES加密保护隧道通信
- ✅ **智能路由**: 自动识别分机号并路由到对应连接
- ✅ **端口管理**: 自动分配和管理RTP/RTCP端口
- ✅ **容错机制**: TCP重连、空闲超时、心跳保活
- ✅ **RTP空闲超时**: 5分钟无数据自动释放资源,防止泄漏
- ✅ **简单部署**: 单一可执行文件,无运行时依赖

## 系统架构

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   PBX       │   UDP   │  tctool-go  │   TCP   │ Tunnel      │
│  (多分机)    │ ◄─────► │   (Client)  │ ◄─────► │  Server     │
│  5060       │         │   本地代理   │  加密    │  远程服务    │
└─────────────┘         └─────────────┘         └─────────────┘
      │                        │                        │
      └────── SIP信令 ─────────┴────── 修改后转发 ───────┘
```

## 快速开始

### 环境要求

- Go 1.21+
- Linux/Windows/MacOS

### 编译

```bash
# 克隆仓库
cd tctool-go

# 下载依赖
go mod download

# 编译
./build.sh
# 或
go build -o tctool-go .
```

### 配置

修改 `main.go` 中的配置常量:

```go
const (
    LocalUDPPort = 5060              // 本地UDP监听端口(PBX端)
    ServerIP     = "172.16.17.22"    // 隧道服务器IP
    ServerPort   = 6060              // 隧道服务器端口
)

var (
    LocalIP     = "192.168.12.18"    // 本地IP(修改Contact头)
    ServerSDPIP = "127.0.0.1"        // 服务器侧SDP地址
)
```

### 运行

```bash
./tctool-go
```

输出示例:
```
========================================
  Tunnel Client (TC) - Go Version
========================================
Local UDP Port:  5060
Server IP:       172.16.17.22
Server Port:     6060
========================================

Connection pool initialized (capacity: 5000)
UDP thread started (recvFromPBX)
Multi-extension mode: TCP connections created per extension on-demand

Tunnel Client is running. Press Ctrl+C to exit.
```

## 工作原理

### 1. 分机识别

从SIP消息的From/Contact头中提取分机号:
```
From: <sip:8001@192.168.1.100>  → 提取分机号 8001
```

### 2. 连接管理

- 每个分机维护独立的TCP连接到服务器
- 首次收到分机消息时自动创建连接
- 连接池管理所有分机连接(最多5000个)
- 空闲30分钟自动清理

### 3. SIP消息处理

**INVITE (PBX → Server)**:
```
1. 解析SDP获取PBX的RTP端口
2. 向服务器申请远程RTP端口
3. 分配本地TC端口
4. 修改SDP中的IP和端口
5. 转发到服务器
```

**200 OK (Server → PBX)**:
```
1. 修改SDP指向本地TC端口
2. 转发给PBX
```

**BYE/CANCEL**:
```
1. 清理端口映射
2. 释放服务器端口
```

### 4. RTP/RTCP转发

为每个呼叫建立双向转发:
```
PBX RTP ◄──► TC端口 ◄──(加密隧道)──► TS端口 ◄──► 远程终端
```

## 项目结构

```
tctool-go/
├── main.go           # 主程序入口,UDP监听,分机路由
├── sip_parser.go     # SIP/SDP解析和修改(sipgo实现)
├── extension.go      # 分机连接管理,连接池
├── ext_parser.go     # 分机号提取逻辑
├── sipmsg.go         # SIP消息类型判断和处理
├── tunnel.go         # 隧道协议,加密通信,端口申请
├── build.sh          # 编译脚本
├── go.mod            # Go模块依赖
├── MIGRATION.md      # PJSIP迁移文档
└── CODE_REVIEW.md    # 代码审查和优化建议
```

## 核心模块

### SIP解析 (sip_parser.go)

```go
// 提取Call-ID
ParseCallID(sipMsg string) (string, error)

// 解析SDP信息
ParseSDPInfo(sipMsg string) (*SDPInfo, error)

// 修改SIP包
ModifySIPPacket(sipMsg, contactHost, contactPort, audioRTP, ...) (string, error)
```

### 连接管理 (extension.go)

```go
// 连接池
type ConnectionPool struct {
    Connections sync.Map
    Count       atomic.Int32
    Capacity    int32
}

// 获取或创建分机连接
GetOrCreate(ext *Extension, pm *PortManager) (*ExtensionConnection, error)

// 分机连接
type ExtensionConnection struct {
    Extension    *Extension
    TCPConn      net.Conn
    AuthManager  *AuthManager
    SrcSIPAddr   *net.UDPAddr  // UDP源地址
    LastActivity time.Time
    // ...
}
```

### 端口管理 (main.go)

```go
// 端口映射
type PortMappingNode struct {
    CallID       string
    TCAudioRTP   int  // 本地TC端口
    TSAudioRTP   int  // 服务器端口
    PBXAudioRTP  int  // PBX端口
    // Video ports...
}

// 端口管理器
type PortManager struct {
    mappings          map[string]*PortMappingNode
    nextAvailablePort int
}
```

## 技术栈

- **Go 1.21+**: 主要开发语言
- **sipgo v0.22.0**: SIP协议解析 (github.com/emiago/sipgo)
- **golang.org/x/crypto**: AES加密
- **标准库**: net, sync, encoding/json等

## 迁移说明

本项目原使用C语言的pjsip库,现已完全迁移到纯Go实现(sipgo)。

**主要变化**:
- 移除所有CGO代码
- 移除pjsip C库依赖
- 使用sipgo进行SIP解析和处理
- 保持API兼容,业务逻辑无变化

详见: [MIGRATION.md](MIGRATION.md)

## 故障排查

### 编译失败

```bash
# 确保Go版本
go version  # 应该 >= 1.21

# 清理并重新下载依赖
go clean -modcache
go mod download
go mod tidy

# 重新编译
go build -o tctool-go .
```

### 连接失败

1. 检查服务器IP和端口配置
2. 确认网络连通性: `telnet 172.16.17.22 6060`
3. 检查防火墙设置
4. 查看日志中的错误信息

### 分机号识别错误

确认PBX发送的SIP消息格式:
```bash
# 查看日志中的SIP消息
# From/Contact头应该包含分机号
From: <sip:8001@...>  ✓
From: <sip:user@...>  ✗ (非数字分机号)
```

### 端口耗尽

默认端口范围: 20000-40000 (20000个端口)
每个呼叫占用2个端口(RTP+RTCP),支持约10000个并发呼叫。

如需调整:
```go
const (
    TCPortStart = 20000
    TCPortMax   = 40000  // 修改此值
)
```

## 性能指标

- **并发分机**: 5000个
- **并发呼叫**: ~10000个
- **消息延迟**: < 50ms
- **内存占用**: ~1MB/分机连接
- **CPU使用**: 1000并发呼叫 < 80%

## 优雅关闭

程序支持优雅关闭(Ctrl+C):

1. 停止接收新消息
2. 关闭所有分机TCP连接
3. 停止所有端口映射
4. 释放服务器端口
5. 关闭UDP监听

## 开发

### 运行测试

```bash
go test ./...
```

### 代码检查

```bash
# 格式化
go fmt ./...

# 静态检查
go vet ./...

# 代码规范检查(需安装golangci-lint)
golangci-lint run
```

### 性能分析

```bash
# CPU性能分析
go test -cpuprofile=cpu.prof -bench=.

# 内存分析
go test -memprofile=mem.prof -bench=.

# 查看分析结果
go tool pprof cpu.prof
```

## 许可证

本项目仅供测试使用。

## 支持

如有问题,请联系开发团队或查看:
- [CODE_REVIEW.md](CODE_REVIEW.md) - 代码审查和优化建议
- [MIGRATION.md](MIGRATION.md) - PJSIP迁移详情
