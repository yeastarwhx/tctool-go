# Quickstart: Multi-Extension High-Performance Forwarding System

**Feature**: 001-multi-extension-forwarding
**Date**: 2025-11-10
**Purpose**: Development setup, build, test, and deployment guide

## Prerequisites

### System Requirements

- **Operating System**: Linux (Ubuntu 20.04+ recommended) or Windows 10+
- **Go Version**: 1.21 or higher
- **Memory**: 4GB minimum (8GB recommended for testing with 5000 extensions)
- **Disk Space**: 2GB free space
- **Network**: Access to tunnel server (IP: 172.16.17.22, Port: 6060)

### Required Tools

```bash
# Check Go version
go version  # Should be >= 1.21

# Install Git (if not already installed)
# Ubuntu/Debian:
sudo apt-get update && sudo apt-get install -y git

# Windows: Download from https://git-scm.com/
```

### PJSUA Library

The project requires PJSUA library for SIP handling. Installation instructions:

**Linux:**
```bash
# Install PJSIP development libraries
sudo apt-get install -y libpjproject-dev

# Verify installation
pkg-config --modversion libpjproject
```

**Windows:**
```powershell
# Download PJSIP from https://www.pjsip.org/download.htm
# Extract and build according to PJSIP documentation
# Set CGO_CFLAGS and CGO_LDFLAGS environment variables
```

---

## Initial Setup

### 1. Clone Repository

```bash
cd /path/to/workspace
git clone <repository-url> tctool-go
cd tctool-go
```

### 2. Checkout Feature Branch

```bash
git checkout 001-multi-extension-forwarding
```

### 3. Install Dependencies

```bash
# Go modules (if go.mod exists)
go mod download

# Verify no external dependencies required
go list -m all
```

---

## Build

### Development Build

```bash
# Build with debug symbols
go build -o tctool-go .

# Or use existing build script
bash build.sh
```

### Production Build

```bash
# Build with optimizations and reduced binary size
go build -ldflags="-s -w" -o tctool-go .
```

### Build Verification

```bash
# Check binary exists and runs
./tctool-go --help  # Should show usage or start message

# Check binary size (should be ~5-10MB)
ls -lh tctool-go
```

---

## Configuration

### Environment Variables

```bash
# Server configuration
export SERVER_IP="172.16.17.22"
export SERVER_PORT="6060"

# Local UDP port for SIP
export LOCAL_UDP_PORT="5060"

# Extension capacity limit
export MAX_EXTENSIONS="5000"

# Idle timeout for cleanup (in minutes)
export IDLE_TIMEOUT="30"
```

### Configuration File (Optional)

Create `config.json`:

```json
{
  "server": {
    "ip": "172.16.17.22",
    "port": 6060
  },
  "local": {
    "udp_port": 5060
  },
  "extensions": {
    "max_capacity": 5000,
    "idle_timeout_minutes": 30,
    "heartbeat_interval_seconds": 120
  },
  "performance": {
    "max_memory_mb": 2048,
    "max_cpu_percent": 80
  }
}
```

---

## Running

### Development Mode

```bash
# Run with console output
./tctool-go
```

**Expected Output:**
```
========================================
  Tunnel Client (TC) - Go Version
========================================
Local UDP Port:  5060
Server IP:        172.16.17.22
Server Port:      6060
========================================

UDP thread started (recvFromPJSIP)
TCP thread started (recvFromTS)

Tunnel Client is running. Press Ctrl+C to exit.
```

### Background Mode

```bash
# Run as background process
nohup ./tctool-go > tctool.log 2>&1 &

# Check process
ps aux | grep tctool-go

# View logs
tail -f tctool.log
```

### Graceful Shutdown

```bash
# Send SIGTERM signal
kill -TERM <pid>

# Or use Ctrl+C if running in foreground
```

**Expected Shutdown Output:**
```
收到关闭信号，正在优雅退出...
正在停止所有端口映射...
Cleaned up 1234 extension connections
所有 goroutines 已正常退出
Tunnel Client 已停止
```

---

## Testing

### Unit Tests

```bash
# Run all unit tests
go test ./... -v

# Run specific test file
go test -v ./tests/extension_test.go

# Run with race detector (IMPORTANT)
go test ./... -race -v
```

### Extension Parsing Tests

```bash
# Test extension extraction from various SIP formats
go test -v ./tests/ext_parser_test.go

# Expected tests:
# - Valid numeric extensions (1001-99999)
# - Invalid formats (non-numeric, too short/long)
# - Multiple header scenarios
# - Malformed SIP messages
```

### Connection Pool Tests

```bash
# Test connection pool operations
go test -v ./tests/extension_test.go

# Expected tests:
# - GetOrCreate (new and existing)
# - Capacity enforcement (5000 limit)
# - Concurrent access (race detector)
# - Cleanup inactive connections
```

### Integration Tests

```bash
# Full end-to-end flow test
go test -v ./tests/integration_test.go

# Test scenarios:
# - Multiple extensions sending SIP messages
# - Port allocation and cleanup
# - Graceful degradation at capacity
# - Connection recovery after failures
```

### Load Testing

```bash
# Simulate 5000 concurrent extensions
go test -v ./tests/load_test.go -timeout 30m

# Monitor resource usage during test
# - Memory: Should stay < 2GB
# - CPU: Should stay < 80%
# - Goroutines: Should be ~20,000 (4 per ext)
```

### Race Condition Testing

```bash
# Run all tests with race detector
go test ./... -race -v

# This MUST pass before merging to main
# Any race conditions are critical bugs
```

---

## Monitoring

### Resource Usage

```bash
# Monitor memory usage
ps aux | grep tctool-go | awk '{print $6/1024 " MB"}'

# Monitor CPU usage
top -p <pid>

# Monitor goroutine count (requires pprof endpoint)
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

### Extension Connection Status

```bash
# View active extension count (add status endpoint)
curl http://localhost:6060/status

# Expected response:
# {
#   "active_extensions": 1234,
#   "capacity": 5000,
#   "memory_mb": 580,
#   "goroutines": 4938
# }
```

### Log Analysis

```bash
# Count extension creation events
grep "Extension connection created" tctool.log | wc -l

# Count cleanup events
grep "Extension connection cleaned up" tctool.log | wc -l

# Find errors
grep "ERROR" tctool.log

# Monitor specific extension
grep "ext=1001" tctool.log
```

---

## Troubleshooting

### Issue: Connection Pool Full

**Symptoms:**
- SIP 503 responses being sent
- Log messages: "Connection pool at capacity"

**Solutions:**
1. Increase MAX_EXTENSIONS environment variable
2. Reduce IDLE_TIMEOUT to cleanup faster
3. Check for inactive extensions not being cleaned up

```bash
# Force cleanup of inactive extensions
kill -USR1 <pid>  # If signal handler implemented
```

### Issue: High Memory Usage

**Symptoms:**
- Memory usage approaching 2GB
- Log warnings: "Memory usage high"

**Solutions:**
1. Check for memory leaks with pprof:
```bash
go tool pprof http://localhost:6060/debug/pprof/heap
```

2. Verify connection cleanup is working:
```bash
grep "cleaned up" tctool.log | tail -20
```

3. Reduce number of active extensions or increase server resources

### Issue: Extension Parsing Failures

**Symptoms:**
- Log messages: "Extension parsing failed"
- Messages being rejected

**Solutions:**
1. Verify SIP message format:
```bash
tcpdump -i any -A 'udp port 5060' > sip_messages.txt
```

2. Check for non-standard extension formats
3. Review ext_parser.go logic for edge cases

### Issue: TCP Connection Failures

**Symptoms:**
- Log messages: "Failed to connect to server"
- Extension connections stuck in CONNECTING state

**Solutions:**
1. Verify server reachability:
```bash
telnet 172.16.17.22 6060
```

2. Check firewall rules
3. Verify server is accepting multiple concurrent connections

### Issue: Race Conditions Detected

**Symptoms:**
- `go test -race` reports data races
- Intermittent crashes or panics

**Solutions:**
1. Review race detector output carefully
2. Ensure all shared state uses proper synchronization:
   - sync.Map for connection pool
   - sync.RWMutex for connection state
   - atomic operations for counters
3. Fix race conditions before proceeding (NON-NEGOTIABLE per constitution)

---

## Performance Benchmarks

### Expected Performance Metrics

| Metric | Target | Measurement Command |
|--------|--------|---------------------|
| Extension connection creation | < 500ms | `time curl -X POST localhost:6060/test/create_extension` |
| SIP forwarding latency | < 100ms | `go test -bench BenchmarkSIPForwarding` |
| RTP forwarding latency | < 10ms | `go test -bench BenchmarkRTPForwarding` |
| Memory usage (5000 ext) | < 2GB | `ps aux \| grep tctool-go` |
| CPU usage (full load) | < 80% | `top -p <pid>` |
| Connection reuse rate | > 95% | `grep "connection reused" tctool.log \| wc -l` |

### Running Benchmarks

```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# Run specific benchmark
go test -bench=BenchmarkSIPForwarding -benchtime=10s

# Generate CPU profile
go test -bench=. -cpuprofile=cpu.prof
go tool pprof cpu.prof

# Generate memory profile
go test -bench=. -memprofile=mem.prof
go tool pprof mem.prof
```

---

## Deployment

### Pre-Deployment Checklist

- [ ] All unit tests pass: `go test ./... -v`
- [ ] Race detector clean: `go test ./... -race`
- [ ] Load test passes (5000 extensions): `go test -v ./tests/load_test.go`
- [ ] 24-hour stability test completed with zero leaks
- [ ] Constitution compliance verified
- [ ] Performance benchmarks meet targets
- [ ] Graceful shutdown tested

### Production Deployment

```bash
# 1. Build production binary
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o tctool-go .

# 2. Copy to production server
scp tctool-go user@prod-server:/opt/tctool/

# 3. Create systemd service
sudo nano /etc/systemd/system/tctool.service
```

**tctool.service:**
```ini
[Unit]
Description=TC Tool - Multi-Extension SIP/RTP Proxy
After=network.target

[Service]
Type=simple
User=tctool
WorkingDirectory=/opt/tctool
ExecStart=/opt/tctool/tctool-go
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal

# Environment
Environment="SERVER_IP=172.16.17.22"
Environment="SERVER_PORT=6060"
Environment="MAX_EXTENSIONS=5000"

# Resource limits
LimitNOFILE=65536
LimitNPROC=32768

[Install]
WantedBy=multi-user.target
```

```bash
# 4. Start service
sudo systemctl daemon-reload
sudo systemctl enable tctool
sudo systemctl start tctool

# 5. Verify service status
sudo systemctl status tctool

# 6. Monitor logs
sudo journalctl -u tctool -f
```

### Rolling Upgrade

```bash
# 1. Build new version
go build -ldflags="-s -w" -o tctool-go-new .

# 2. Stop old service gracefully
sudo systemctl stop tctool

# 3. Replace binary
sudo mv /opt/tctool/tctool-go /opt/tctool/tctool-go.old
sudo mv tctool-go-new /opt/tctool/tctool-go

# 4. Start new version
sudo systemctl start tctool

# 5. Verify connections restored
curl http://localhost:6060/status
```

---

## Development Workflow

### 1. Feature Development

```bash
# Create feature branch
git checkout -b feature/add-extension-metrics

# Make changes
vim extension.go

# Run tests
go test ./... -v

# Run race detector
go test ./... -race
```

### 2. Code Review Checklist

- [ ] Constitution compliance verified (all 7 principles)
- [ ] Unit tests added for new functions
- [ ] Race detector passes
- [ ] Error handling includes extension context in logs
- [ ] Mutex locks follow defer pattern
- [ ] Connection cleanup properly implemented
- [ ] No hardcoded configuration values

### 3. Commit and Push

```bash
# Stage changes
git add .

# Commit with descriptive message
git commit -m "feat(extension): add per-extension metrics tracking

- Add prometheus metrics for connection pool
- Include extension count, memory usage, latency
- Update constitution compliance check

Refs: #001-multi-extension-forwarding"

# Push to remote
git push origin feature/add-extension-metrics
```

---

## Additional Resources

### Documentation

- [Feature Specification](./spec.md)
- [Implementation Plan](./plan.md)
- [Data Model](./data-model.md)
- [Internal API Contracts](./contracts/internal-api.md)
- [Research Decisions](./research.md)

### Go Resources

- [Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go sync.Map Documentation](https://pkg.go.dev/sync#Map)
- [Race Detector Guide](https://go.dev/doc/articles/race_detector)

### SIP Protocol

- [RFC 3261 - SIP](https://datatracker.ietf.org/doc/html/rfc3261)
- [SIP URI Format](https://www.ietf.org/rfc/rfc3986.txt)

### Support

- Project Issues: [GitHub Issues](https://github.com/your-org/tctool-go/issues)
- Constitution: [.specify/memory/constitution.md](../../.specify/memory/constitution.md)

---

## Quick Reference Commands

```bash
# Build
go build -o tctool-go .

# Test
go test ./... -v
go test ./... -race

# Run
./tctool-go

# Monitor
ps aux | grep tctool-go
top -p <pid>

# Logs
tail -f tctool.log
grep "ERROR" tctool.log

# Benchmarks
go test -bench=. -benchmem

# Profile
go tool pprof http://localhost:6060/debug/pprof/heap
```
