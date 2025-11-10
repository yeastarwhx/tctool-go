# Data Model: Multi-Extension High-Performance Forwarding System

**Feature**: 001-multi-extension-forwarding
**Date**: 2025-11-10
**Purpose**: Define core entities, relationships, and state machines for extension management

## Core Entities

### 1. Extension

**Purpose**: Represents a unique extension number with its identity and validation rules

**Attributes**:
- `Number` (string): Extension identifier (3-5 digit numeric string, e.g., "1001")
- `IsValid` (bool): Validation status after format check

**Validation Rules**:
- MUST be numeric characters only (regex: `^[0-9]{3,5}$`)
- MUST NOT be empty or nil
- MUST be between 3-5 characters in length
- SHOULD be unique across active connections

**Relationships**:
- One Extension has one ExtensionConnection (1:1)
- One Extension has one ExtensionAuthManager (1:1)
- One Extension has one ExtensionPortManager (1:1)

**Example**:
```json
{
  "Number": "1001",
  "IsValid": true
}
```

---

### 2. ExtensionConnection

**Purpose**: Manages TCP connection lifecycle, state, and activity tracking for a single extension

**Attributes**:
- `Extension` (Extension): Associated extension identifier
- `TCPConn` (net.Conn): Active TCP connection to tunnel server
- `State` (ConnectionState): Current connection state (enum)
- `CreatedAt` (time.Time): Connection creation timestamp
- `LastActivity` (time.Time): Timestamp of last SIP/RTP message
- `HeartbeatChan` (chan struct{}): Channel for heartbeat goroutine shutdown
- `ShutdownChan` (chan struct{}): Channel for graceful shutdown signaling
- `Mutex` (sync.RWMutex): Protects concurrent access to connection state

**State Machine** (ConnectionState enum):
```
DISCONNECTED (0) -> CONNECTING (1) -> AUTHENTICATED (2) -> ACTIVE (3) -> IDLE (4) -> CLEANUP (5) -> DISCONNECTED (0)
```

**State Definitions**:
- **DISCONNECTED**: No TCP connection exists, initial state or after cleanup
- **CONNECTING**: TCP dial in progress, authentication request sent
- **AUTHENTICATED**: Handshake complete, AES keys received and stored
- **ACTIVE**: Messages being forwarded, last activity within 5 minutes
- **IDLE**: No activity for 5-30 minutes, sending periodic heartbeats
- **CLEANUP**: Idle timeout exceeded, closing TCP connection and releasing resources

**State Transition Rules**:
| From State | To State | Trigger | Validation |
|------------|----------|---------|------------|
| DISCONNECTED | CONNECTING | First SIP message for extension | Extension must be valid |
| CONNECTING | AUTHENTICATED | Authentication response received | AES key must be 16 bytes |
| CONNECTING | DISCONNECTED | Authentication failure or timeout | Error logged, resources released |
| AUTHENTICATED | ACTIVE | First message forwarded | TCP connection verified |
| ACTIVE | ACTIVE | Message forwarded | Update LastActivity timestamp |
| ACTIVE | IDLE | 5 minutes without activity | Start heartbeat goroutine |
| IDLE | ACTIVE | New message arrives | Update LastActivity, stop heartbeat |
| IDLE | CLEANUP | 30 minutes without activity | Trigger cleanup sequence |
| CLEANUP | DISCONNECTED | Resources released | Close TCP, stop goroutines |
| Any State | DISCONNECTED | TCP connection error | Error handling, reconnection possible |

**Lifecycle Operations**:
```go
// Creation
func NewExtensionConnection(ext Extension) (*ExtensionConnection, error)

// Activity tracking
func (ec *ExtensionConnection) UpdateActivity()

// State queries
func (ec *ExtensionConnection) IsActive() bool
func (ec *ExtensionConnection) GetIdleTime() time.Duration

// Message forwarding
func (ec *ExtensionConnection) SendSIP(msg string, aesKey string) error
func (ec *ExtensionConnection) SendRTP(data []byte, aesKey string) error

// Lifecycle management
func (ec *ExtensionConnection) Cleanup() error
```

---

### 3. ConnectionPool

**Purpose**: Centralized registry managing all extension connections with concurrent access

**Attributes**:
- `Connections` (sync.Map): Map of extension number → *ExtensionConnection
- `Capacity` (int): Maximum allowed extensions (default: 5000)
- `Count` (atomic.Int32): Current number of active extensions
- `Mutex` (sync.RWMutex): Protects capacity checks and bulk operations

**Operations**:
```go
// Connection lifecycle
func (cp *ConnectionPool) GetOrCreate(ext Extension) (*ExtensionConnection, error)
func (cp *ConnectionPool) Get(extNumber string) (*ExtensionConnection, bool)
func (cp *ConnectionPool) Remove(extNumber string) error

// Capacity management
func (cp *ConnectionPool) IsFull() bool
func (cp *ConnectionPool) GetCount() int
func (cp *ConnectionPool) GetCapacity() int

// Bulk operations
func (cp *ConnectionPool) GetAllExtensions() []string
func (cp *ConnectionPool) CleanupInactive(idleTimeout time.Duration) int
func (cp *ConnectionPool) ShutdownAll() error
```

**Concurrency Patterns**:
- Read operations (Get, GetCount): Lock-free via sync.Map and atomic.Int32
- Write operations (GetOrCreate, Remove): Requires atomic count increment/decrement
- Bulk operations (CleanupInactive, ShutdownAll): Acquires mutex to prevent concurrent modifications

**Capacity Enforcement**:
```go
func (cp *ConnectionPool) GetOrCreate(ext Extension) (*ExtensionConnection, error) {
    // Fast path: Check existing (lock-free)
    if conn, ok := cp.Connections.Load(ext.Number); ok {
        return conn.(*ExtensionConnection), nil
    }

    // Slow path: Create new connection
    if cp.Count.Load() >= int32(cp.Capacity) {
        return nil, ErrCapacityReached
    }

    conn := NewExtensionConnection(ext)
    cp.Connections.Store(ext.Number, conn)
    cp.Count.Add(1)
    return conn, nil
}
```

---

### 4. ExtensionAuthManager

**Purpose**: Handles per-extension authentication with tunnel server and AES key management

**Attributes**:
- `Extension` (Extension): Associated extension identifier
- `TCAESKey` (string): Client-generated AES key (16 bytes, hex-encoded)
- `TSAESKey` (string): Server-provided AES key (16 bytes, hex-encoded)
- `AuthState` (AuthState): Authentication status (enum)
- `RetryCount` (int): Number of authentication attempts
- `Mutex` (sync.RWMutex): Protects key access and state updates

**Auth State Machine**:
```
UNAUTHENTICATED (0) -> AUTHENTICATING (1) -> AUTHENTICATED (2) -> FAILED (3)
```

**State Definitions**:
- **UNAUTHENTICATED**: Initial state, no authentication attempted
- **AUTHENTICATING**: Authentication request sent, waiting for response
- **AUTHENTICATED**: Received valid ts_aeskey from server
- **FAILED**: Authentication failed after retries, connection unusable

**Operations**:
```go
// Authentication flow
func (eam *ExtensionAuthManager) GenerateTCAESKey() string
func (eam *ExtensionAuthManager) Authenticate(conn net.Conn) error
func (eam *ExtensionAuthManager) AuthenticateWithRetry(conn net.Conn, retries int) error

// Key management
func (eam *ExtensionAuthManager) GetTCAESKey() string
func (eam *ExtensionAuthManager) GetTSAESKey() string
func (eam *ExtensionAuthManager) SetTSAESKey(key string) error
```

**Security Requirements**:
- TC AES keys MUST be generated using crypto/rand (cryptographically secure)
- TS AES keys MUST be validated (length == 16 bytes) before storage
- Keys MUST NOT be logged or exposed in error messages
- Each extension MUST have independent keys (no key reuse across extensions)

---

### 5. ExtensionPortManager

**Purpose**: Manages port allocations scoped to a specific extension for RTP/RTCP forwarding

**Attributes**:
- `Extension` (Extension): Associated extension identifier
- `Mappings` (map[string]*PortMappingNode): Call-ID → port mapping
- `NextAvailablePort` (int): Sequentially allocated port number
- `PortRange` (PortRange): Allowed port range for this extension
- `Mutex` (sync.RWMutex): Protects port allocation and mapping access

**PortRange** (sub-entity):
- `Start` (int): Starting port number (e.g., 20000)
- `End` (int): Ending port number (e.g., 40000)
- `MaxPorts` (int): Maximum ports per extension (e.g., 100)

**Port Allocation Strategy**:
- Each extension receives ports from global range (20000-40000)
- Ports allocated sequentially within extension's quota
- RTP/RTCP pairs MUST be consecutive (even RTP, odd RTCP)
- Port reuse only after call terminates and cleanup completes

**Operations**:
```go
// Port allocation
func (epm *ExtensionPortManager) AllocatePortPair() (rtpPort int, rtcpPort int, error)
func (epm *ExtensionPortManager) AllocatePortsForCall(hasVideo bool) (audioRTP, audioRTCP, videoRTP, videoRTCP int, error)

// Mapping management
func (epm *ExtensionPortManager) AddMapping(callID string, node *PortMappingNode) error
func (epm *ExtensionPortManager) FindMapping(callID string) *PortMappingNode
func (epm *ExtensionPortManager) RemoveMapping(callID string) (*PortMappingNode, error)

// Cleanup
func (epm *ExtensionPortManager) ReleaseCallPorts(callID string, tsAESKey string) error
func (epm *ExtensionPortManager) ReleaseAllPorts(tsAESKey string) error
```

---

## Entity Relationships

```
ConnectionPool (1)
    ├── Contains (1:N) ────> ExtensionConnection (N)
                                ├── Has (1:1) ────> Extension (1)
                                ├── Has (1:1) ────> ExtensionAuthManager (1)
                                └── Has (1:1) ────> ExtensionPortManager (1)
                                        └── Manages (1:N) ────> PortMappingNode (N)
```

**Cardinality Rules**:
- One ConnectionPool instance per application (singleton)
- One ExtensionConnection per unique extension number
- One Extension entity per connection (immutable after creation)
- One ExtensionAuthManager per connection (independent keys)
- One ExtensionPortManager per connection (isolated port tracking)
- Multiple PortMappingNodes per extension (one per concurrent call)

---

## Data Flow Diagrams

### Extension Connection Creation Flow

```
[UDP SIP Message]
    → ParseExtension(sipMsg)
    → Extension{Number: "1001"}
    → ConnectionPool.GetOrCreate(ext)
        → [If exists] Return existing ExtensionConnection
        → [If new and capacity OK]:
            1. NewExtensionConnection(ext)
            2. ConnectToServer() → TCPConn
            3. ExtensionAuthManager.Authenticate(conn) → TSAESKey
            4. State: DISCONNECTED → CONNECTING → AUTHENTICATED → ACTIVE
            5. Store in ConnectionPool.Connections
            6. Increment ConnectionPool.Count
            7. Return ExtensionConnection
        → [If capacity full] Return ErrCapacityReached
```

### Message Forwarding Flow

```
[SIP Message Arrives]
    → ParseExtension(msg) → Extension
    → ConnectionPool.Get(ext.Number) → ExtensionConnection
    → ExtensionConnection.SendSIP(msg, tsAESKey)
        → AESEncrypt(msg, tsAESKey)
        → TCPConn.Write(encrypted)
        → UpdateActivity()
        → State: IDLE → ACTIVE (if needed)
```

### Cleanup Flow

```
[Cleanup Timer Fires]
    → ConnectionPool.CleanupInactive(30min)
        → Iterate all ExtensionConnections
        → For each conn:
            → GetIdleTime() > 30min?
                → ExtensionPortManager.ReleaseAllPorts()
                → ExtensionConnection.Cleanup()
                    → Close(TCPConn)
                    → Close(HeartbeatChan)
                    → State → CLEANUP → DISCONNECTED
                → ConnectionPool.Remove(ext.Number)
                → Decrement ConnectionPool.Count
```

---

## Memory Layout Estimates

| Entity | Size per Instance | Count (5000 ext) | Total Memory |
|--------|-------------------|------------------|--------------|
| Extension | 32 bytes | 5000 | 160 KB |
| ExtensionConnection | 120 bytes | 5000 | 600 KB |
| ExtensionAuthManager | 80 bytes | 5000 | 400 KB |
| ExtensionPortManager | 100 bytes | 5000 | 500 KB |
| PortMappingNode | 200 bytes | 50000 (avg 10/ext) | 10 MB |
| TCP connections | 8 KB (kernel buffer) | 5000 | 40 MB |
| **Total Core Entities** | | | **~52 MB** |

**Additional Overhead**:
- sync.Map internal structure: ~5MB
- Goroutine stacks (4 per ext): 5000 * 4 * 2KB = 40MB
- Message buffers (sync.Pool): ~100MB
- **Estimated Total**: ~200MB (well under 2GB limit)

---

## Validation Rules Summary

### Extension Validation
- ✅ Numeric format (3-5 digits)
- ✅ Non-empty
- ✅ Unique across active connections

### Connection State Validation
- ✅ State transitions follow defined state machine
- ✅ TCP connection verified before AUTHENTICATED state
- ✅ AES keys validated before state transition

### Capacity Validation
- ✅ Connection count never exceeds capacity limit
- ✅ Memory usage monitored and enforced
- ✅ Port exhaustion detected and reported

### Concurrency Validation
- ✅ All shared state protected by mutex or atomic operations
- ✅ No data races (validated with `go test -race`)
- ✅ Deadlock prevention via consistent lock ordering

---

## Next Steps

1. **Phase 1 Contracts**: Define internal API contracts for ConnectionPool operations
2. **Phase 1 Quickstart**: Document development setup and testing procedures
3. **Phase 2 Tasks**: Generate task breakdown for implementation in priority order
