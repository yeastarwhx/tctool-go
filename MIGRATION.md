# PJSIP to sipgo Migration

## 变更概述

已成功将项目从C语言的pjsip库迁移到纯Go实现的sipgo库。

## 主要变化

### 1. 依赖变更
- **移除**: pjsip C库依赖 (需要CGO)
- **新增**: github.com/emiago/sipgo v0.22.0 (纯Go实现)

### 2. 文件变更
- **新建**: `sip_parser.go` - 使用sipgo实现的SIP解析器
- **备份**: `pjsip.go` -> `pjsip.go.bak` (已保留备份)
- **更新**: `main.go` - 移除PJSUA初始化和清理代码
- **更新**: `build.sh` - 更新注释说明不再需要CGO
- **更新**: `go.mod` - 添加sipgo依赖

### 3. API兼容性

所有原有函数签名保持不变,实现了完全兼容:

```go
// 这些函数API保持不变
func ParseCallID(sipMsg string) (string, error)
func ParseSDPInfo(sipMsg string) (*SDPInfo, error)
func ModifySIPPacket(...) (string, error)
func InitPJSUA(cfgName *string, codec string) error  // 现在是空操作
func DestroyPJSUA() error                              // 现在是空操作
```

### 4. 功能实现

#### ParseCallID
- 使用 `sip.ParseMessage()` 解析SIP消息
- 通过 `msg.CallID()` 获取Call-ID头

#### ParseSDPInfo
- 解析消息体中的SDP内容
- 支持解析:
  - c=行: 连接地址
  - m=行: 音视频媒体端口
  - a=rtcp:行: RTCP端口

#### ModifySIPPacket
- 修改Contact头 (host和port)
- 修改SDP内容 (连接地址和媒体端口)
- 自动更新Content-Length

## 优势

1. **无CGO依赖**:
   - 跨平台编译更简单
   - 不需要安装C库和头文件
   - 编译速度更快

2. **部署简化**:
   - 单一可执行文件
   - 无运行时库依赖
   - 容器化更轻量

3. **代码维护**:
   - 纯Go代码更易维护
   - 更好的类型安全
   - 更容易调试

4. **开发体验**:
   - Go原生工具链完全支持
   - IDE支持更好
   - 无需处理C/Go内存互操作

## 构建说明

### 安装依赖
```bash
go mod download
```

### 构建
```bash
# Linux/Mac
./build.sh

# 或直接使用go命令
go build -o tctool-go .
```

### 跨平台编译示例
```bash
# Windows
GOOS=windows GOARCH=amd64 go build -o tctool-go.exe .

# Linux
GOOS=linux GOARCH=amd64 go build -o tctool-go .

# Mac
GOOS=darwin GOARCH=arm64 go build -o tctool-go .
```

## 测试建议

1. **基础功能测试**:
   - SIP消息解析 (Call-ID提取)
   - SDP信息解析 (音视频端口、连接地址)
   - SIP包修改 (Contact头、SDP字段)

2. **场景测试**:
   - INVITE请求处理
   - 200 OK响应处理
   - BYE/CANCEL请求处理
   - 音视频通话建立

3. **边界情况**:
   - 不包含SDP的SIP消息
   - 仅音频的会话
   - 音视频会话
   - 不规范的SDP格式

## 回滚方案

如果需要回滚到pjsip实现:

```bash
# 恢复pjsip.go
mv pjsip.go.bak pjsip.go

# 删除sip_parser.go
rm sip_parser.go

# 恢复main.go中的PJSUA初始化代码
# (需要手动恢复)

# 更新go.mod移除sipgo依赖
go mod edit -droprequire github.com/emiago/sipgo
```

## 注意事项

1. sipgo库目前还在向1.0版本发展,API可能有变化
2. 如果遇到特殊的SIP/SDP格式,可能需要调整解析逻辑
3. 性能方面,纯Go实现可能略低于C实现,但对于测试环境足够

## 相关资源

- [sipgo GitHub](https://github.com/emiago/sipgo)
- [sipgo文档](https://pkg.go.dev/github.com/emiago/sipgo)
- [RFC 3261 - SIP协议](https://www.rfc-editor.org/rfc/rfc3261)
