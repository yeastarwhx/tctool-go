# Internal API Contracts: Extension Connection Management

**Feature**: 001-multi-extension-forwarding
**Date**: 2025-11-10
**Purpose**: Define internal Go API contracts for extension connection pool and lifecycle management

## Overview

This document defines the programmatic interfaces (Go APIs) for managing extension connections within tctool-go. These are **internal APIs** used within the application, not external HTTP/REST endpoints.

---

## 1. Extension Parser API

### ExtractExtension

**Purpose**: Parse extension number from SIP message headers

**Signature**:
```go
func ExtractExtension(sipMsg string) (Extension, error)
```

**Input**:
- `sipMsg` (string): Complete SIP message including headers and body

**Output**:
- `Extension`: Parsed and validated extension entity
- `error`: Parsing or validation error

**Behavior**:
1. Search for "From:" header in SIP message
2. Extract SIP URI between `<` and `>` or after header name
3. Parse user part before `@` symbol
4. Validate numeric format (3-5 digits)
5. Return Extension{Number: parsed, IsValid: true} or error

**Errors**:
- `ErrExtensionNotFound`: No valid extension in From/To headers
- `ErrInvalidExtensionFormat`: Extension doesn't match ^[0-9]{3,5}$ regex
- `ErrMalformedSIPMessage`: SIP message structure invalid

**Example**:
```go
sipMsg := "INVITE sip:2000@pbx.example.com SIP/2.0\r\n" +
          "From: <sip:1001@192.168.1.10:5060>\r\n" +
          "To: <sip:2000@pbx.example.com>\r\n"

ext, err := ExtractExtension(sipMsg)
// ext.Number = "1001"
// ext.IsValid = true
```

**Edge Cases**:
- Multiple From headers: Use first occurrence
- No angle brackets in URI: Parse directly after "From: "
- Extension in To header only: Fallback to To header
- Non-numeric extension: Return ErrInvalidExtensionFormat

---

## 2. Connection Pool API

### GetOrCreate

**Purpose**: Retrieve existing extension connection or create new one if capacity permits

**Signature**:
```go
func (cp *ConnectionPool) GetOrCreate(ext Extension) (*ExtensionConnection, error)
```

**Input**:
- `ext` (Extension): Validated extension entity

**Output**:
- `*ExtensionConnection`: Existing or newly created connection
- `error`: Capacity or creation error

**Behavior**:
1. Attempt lock-free lookup in sync.Map (fast path)
2. If found, return existing connection
3. If not found, check capacity limit
4. If capacity available:
   - Create new ExtensionConnection
   - Connect to tunnel server (TCP)
   - Authenticate and obtain AES keys
   - Store in sync.Map
   - Increment atomic count
   - Return new connection
5. If capacity full, return ErrCapacityReached

**Errors**:
- `ErrCapacityReached`: Connection pool at maximum (5000)
- `ErrConnectionFailed`: TCP connection to server failed
- `ErrAuthenticationFailed`: Authentication with server failed

**Concurrency**: Thread-safe via sync.Map and atomic operations

**Example**:
```go
pool := NewConnectionPool(5000)
ext := Extension{Number: "1001", IsValid: true}

conn, err := pool.GetOrCreate(ext)
if err == ErrCapacityReached {
    // Send SIP 503 response
} else if err != nil {
    // Log error, reject message
} else {
    // Use conn to forward SIP message
}
```

---

### Get

**Purpose**: Retrieve existing extension connection without creating new one

**Signature**:
```go
func (cp *ConnectionPool) Get(extNumber string) (*ExtensionConnection, bool)
```

**Input**:
- `extNumber` (string): Extension number to lookup

**Output**:
- `*ExtensionConnection`: Found connection (nil if not found)
- `bool`: True if connection exists, false otherwise

**Behavior**:
1. Perform lock-free lookup in sync.Map
2. Return connection and existence flag

**Concurrency**: Thread-safe (lock-free read)

**Example**:
```go
conn, exists := pool.Get("1001")
if exists {
    conn.SendSIP(sipMsg, tsAESKey)
}
```

---

### Remove

**Purpose**: Remove extension connection from pool and cleanup resources

**Signature**:
```go
func (cp *ConnectionPool) Remove(extNumber string) error
```

**Input**:
- `extNumber` (string): Extension number to remove

**Output**:
- `error`: Error if connection not found or cleanup failed

**Behavior**:
1. Lookup connection in sync.Map
2. If found:
   - Call conn.Cleanup() to release resources
   - Delete from sync.Map
   - Decrement atomic count
3. If not found, return ErrExtensionNotFound

**Errors**:
- `ErrExtensionNotFound`: No connection exists for extension
- `ErrCleanupFailed`: Resource cleanup encountered error

**Concurrency**: Thread-safe via atomic count and sync.Map

---

### CleanupInactive

**Purpose**: Periodic cleanup of idle extension connections

**Signature**:
```go
func (cp *ConnectionPool) CleanupInactive(idleTimeout time.Duration) int
```

**Input**:
- `idleTimeout` (time.Duration): Idle time threshold (e.g., 30 minutes)

**Output**:
- `int`: Number of connections cleaned up

**Behavior**:
1. Iterate all connections in sync.Map
2. For each connection:
   - Check GetIdleTime() > idleTimeout
   - If idle, call Remove(extNumber)
3. Return count of removed connections

**Concurrency**: Thread-safe, but holds no locks (may miss connections added during iteration)

**Example**:
```go
// Cleanup goroutine
ticker := time.NewTicker(5 * time.Minute)
for range ticker.C {
    count := pool.CleanupInactive(30 * time.Minute)
    log.Printf("Cleaned up %d inactive extensions", count)
}
```

---

### ShutdownAll

**Purpose**: Graceful shutdown of all extension connections

**Signature**:
```go
func (cp *ConnectionPool) ShutdownAll() error
```

**Output**:
- `error`: Error if any connection cleanup failed

**Behavior**:
1. Iterate all connections in sync.Map
2. For each connection:
   - Release server-side RTP ports via ExtensionPortManager
   - Call conn.Cleanup()
3. Clear sync.Map
4. Reset count to zero
5. Return first error encountered (if any)

**Concurrency**: Thread-safe, but blocks new GetOrCreate calls during shutdown

**Example**:
```go
// Signal handler
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

<-sigChan
log.Println("Shutting down all extensions...")
if err := pool.ShutdownAll(); err != nil {
    log.Printf("Shutdown error: %v", err)
}
```

---

## 3. Extension Connection API

### SendSIP

**Purpose**: Encrypt and forward SIP message through extension's TCP connection

**Signature**:
```go
func (ec *ExtensionConnection) SendSIP(msg string, tsAESKey string) error
```

**Input**:
- `msg` (string): SIP message to forward
- `tsAESKey` (string): Server AES key for encryption

**Output**:
- `error`: Encryption or transmission error

**Behavior**:
1. Update LastActivity timestamp
2. Encrypt SIP message with AES-128-CBC
3. Pack encrypted data with tunnel protocol header
4. Write to TCP connection
5. Transition state from IDLE to ACTIVE if needed

**Errors**:
- `ErrNotAuthenticated`: Connection not in AUTHENTICATED state
- `ErrEncryptionFailed`: AES encryption failed
- `ErrTCPWriteFailed`: TCP connection write failed

**Concurrency**: Thread-safe via connection mutex

---

### SendRTP

**Purpose**: Encrypt and forward RTP/RTCP packet through extension's UDP tunnel

**Signature**:
```go
func (ec *ExtensionConnection) SendRTP(data []byte, portType string, tsAESKey string) error
```

**Input**:
- `data` ([]byte): RTP/RTCP packet data
- `portType` (string): Port identifier (audio_rtp, audio_rtcp, video_rtp, video_rtcp)
- `tsAESKey` (string): Server AES key for encryption

**Output**:
- `error`: Encryption or transmission error

**Behavior**:
1. Update LastActivity timestamp
2. Lookup port mapping for active call
3. Build RTP header with PBX address and TS ports
4. Encrypt header + data with AES-128-CBC
5. Send to tunnel server UDP endpoint

**Errors**:
- `ErrNoPortMapping`: No active call found for extension
- `ErrEncryptionFailed`: AES encryption failed
- `ErrUDPSendFailed`: UDP transmission failed

---

### UpdateActivity

**Purpose**: Update last activity timestamp to prevent idle timeout

**Signature**:
```go
func (ec *ExtensionConnection) UpdateActivity()
```

**Behavior**:
1. Set LastActivity to current time (atomic store)
2. If state is IDLE, transition to ACTIVE

**Concurrency**: Thread-safe via atomic timestamp

---

### GetIdleTime

**Purpose**: Calculate time since last activity

**Signature**:
```go
func (ec *ExtensionConnection) GetIdleTime() time.Duration
```

**Output**:
- `time.Duration`: Time elapsed since LastActivity

**Behavior**:
1. Load LastActivity timestamp (atomic load)
2. Return time.Since(lastActivity)

**Concurrency**: Thread-safe via atomic timestamp

---

### Cleanup

**Purpose**: Release all resources associated with extension connection

**Signature**:
```go
func (ec *ExtensionConnection) Cleanup() error
```

**Output**:
- `error`: Error if cleanup failed

**Behavior**:
1. Release server-side RTP ports via ExtensionPortManager
2. Close TCP connection
3. Close heartbeat channel (stops heartbeat goroutine)
4. Close shutdown channel (signals cleanup complete)
5. Transition state to DISCONNECTED

**Errors**:
- `ErrPortReleaseFailed`: Server-side port release failed (logged, not fatal)

**Concurrency**: Thread-safe via connection mutex

---

## 4. Extension Auth Manager API

### Authenticate

**Purpose**: Perform authentication handshake with tunnel server

**Signature**:
```go
func (eam *ExtensionAuthManager) Authenticate(conn net.Conn) error
```

**Input**:
- `conn` (net.Conn): TCP connection to tunnel server

**Output**:
- `error`: Authentication failure error

**Behavior**:
1. Generate TC AES key (16 bytes random)
2. Build JSON auth request: `{"tc_aeskey": "..."}`
3. Encrypt with RSA-OAEP using server public key
4. Send authentication request to server
5. Read encrypted response
6. Decrypt response with TC AES key
7. Parse JSON and extract `ts_aeskey`
8. Validate ts_aeskey length (must be 16 bytes)
9. Store ts_aeskey for message encryption

**Errors**:
- `ErrAuthTimeout`: No response within 5 seconds
- `ErrInvalidResponse`: Response JSON parse failed
- `ErrInvalidTSAESKey`: ts_aeskey validation failed

**Concurrency**: Not thread-safe (called once during connection creation)

---

### GetTSAESKey

**Purpose**: Retrieve server AES key for message encryption

**Signature**:
```go
func (eam *ExtensionAuthManager) GetTSAESKey() string
```

**Output**:
- `string`: Server AES key (16-byte hex-encoded)

**Behavior**:
1. Acquire read lock
2. Return tsAESKey
3. Release lock

**Concurrency**: Thread-safe via RWMutex

---

## 5. Extension Port Manager API

### AllocatePortsForCall

**Purpose**: Allocate RTP/RTCP port pairs for new call

**Signature**:
```go
func (epm *ExtensionPortManager) AllocatePortsForCall(hasVideo bool) (audioRTP, audioRTCP, videoRTP, videoRTCP int, error)
```

**Input**:
- `hasVideo` (bool): Whether call includes video stream

**Output**:
- `audioRTP` (int): Audio RTP port
- `audioRTCP` (int): Audio RTCP port (audioRTP + 1)
- `videoRTP` (int): Video RTP port (0 if hasVideo=false)
- `videoRTCP` (int): Video RTCP port (0 if hasVideo=false)
- `error`: Port allocation error

**Behavior**:
1. Allocate consecutive port pair for audio (RTP even, RTCP odd)
2. If hasVideo=true, allocate second port pair for video
3. Update NextAvailablePort
4. Check if port limit reached

**Errors**:
- `ErrPortExhausted`: No available ports in range

**Concurrency**: Thread-safe via port manager mutex

---

### ReleaseCallPorts

**Purpose**: Release RTP/RTCP ports when call terminates

**Signature**:
```go
func (epm *ExtensionPortManager) ReleaseCallPorts(callID string, tsAESKey string) error
```

**Input**:
- `callID` (string): SIP Call-ID to release
- `tsAESKey` (string): Server AES key for port release request

**Output**:
- `error`: Port release error

**Behavior**:
1. Lookup port mapping by Call-ID
2. Collect all TS ports (audio RTP, audio RTCP, video RTP, video RTCP)
3. Send port release request to tunnel server
4. Remove port mapping from map

**Errors**:
- `ErrCallNotFound`: No port mapping for Call-ID
- `ErrPortReleaseFailed`: Server-side release failed

**Concurrency**: Thread-safe via port manager mutex

---

## Error Definitions

```go
// Extension parsing errors
var (
    ErrExtensionNotFound      = errors.New("extension number not found in SIP headers")
    ErrInvalidExtensionFormat = errors.New("extension format invalid (must be 3-5 digits)")
    ErrMalformedSIPMessage    = errors.New("SIP message structure invalid")
)

// Connection pool errors
var (
    ErrCapacityReached      = errors.New("connection pool at maximum capacity")
    ErrConnectionFailed     = errors.New("TCP connection to server failed")
    ErrAuthenticationFailed = errors.New("authentication with server failed")
    ErrExtensionNotFound    = errors.New("extension not found in pool")
    ErrCleanupFailed        = errors.New("resource cleanup failed")
)

// Connection errors
var (
    ErrNotAuthenticated  = errors.New("connection not authenticated")
    ErrEncryptionFailed  = errors.New("message encryption failed")
    ErrTCPWriteFailed    = errors.New("TCP write operation failed")
    ErrUDPSendFailed     = errors.New("UDP send operation failed")
    ErrNoPortMapping     = errors.New("no port mapping found for call")
)

// Auth errors
var (
    ErrAuthTimeout      = errors.New("authentication timeout")
    ErrInvalidResponse  = errors.New("invalid authentication response")
    ErrInvalidTSAESKey  = errors.New("invalid server AES key")
)

// Port management errors
var (
    ErrPortExhausted     = errors.New("no available ports in range")
    ErrCallNotFound      = errors.New("call ID not found in mappings")
    ErrPortReleaseFailed = errors.New("server-side port release failed")
)
```

---

## Usage Example: Complete Flow

```go
// 1. Initialize connection pool
pool := NewConnectionPool(5000)

// 2. Receive SIP message from UDP
sipMsg := receiveSIPFromUDP()

// 3. Parse extension
ext, err := ExtractExtension(sipMsg)
if err != nil {
    log.Printf("Extension parsing failed: %v", err)
    return
}

// 4. Get or create connection
conn, err := pool.GetOrCreate(ext)
if err == ErrCapacityReached {
    sendSIPResponse("SIP/2.0 503 Service Unavailable\r\nRetry-After: 180\r\n")
    return
} else if err != nil {
    log.Printf("Connection creation failed: %v", err)
    return
}

// 5. Forward SIP message
if err := conn.SendSIP(sipMsg, conn.AuthManager.GetTSAESKey()); err != nil {
    log.Printf("SIP forwarding failed: %v", err)
    return
}

// 6. Periodic cleanup (separate goroutine)
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        count := pool.CleanupInactive(30 * time.Minute)
        log.Printf("Cleaned up %d inactive extensions", count)
    }
}()

// 7. Graceful shutdown
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
<-sigChan

if err := pool.ShutdownAll(); err != nil {
    log.Printf("Shutdown error: %v", err)
}
```

---

## Contract Versioning

**Version**: 1.0.0
**Status**: Initial Draft
**Last Updated**: 2025-11-10

**Breaking Changes Policy**:
- Function signature changes require major version bump
- New error types require minor version bump
- Behavior clarifications require patch version bump
