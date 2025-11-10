# Research: Multi-Extension High-Performance Forwarding System

**Feature**: 001-multi-extension-forwarding
**Date**: 2025-11-10
**Purpose**: Document technical research and design decisions for scaling tctool-go to support 5000+ concurrent extensions

## Research Areas

### 1. Extension Number Extraction from SIP Messages

**Decision**: Extract extension from SIP URI user part in From/To headers

**Rationale**:
- Standard SIP URI format: `sip:extension@domain` or `sip:extension@ip:port`
- From header contains originating extension (outbound calls)
- To header contains destination extension (inbound calls)
- Go standard library provides string parsing capabilities
- No regex required - simple string manipulation sufficient

**Implementation Pattern**:
```go
// Parse SIP URI: sip:1001@192.168.1.10:5060
// Extract user part "1001" before @ symbol
func ExtractExtension(sipMsg string) (string, error) {
    // Search for "From:" or "To:" header
    // Extract URI between < and > or after header name
    // Split by @ and take user part
    // Validate numeric format (3-5 digits)
}
```

**Alternatives Considered**:
- Custom SIP header (X-Extension): Rejected - requires PBX configuration changes
- Contact header parsing: Rejected - Contact may not contain extension in all scenarios
- Via header: Rejected - contains network routing info, not extension identity

**Edge Cases Addressed**:
- Multiple From/To headers: Use first occurrence
- URI without < > brackets: Handle both formats
- Non-numeric extensions: Validate and reject with error

---

### 2. Connection Pool Architecture for 5000+ Extensions

**Decision**: Centralized connection pool with sync.Map for concurrent access

**Rationale**:
- sync.Map optimized for read-heavy workloads (extension lookups frequent)
- Built-in concurrency safety without explicit locking on reads
- Key: extension number (string), Value: *ExtensionConnection struct
- O(1) lookup performance critical for message routing at scale
- Go runtime handles sync.Map internal sharding for concurrent access

**Implementation Pattern**:
```go
type ConnectionPool struct {
    connections sync.Map // map[string]*ExtensionConnection
    capacity    int      // max 5000
    count       atomic.Int32
}

func (cp *ConnectionPool) GetOrCreate(ext string) (*ExtensionConnection, error) {
    // Load existing connection (lock-free read)
    if conn, ok := cp.connections.Load(ext); ok {
        return conn.(*ExtensionConnection), nil
    }

    // Check capacity before creation
    if cp.count.Load() >= int32(cp.capacity) {
        return nil, ErrCapacityReached
    }

    // Create new connection (rare operation)
    conn := NewExtensionConnection(ext)
    cp.connections.Store(ext, conn)
    cp.count.Add(1)
    return conn, nil
}
```

**Alternatives Considered**:
- map[string]*ExtensionConnection with RWMutex: Rejected - write lock required on every lookup, higher contention
- Sharded map with multiple locks: Rejected - adds complexity, sync.Map already optimized
- LRU cache with eviction: Rejected - eviction policy conflicts with active connection requirement

**Performance Characteristics**:
- Read operations (GetOrCreate with existing): Lock-free, ~50ns per lookup
- Write operations (new extension): ~500μs including TCP handshake
- Memory overhead: ~200 bytes per extension = 1MB for 5000 extensions

---

### 3. Per-Extension TCP Connection Management

**Decision**: One persistent TCP connection per extension with independent lifecycle

**Rationale**:
- Isolates extension failures (one bad connection doesn't affect others)
- Independent authentication and encryption keys per RFC security best practices
- Simplifies connection state tracking (no shared connection multiplexing)
- Aligns with tunnel server's existing connection-based protocol
- Goroutine per connection is acceptable in Go (lightweight: ~2KB stack)

**Lifecycle States**:
```
DISCONNECTED -> CONNECTING -> AUTHENTICATED -> ACTIVE -> IDLE -> CLEANUP -> DISCONNECTED
```

**State Transitions**:
- DISCONNECTED: No TCP connection exists
- CONNECTING: TCP dial in progress, authentication pending
- AUTHENTICATED: Handshake complete, AES keys exchanged
- ACTIVE: Messages being forwarded, last activity <5min
- IDLE: No activity for 5-30min, heartbeat sent periodically
- CLEANUP: Idle timeout reached, closing connection and releasing resources

**Implementation Pattern**:
```go
type ExtensionConnection struct {
    ext       string
    tcpConn   net.Conn
    tsAESKey  string
    portMgr   *PortManager
    state     atomic.Uint32  // State enum
    lastActivity atomic.Int64  // Unix timestamp
    mu        sync.RWMutex
}

func (ec *ExtensionConnection) SendSIP(msg string) error {
    ec.lastActivity.Store(time.Now().Unix())
    // Encrypt and send via tcpConn
}
```

**Alternatives Considered**:
- Connection multiplexing with extension ID in header: Rejected - requires server protocol changes
- Connection pooling with reuse across extensions: Rejected - violates security isolation principle
- HTTP/2 multiplexing: Rejected - tunnel server uses custom TCP protocol

---

### 4. Heartbeat and Connection Keepalive Strategy

**Decision**: Separate heartbeat goroutine per extension with configurable intervals

**Rationale**:
- Prevents server-side idle timeout (typically 10-15min for TCP)
- Detects connection failures before next message send
- Per-extension heartbeat allows independent timing based on activity
- Goroutine overhead acceptable (~2KB * 5000 = 10MB total)

**Heartbeat Logic**:
- Active connections (< 5min idle): No heartbeat (message traffic keeps alive)
- Idle connections (5-30min): Heartbeat every 2 minutes
- Inactive connections (> 30min): Stop heartbeat, trigger cleanup

**Implementation Pattern**:
```go
func (ec *ExtensionConnection) heartbeatLoop() {
    ticker := time.NewTicker(2 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            idleTime := time.Since(ec.getLastActivity())
            if idleTime > 30*time.Minute {
                ec.Cleanup()
                return
            } else if idleTime > 5*time.Minute {
                ec.sendHeartbeat()
            }
        case <-ec.shutdownChan:
            return
        }
    }
}
```

**Alternatives Considered**:
- Single global heartbeat manager: Rejected - increases complexity with centralized state
- TCP keepalive only: Rejected - timeout detection too slow (hours), doesn't detect application-level failures
- No heartbeat (reactive reconnection): Rejected - causes call setup delays on first message after idle

---

### 5. Graceful Degradation and Capacity Management

**Decision**: Hard limit at 5000 extensions with SIP 503 response for excess

**Rationale**:
- Predictable resource usage enables capacity planning
- SIP 503 "Service Unavailable" is standard protocol response for overload
- Retry-After header guides clients when to retry (60-300 seconds)
- Protects existing extensions from resource starvation
- Memory/CPU monitoring triggers warnings before hard limit

**Capacity Enforcement**:
```go
func (cp *ConnectionPool) GetOrCreate(ext string) (*ExtensionConnection, error) {
    if cp.count.Load() >= int32(cp.capacity) {
        return nil, ErrCapacityReached
    }
    // ... create connection
}

// In SIP handling:
if err == ErrCapacityReached {
    return fmt.Sprintf("SIP/2.0 503 Service Unavailable\r\nRetry-After: 180\r\n")
}
```

**Resource Monitoring**:
- Memory usage check: Reject new connections if >90% of 2GB limit
- Goroutine count: Log warning if >25000 (5000 extensions * 4 goroutines + overhead)
- CPU usage: Log warning if sustained >80% for >5 minutes

**Alternatives Considered**:
- Dynamic limit based on available resources: Rejected - unpredictable behavior, harder to test
- Connection queuing: Rejected - adds complexity, violates real-time requirement
- LRU eviction of idle connections: Rejected - unexpected extension disconnections hurt user experience

---

### 6. Memory Optimization for 5000 Extensions

**Decision**: Reuse buffer pools and optimize struct layout

**Rationale**:
- Each extension has: TCP conn, port manager, state - total ~400 bytes
- Message buffers reused via sync.Pool reduces GC pressure
- Struct field ordering reduces memory alignment padding
- Total estimated memory: 5000 extensions * 400 bytes + buffers = ~50MB base + ~100MB buffers

**Optimization Techniques**:
```go
// Buffer pool for SIP messages (reuse 4KB buffers)
var sipBufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 4096)
    },
}

// Optimized struct layout (fields ordered by size)
type ExtensionConnection struct {
    tcpConn      net.Conn       // 8 bytes pointer
    portMgr      *PortManager   // 8 bytes pointer
    lastActivity atomic.Int64   // 8 bytes
    state        atomic.Uint32  // 4 bytes
    ext          string          // 16 bytes (header + data)
    tsAESKey     [16]byte        // 16 bytes inline array
    mu           sync.RWMutex    // 24 bytes
    // Total: ~90 bytes + TCP conn overhead
}
```

**Memory Budget Breakdown**:
- Extension connections: 5000 * 400 bytes = 2MB
- Port mappings: 5000 * 10 ports avg * 100 bytes = 5MB
- Message buffers: 5000 * 8KB = 40MB
- Goroutine stacks: 5000 * 4 * 2KB = 40MB
- Go runtime heap: ~500MB (GC overhead, fragmentation)
- Total: ~590MB well under 2GB limit

**Alternatives Considered**:
- Object pooling for ExtensionConnection: Rejected - lifecycle management complexity, marginal benefit
- Compression of inactive connection state: Rejected - CPU overhead not justified
- External memory (Redis): Rejected - adds latency and external dependency

---

## Technology Stack Summary

| Component | Technology | Justification |
|-----------|------------|---------------|
| Language | Go 1.21+ | Existing codebase, excellent concurrency primitives |
| Concurrency | sync.Map, sync.RWMutex | Optimized for read-heavy concurrent access patterns |
| Networking | net.Conn (TCP/UDP) | Standard library, low-level control required |
| Encryption | crypto/aes, crypto/rsa | Existing security requirements, standard library sufficient |
| Testing | go test, -race flag | Built-in tooling, race detector critical for concurrency |
| Storage | In-memory only | No persistence required, performance critical |
| Dependencies | Zero new external deps | Minimizes supply chain risk, standard library sufficient |

---

## Risk Analysis

### High Risk
- **Goroutine explosion**: 20,000+ goroutines (4 per extension * 5000)
  - Mitigation: Goroutine pooling for RTP forwarding, careful lifecycle management
  - Monitoring: Runtime goroutine count tracking, alerts at thresholds

- **Memory leaks from unclosed connections**
  - Mitigation: Defer close patterns, WaitGroups for lifecycle tracking
  - Testing: 24-hour leak test (SC-009), memory profiling

### Medium Risk
- **Extension parsing edge cases** (malformed SIP messages)
  - Mitigation: Comprehensive parsing tests, fallback to From/To header priority
  - Impact: Invalid extension causes rejection, doesn't crash system

- **TCP connection storms** (5000 extensions connecting simultaneously)
  - Mitigation: Connection rate limiting (100 connections/second max), backpressure
  - Testing: Load test with simultaneous connection burst

### Low Risk
- **Port exhaustion** (20000-40000 range, 20000 ports available)
  - Mitigation: Average 4 ports per active call, 5000 simultaneous calls = max 20000 ports (at limit)
  - Monitoring: Port usage tracking, warnings at 80% capacity

---

## Open Questions Resolved

1. **Q: Does tunnel server support multiple concurrent TCP connections from same IP?**
   - A: Assumption YES - server-side authentication is per-connection based on existing protocol
   - Validation: Test with 2-3 concurrent connections during POC phase

2. **Q: Can PJSUA handle receiving from multiple local extensions on single UDP port?**
   - A: Assumption YES - PJSUA receives all SIP messages, extension differentiation happens at application layer
   - Validation: Current code already receives from any local source, extension parsing is new layer

3. **Q: What is acceptable inactive extension timeout?**
   - A: Decision: 30 minutes based on typical PBX idle timeout standards
   - Rationale: Balances resource cleanup with user experience (no unexpected disconnections)

---

## Next Steps

1. **Phase 1 Design**: Create data-model.md defining extension entities and state machine
2. **Phase 1 Contracts**: Define internal API contracts for ConnectionPool operations
3. **Phase 1 Quickstart**: Document development setup and testing procedures
4. **Phase 2 Tasks**: Generate task breakdown for implementation in priority order
