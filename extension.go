package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Global server configuration (reuse from main.go)
var (
	serverIP   = ServerIP
	serverPort = fmt.Sprintf("%d", ServerPort)
)

// ===================================================================
// Constants for Extension Configuration
// ===================================================================

const (
	// MAX_EXTENSIONS defines maximum number of concurrent extension connections
	MAX_EXTENSIONS = 5000

	// IDLE_TIMEOUT defines timeout for inactive extension cleanup (30 minutes)
	IDLE_TIMEOUT = 30 * time.Minute

	// HEARTBEAT_INTERVAL defines interval for sending keepalive messages (2 minutes)
	HEARTBEAT_INTERVAL = 2 * time.Minute

	// IDLE_THRESHOLD defines activity threshold before heartbeat starts (5 minutes)
	IDLE_THRESHOLD = 5 * time.Minute

	// PORT_RANGE_START defines starting port for RTP/RTCP allocation
	PORT_RANGE_START = 20000

	// PORT_RANGE_END defines ending port for RTP/RTCP allocation
	PORT_RANGE_END = 40000

	// MAX_RECONNECT_ATTEMPTS defines maximum TCP reconnection attempts
	MAX_RECONNECT_ATTEMPTS = 3

	// MESSAGE_QUEUE_SIZE defines buffer size for messages during reconnection
	MESSAGE_QUEUE_SIZE = 10

	// MESSAGE_QUEUE_TIMEOUT defines max wait time for queued messages
	MESSAGE_QUEUE_TIMEOUT = 30 * time.Second
)

// ===================================================================
// Error Definitions for Extension Management
// ===================================================================

var (
	// ErrExtensionNotFound indicates no valid extension number found in SIP headers
	ErrExtensionNotFound = errors.New("extension number not found in SIP headers")

	// ErrInvalidExtensionFormat indicates extension doesn't match numeric format (3-5 digits)
	ErrInvalidExtensionFormat = errors.New("extension format invalid (must be 3-5 digits)")

	// ErrMalformedSIPMessage indicates SIP message structure is invalid
	ErrMalformedSIPMessage = errors.New("SIP message structure invalid")

	// ErrCapacityReached indicates connection pool at maximum capacity
	ErrCapacityReached = errors.New("connection pool at maximum capacity")

	// ErrConnectionFailed indicates TCP connection to server failed
	ErrConnectionFailed = errors.New("TCP connection to server failed")

	// ErrAuthenticationFailed indicates authentication with server failed
	ErrAuthenticationFailed = errors.New("authentication with server failed")

	// ErrExtensionConnectionNotFound indicates extension not found in pool
	ErrExtensionConnectionNotFound = errors.New("extension not found in pool")

	// ErrCleanupFailed indicates resource cleanup failed
	ErrCleanupFailed = errors.New("resource cleanup failed")

	// ErrNotAuthenticated indicates connection not authenticated
	ErrNotAuthenticated = errors.New("connection not authenticated")

	// ErrEncryptionFailed indicates message encryption failed
	ErrEncryptionFailed = errors.New("message encryption failed")

	// ErrTCPWriteFailed indicates TCP write operation failed
	ErrTCPWriteFailed = errors.New("TCP write operation failed")

	// ErrUDPSendFailed indicates UDP send operation failed
	ErrUDPSendFailed = errors.New("UDP send operation failed")

	// ErrNoPortMapping indicates no port mapping found for call
	ErrNoPortMapping = errors.New("no port mapping found for call")

	// ErrAuthTimeout indicates authentication timeout
	ErrAuthTimeout = errors.New("authentication timeout")

	// ErrInvalidResponse indicates invalid authentication response
	ErrInvalidResponse = errors.New("invalid authentication response")

	// ErrInvalidTSAESKey indicates invalid server AES key
	ErrInvalidTSAESKey = errors.New("invalid server AES key")

	// ErrPortExhausted indicates no available ports in range
	ErrPortExhausted = errors.New("no available ports in range")

	// ErrCallNotFound indicates call ID not found in mappings
	ErrCallNotFound = errors.New("call ID not found in mappings")

	// ErrPortReleaseFailed indicates server-side port release failed
	ErrPortReleaseFailed = errors.New("server-side port release failed")
)

// ===================================================================
// Type Definitions (to be implemented in Phase 2)
// ===================================================================

// Extension represents a unique extension number with validation
type Extension struct {
	Number  string
	IsValid bool
}

// ConnectionState represents the lifecycle state of an extension connection
type ConnectionState uint32

const (
	DISCONNECTED ConnectionState = iota // No TCP connection exists
	CONNECTING                           // TCP dial in progress, authentication pending
	AUTHENTICATED                        // Handshake complete, AES keys exchanged
	ACTIVE                               // Messages being forwarded, activity <5min
	IDLE                                 // No activity 5-30min, sending heartbeats
	CLEANUP                              // Idle timeout exceeded, closing connection
)

// AuthState represents authentication status
type AuthState uint32

const (
	UNAUTHENTICATED AuthState = iota // Initial state, no authentication attempted
	AUTHENTICATING                    // Authentication request sent, waiting for response
	AUTH_AUTHENTICATED                // Received valid ts_aeskey from server
	FAILED                            // Authentication failed after retries
)

// ExtensionConnection manages TCP connection lifecycle for one extension
type ExtensionConnection struct {
	Extension      Extension
	TCPConn        net.Conn
	State          atomic.Uint32 // ConnectionState
	CreatedAt      time.Time
	LastActivity   atomic.Int64 // Unix timestamp
	HeartbeatChan  chan struct{}
	ShutdownChan   chan struct{}
	AuthManager    *ExtensionAuthManager
	PortManager    *ExtensionPortManager
	GlobalPortMgr  *PortManager // Reference to global port manager for SIP processing
	MessageQueue   chan string  // Buffer for messages during reconnection
	SrcSIPAddr     *net.UDPAddr // Source UDP address for this extension (where to send responses)
	mu             sync.RWMutex
}

// ConnectionPool manages all extension connections
type ConnectionPool struct {
	Connections sync.Map // map[string]*ExtensionConnection
	Capacity    int
	Count       atomic.Int32
	mu          sync.RWMutex
}

// ExtensionAuthManager handles per-extension authentication
type ExtensionAuthManager struct {
	Extension  Extension
	TCAESKey   string
	TSAESKey   string
	AuthState  atomic.Uint32 // AuthState
	RetryCount int
	mu         sync.RWMutex
}

// ExtensionPortManager manages port allocations for one extension
type ExtensionPortManager struct {
	Extension         Extension
	Mappings          map[string]*PortMappingNode
	NextAvailablePort int
	PortRange         PortRange
	mu                sync.RWMutex
}

// PortRange defines allowed port range
type PortRange struct {
	Start    int
	End      int
	MaxPorts int
}

// ===================================================================
// ConnectionPool Implementation
// ===================================================================

// NewConnectionPool creates a new connection pool with specified capacity
func NewConnectionPool(capacity int) *ConnectionPool {
	return &ConnectionPool{
		Connections: sync.Map{},
		Capacity:    capacity,
		Count:       atomic.Int32{},
	}
}

// GetOrCreate retrieves existing extension connection or creates new one
// Returns error if capacity reached
func (cp *ConnectionPool) GetOrCreate(ext Extension, pm *PortManager) (*ExtensionConnection, error) {
	// Fast path: Check existing connection (lock-free)
	if conn, ok := cp.Connections.Load(ext.Number); ok {
		extConn := conn.(*ExtensionConnection)

		// Check if connection is disconnected and needs reconnection
		currentState := ConnectionState(extConn.State.Load())
		if currentState == DISCONNECTED {
			log.Printf("[Extension %s] Reconnecting disconnected connection", ext.Number)
			err := extConn.reconnect()
			if err != nil {
				log.Printf("[Extension %s] Reconnection failed: %v", ext.Number, err)
				return nil, err
			}
		}

		return extConn, nil
	}

	// Check capacity before creation
	if cp.Count.Load() >= int32(cp.Capacity) {
		return nil, ErrCapacityReached
	}

	// Slow path: Create new connection
	conn, err := NewExtensionConnection(ext, pm)
	if err != nil {
		return nil, err
	}

	// Store in map and increment count
	cp.Connections.Store(ext.Number, conn)
	cp.Count.Add(1)

	return conn, nil
}

// Get retrieves existing extension connection without creating new one
// Returns connection and true if found, nil and false otherwise
func (cp *ConnectionPool) Get(extNumber string) (*ExtensionConnection, bool) {
	if conn, ok := cp.Connections.Load(extNumber); ok {
		return conn.(*ExtensionConnection), true
	}
	return nil, false
}

// ===================================================================
// ExtensionConnection Implementation
// ===================================================================

// NewExtensionConnection creates a new extension connection
func NewExtensionConnection(ext Extension, pm *PortManager) (*ExtensionConnection, error) {
	conn := &ExtensionConnection{
		Extension:     ext,
		CreatedAt:     time.Now(),
		HeartbeatChan: make(chan struct{}),
		ShutdownChan:  make(chan struct{}),
		MessageQueue:  make(chan string, MESSAGE_QUEUE_SIZE),
		AuthManager:   NewExtensionAuthManager(ext),
		PortManager:   NewExtensionPortManager(ext),
		GlobalPortMgr: pm, // Store reference to global port manager
	}

	// Initialize state to DISCONNECTED
	conn.State.Store(uint32(DISCONNECTED))
	conn.LastActivity.Store(time.Now().Unix())

	// Connect to tunnel server and authenticate
	err := conn.connectAndAuthenticate()
	if err != nil {
		return nil, err
	}

	// Start TCP reader goroutine for this extension
	go conn.readFromTCP()

	return conn, nil
}

// readFromTCP reads SIP messages from tunnel server for this extension
func (ec *ExtensionConnection) readFromTCP() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Extension %s] TCP reader panic: %v", ec.Extension.Number, r)
		}
	}()

	for {
		select {
		case <-ec.ShutdownChan:
			return
		default:
		}

		// Check if we should exit
		if isGlobalExit() {
			return
		}

		ec.mu.RLock()
		conn := ec.TCPConn
		ec.mu.RUnlock()

		if conn == nil {
			// Connection not available, wait and retry
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Set read timeout
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		// Read header
		tunnelType, length, err := TCReadDataHead(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if isGlobalExit() {
				return
			}

			// Handle EOF or connection errors - cleanup and exit
			if err.Error() == "EOF" || strings.Contains(err.Error(), "connection") {
				log.Printf("[Extension %s] TCP connection closed by server, cleaning up", ec.Extension.Number)
				ec.cleanup()
				return
			}

			log.Printf("[Extension %s] TCP read header error: %v", ec.Extension.Number, err)
			time.Sleep(1 * time.Second)
			continue
		}

		if length <= 0 || length > 4096 {
			continue
		}

		// Read encrypted data
		encryptedData, err := ReadTunnelData(conn, length)
		if err != nil {
			if isGlobalExit() {
				return
			}
			log.Printf("[Extension %s] TCP read data error: %v", ec.Extension.Number, err)
			continue
		}

		// Only process SIP types
		if tunnelType != TunnelSIPLinkus && tunnelType != TunnelSIPIPPhone {
			continue
		}

		// Decrypt data
		tsAESKey := ec.AuthManager.GetTSAESKey()
		if tsAESKey == "" {
			log.Printf("[Extension %s] No TS AES key available", ec.Extension.Number)
			continue
		}

		keyBytes := make([]byte, len(tsAESKey))
		copy(keyBytes, []byte(tsAESKey))

		sipData, err := AESDecryptPKCS5(encryptedData, keyBytes)
		if err != nil {
			log.Printf("[Extension %s] Failed to decrypt SIP data: %v", ec.Extension.Number, err)
			continue
		}

		sipMsg := string(sipData)

		// Update activity
		ec.UpdateActivity()

		// Process different types of SIP messages
		var outputMsg string
		if (IsSIP200OK(sipMsg) || IsINVITERequest(sipMsg)) && HasSDPContent(sipMsg) {
			if IsSIP200OK(sipMsg) {
				outputMsg, _ = Handle200OKFromTCP(sipMsg, ec.GlobalPortMgr)
			} else if IsINVITERequest(sipMsg) {
				outputMsg, _ = HandleINVITEFromTCP(sipMsg, ec.GlobalPortMgr, tsAESKey)
			}
		} else if IsBYERequest(sipMsg) || IsCANCELRequest(sipMsg) {
			HandleCallTermination(sipMsg, ec.GlobalPortMgr, tsAESKey)
			outputMsg = sipMsg
		} else {
			outputMsg = sipMsg
		}

		if outputMsg == "" {
			outputMsg = sipMsg
		}

		// Forward to UDP using this extension's source address
		targetAddr := ec.GetSrcSIPAddr()
		if targetAddr != nil {
			sendSIPToUDPAddr(outputMsg, targetAddr)
		} else {
			log.Printf("[Extension %s] No source address available, cannot send response", ec.Extension.Number)
		}
	}
}

// cleanup closes the TCP connection and removes this connection from the pool
func (ec *ExtensionConnection) cleanup() {
	// Close TCP connection
	ec.mu.Lock()
	if ec.TCPConn != nil {
		ec.TCPConn.Close()
		ec.TCPConn = nil
	}
	ec.mu.Unlock()

	// Transition to DISCONNECTED state
	ec.State.Store(uint32(DISCONNECTED))

	log.Printf("[Extension %s] Connection cleaned up, will be recreated on next SIP message", ec.Extension.Number)
}

// SetSrcSIPAddr sets the source UDP address for this extension
func (ec *ExtensionConnection) SetSrcSIPAddr(addr *net.UDPAddr) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.SrcSIPAddr = addr
}

// GetSrcSIPAddr gets the source UDP address for this extension
func (ec *ExtensionConnection) GetSrcSIPAddr() *net.UDPAddr {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	return ec.SrcSIPAddr
}

// reconnect re-establishes TCP connection and authenticates
func (ec *ExtensionConnection) reconnect() error {
	log.Printf("[Extension %s] Attempting to reconnect...", ec.Extension.Number)

	// Reconnect and authenticate
	err := ec.connectAndAuthenticate()
	if err != nil {
		return err
	}

	// Restart TCP reader goroutine
	go ec.readFromTCP()

	log.Printf("[Extension %s] Successfully reconnected", ec.Extension.Number)
	return nil
}

// connectAndAuthenticate establishes TCP connection and authenticates
func (ec *ExtensionConnection) connectAndAuthenticate() error {
	// Transition to CONNECTING state
	ec.State.Store(uint32(CONNECTING))

	// Connect to tunnel server (use global server config)
	tcpConn, err := net.DialTimeout("tcp", serverIP+":"+serverPort, 5*time.Second)
	if err != nil {
		ec.State.Store(uint32(DISCONNECTED))
		return ErrConnectionFailed
	}

	ec.mu.Lock()
	ec.TCPConn = tcpConn
	ec.mu.Unlock()

	// Authenticate with server
	err = ec.AuthManager.Authenticate(tcpConn)
	if err != nil {
		log.Printf("[Extension %s] Authentication failed: %v", ec.Extension.Number, err)
		tcpConn.Close()
		ec.State.Store(uint32(DISCONNECTED))
		return err
	}

	log.Printf("[Extension %s] Successfully authenticated, TS AES key: %s", ec.Extension.Number, ec.AuthManager.GetTSAESKey())

	// Transition to AUTHENTICATED then ACTIVE
	ec.State.Store(uint32(AUTHENTICATED))
	ec.State.Store(uint32(ACTIVE))

	return nil
}

// UpdateActivity updates last activity timestamp
func (ec *ExtensionConnection) UpdateActivity() {
	ec.LastActivity.Store(time.Now().Unix())

	// If IDLE, transition back to ACTIVE
	currentState := ConnectionState(ec.State.Load())
	if currentState == IDLE {
		ec.State.Store(uint32(ACTIVE))
	}
}

// SendSIP encrypts and sends SIP message through extension's TCP connection
func (ec *ExtensionConnection) SendSIP(msg string, tsAESKey string) error {
	// Check authentication state
	currentState := ConnectionState(ec.State.Load())
	if currentState != ACTIVE && currentState != IDLE && currentState != AUTHENTICATED {
		return ErrNotAuthenticated
	}

	// Update activity
	ec.UpdateActivity()

	// Encrypt message with AES-128-CBC
	encrypted, err := AESEncryptPKCS5([]byte(msg), []byte(tsAESKey))
	if err != nil {
		return ErrEncryptionFailed
	}

	// Pack data with tunnel protocol header
	packed, err := TCPackSIPData(encrypted)
	if err != nil {
		return ErrEncryptionFailed
	}

	// Send via TCP connection
	ec.mu.RLock()
	conn := ec.TCPConn
	ec.mu.RUnlock()

	if conn == nil {
		return ErrConnectionFailed
	}

	_, err = conn.Write(packed)
	if err != nil {
		return ErrTCPWriteFailed
	}

	return nil
}

// ===================================================================
// ExtensionAuthManager Implementation
// ===================================================================

// NewExtensionAuthManager creates a new authentication manager
func NewExtensionAuthManager(ext Extension) *ExtensionAuthManager {
	mgr := &ExtensionAuthManager{
		Extension:  ext,
		RetryCount: 0,
	}
	mgr.AuthState.Store(uint32(UNAUTHENTICATED))
	return mgr
}

// GenerateTCAESKey generates client AES key using crypto/rand
func (eam *ExtensionAuthManager) GenerateTCAESKey() string {
	return generateAESKey()
}

// Authenticate performs authentication handshake with tunnel server
func (eam *ExtensionAuthManager) Authenticate(conn net.Conn) error {
	eam.AuthState.Store(uint32(AUTHENTICATING))

	// Generate TC AES key
	eam.mu.Lock()
	eam.TCAESKey = eam.GenerateTCAESKey()
	tcKey := eam.TCAESKey
	eam.mu.Unlock()

	// Build authentication request (reuse existing auth logic)
	authReq := buildAuthRequest(tcKey)

	// Send authentication request
	_, err := conn.Write(authReq)
	if err != nil {
		eam.AuthState.Store(uint32(FAILED))
		return ErrAuthenticationFailed
	}

	// Read authentication response with timeout
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	response := make([]byte, 4096)
	n, err := conn.Read(response)
	if err != nil {
		eam.AuthState.Store(uint32(FAILED))
		return ErrAuthTimeout
	}

	// Decrypt and parse response
	tsKey, err := parseAuthResponse(response[:n], tcKey)
	if err != nil {
		eam.AuthState.Store(uint32(FAILED))
		log.Printf("[Extension %s] Auth response parse error: %v", eam.Extension.Number, err)
		return ErrInvalidResponse
	}

	// Validate TS AES key
	if len(tsKey) != 16 {
		eam.AuthState.Store(uint32(FAILED))
		return ErrInvalidTSAESKey
	}

	// Store TS AES key
	eam.mu.Lock()
	eam.TSAESKey = tsKey
	eam.mu.Unlock()

	eam.AuthState.Store(uint32(AUTH_AUTHENTICATED))
	return nil
}

// GetTSAESKey returns server AES key (thread-safe)
func (eam *ExtensionAuthManager) GetTSAESKey() string {
	eam.mu.RLock()
	defer eam.mu.RUnlock()
	return eam.TSAESKey
}

// ===================================================================
// ExtensionPortManager Implementation
// ===================================================================

// NewExtensionPortManager creates a new port manager for extension
func NewExtensionPortManager(ext Extension) *ExtensionPortManager {
	return &ExtensionPortManager{
		Extension:         ext,
		Mappings:          make(map[string]*PortMappingNode),
		NextAvailablePort: PORT_RANGE_START,
		PortRange: PortRange{
			Start:    PORT_RANGE_START,
			End:      PORT_RANGE_END,
			MaxPorts: 100, // Each extension can use up to 100 ports
		},
	}
}

// ===================================================================
// Helper Functions
// ===================================================================

// generateAESKey generates a 16-character hex AES key using crypto/rand
// Matches the format of GenerateAESKey() in tunnel.go
func generateAESKey() string {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate AES key: %v", err))
	}
	return fmt.Sprintf("%04x%04x%04x%04x",
		uint16(b[0])<<8|uint16(b[1]),
		uint16(b[2])<<8|uint16(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]))
}

// buildAuthRequest builds authentication request packet
func buildAuthRequest(tcKey string) []byte {
	// Reuse existing TCPackConnectReqData function
	authReq, err := TCPackConnectReqData(tcKey)
	if err != nil {
		panic(fmt.Sprintf("Failed to build auth request: %v", err))
	}
	return authReq
}

// parseAuthResponse parses authentication response and extracts TS AES key
func parseAuthResponse(response []byte, tcKey string) (string, error) {
	// Response format: TCHeadLen (32 bytes) + encrypted JSON data
	if len(response) < TCHeadLen {
		return "", errors.New("response too short")
	}

	encryptedData := response[TCHeadLen:]

	// Decrypt with TC AES key (use raw bytes, not hex-decoded)
	decrypted, err := AESDecryptPKCS5(encryptedData, []byte(tcKey))
	if err != nil {
		return "", fmt.Errorf("failed to decrypt response: %v", err)
	}

	// Parse JSON to extract ts_aeskey
	var authResp struct {
		TSAESKey string `json:"ts_aeskey"`
	}

	err = json.Unmarshal(decrypted, &authResp)
	if err != nil {
		return "", fmt.Errorf("failed to parse JSON response: %v", err)
	}

	if authResp.TSAESKey == "" {
		return "", errors.New("ts_aeskey not found in response")
	}

	return authResp.TSAESKey, nil
}
