package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Configuration constants
const (
	LocalUDPPort = 5060
	ServerIP     = "172.16.17.22"
	ServerPort   = 6060
	BufSize      = 4096
	HeaderSize   = 80
)

// Global state
var (
	globalExit      bool
	globalExitMutex sync.RWMutex
	udpConn         *net.UDPConn
	udpMutex        sync.Mutex
	srcSIPAddr      *net.UDPAddr
	srcMutex        sync.Mutex
	connectionPool  *ConnectionPool // Per-extension connection pool
)

// Global exit flag helpers
func isGlobalExit() bool {
	globalExitMutex.RLock()
	defer globalExitMutex.RUnlock()
	return globalExit
}

func setGlobalExit(val bool) {
	globalExitMutex.Lock()
	defer globalExitMutex.Unlock()
	globalExit = val
}

func main() {
	fmt.Println("========================================")
	fmt.Println("  Tunnel Client (TC) - Go Version")
	fmt.Println("========================================")
	fmt.Println("Local UDP Port: ", LocalUDPPort)
	fmt.Println("Server IP:      ", ServerIP)
	fmt.Println("Server Port:    ", ServerPort)
	fmt.Println("========================================\n")

	// Initialize connection pool for multi-extension support
	connectionPool = NewConnectionPool(MAX_EXTENSIONS)
	fmt.Printf("Connection pool initialized (capacity: %d)\n", MAX_EXTENSIONS)

	// Create managers
	portManager := NewPortManager()
	authManager := NewAuthManager()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create wait group for goroutines
	var wg sync.WaitGroup

	// Start UDP thread (receives SIP messages from PBX)
	wg.Add(1)
	go func() {
		defer wg.Done()
		recvFromPBX(portManager, authManager)
	}()
	fmt.Println("UDP thread started (recvFromPBX)")

	// NOTE: No longer start global recvFromTS thread
	// Each extension now has its own TCP connection with dedicated reader goroutine
	// Connections are created on-demand when SIP messages arrive
	fmt.Println("Multi-extension mode: TCP connections created per extension on-demand")
	fmt.Println("\nTunnel Client is running. Press Ctrl+C to exit.\n")

	// Wait for signal
	<-sigChan
	fmt.Println("\n收到关闭信号，正在优雅退出...")

	// Set global exit flag
	setGlobalExit(true)

	// Cleanup all extension connections
	log.Println("正在关闭所有分机连接...")
	cleanupAllExtensions()

	// Stop all active port mappings
	log.Println("正在停止所有端口映射...")
	portManager.StopAll(authManager.GetTSAESKey())

	// Close UDP connection
	closeUDPConn()

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("所有 goroutines 已正常退出")
	case <-time.After(5 * time.Second):
		log.Println("等待 goroutines 退出超时")
	}

	fmt.Println("\nTunnel Client 已停止")
}

// cleanupAllExtensions closes all extension connections in the pool
func cleanupAllExtensions() {
	if connectionPool == nil {
		return
	}

	count := 0
	connectionPool.Connections.Range(func(key, value interface{}) bool {
		extConn := value.(*ExtensionConnection)

		// Signal shutdown
		select {
		case <-extConn.ShutdownChan:
			// Already closed
		default:
			close(extConn.ShutdownChan)
		}

		// Close TCP connection
		extConn.mu.Lock()
		if extConn.TCPConn != nil {
			extConn.TCPConn.Close()
			extConn.TCPConn = nil
		}
		extConn.mu.Unlock()

		count++
		return true
	})

	log.Printf("已关闭 %d 个分机连接", count)
}

// recvFromPBX receives SIP messages from PBX and forwards to tunnel server
func recvFromPBX(pm *PortManager, am *AuthManager) {
	// Bind UDP socket
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", LocalUDPPort))
	if err != nil {
		log.Printf("Failed to resolve UDP address: %v", err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("Failed to bind UDP socket: %v", err)
		return
	}
	defer conn.Close()

	setUDPConn(conn)

	buf := make([]byte, BufSize)

	for !isGlobalExit() {
		// Set read timeout
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, srcAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if isGlobalExit() {
				break
			}
			log.Printf("UDP read error: %v", err)
			continue
		}

		if n > 16 {
			sipMsg := string(buf[:n])

			// Extract extension number from SIP message
			extNumber, err := ExtractExtension(sipMsg)
			if err != nil {
				log.Printf("Failed to extract extension from SIP message: %v", err)
				log.Printf("SIP message preview (first 500 chars): %s",
					func() string {
						if len(sipMsg) > 500 {
							return sipMsg[:500] + "..."
						}
						return sipMsg
					}())
				continue
			}

			// Get or create extension connection
			ext, err := NewExtension(extNumber)
			if err != nil {
				log.Printf("Invalid extension number %s: %v, message discarded", extNumber, err)
				continue
			}

			extConn, err := connectionPool.GetOrCreate(ext, pm)
			if err != nil {
				if err == ErrCapacityReached {
					log.Printf("Connection pool capacity reached, rejecting extension %s", extNumber)
					// TODO: Send SIP 503 Service Unavailable response
				} else {
					log.Printf("Failed to get/create connection for extension %s: %v", extNumber, err)
				}
				continue
			}

			// Update source address for this extension
			extConn.SetSrcSIPAddr(srcAddr)

			// Process different types of SIP messages
			var modifiedMsg string
			if IsINVITERequest(sipMsg) {
				modifiedMsg, _ = HandleINVITEFromUDP(sipMsg, pm, extConn.AuthManager.GetTSAESKey())
			} else if IsSIP200OK(sipMsg) && HasSDPContent(sipMsg) {
				modifiedMsg, _ = Handle200OKFromUDP(sipMsg, pm)
			} else if IsBYERequest(sipMsg) || IsCANCELRequest(sipMsg) {
				HandleCallTermination(sipMsg, pm, extConn.AuthManager.GetTSAESKey())
				modifiedMsg = sipMsg
			} else {
				modifiedMsg = sipMsg
			}

			// Send to extension's TCP connection
			err = extConn.SendSIP(modifiedMsg, extConn.AuthManager.GetTSAESKey())
			if err != nil {
				log.Printf("Failed to send SIP message for extension %s: %v", extNumber, err)
			}
		}
	}
}

// Helper functions for connection management
func setUDPConn(conn *net.UDPConn) {
	udpMutex.Lock()
	defer udpMutex.Unlock()
	udpConn = conn
}

func closeUDPConn() {
	udpMutex.Lock()
	defer udpMutex.Unlock()
	if udpConn != nil {
		udpConn.Close()
		udpConn = nil
	}
}

func setSrcSIPAddr(addr *net.UDPAddr) {
	srcMutex.Lock()
	defer srcMutex.Unlock()
	srcSIPAddr = addr
}

func getSrcSIPAddr() *net.UDPAddr {
	srcMutex.Lock()
	defer srcMutex.Unlock()
	return srcSIPAddr
}

// sendSIPToUDP sends SIP message to UDP client (global source address - legacy)
func sendSIPToUDP(sipData string) {
	udpMutex.Lock()
	conn := udpConn
	udpMutex.Unlock()

	if conn == nil {
		return
	}

	targetAddr := getSrcSIPAddr()
	if targetAddr == nil {
		return
	}
	log.Printf("Sending SIP to UDP %s:\n%s", targetAddr.String(), sipData)
	_, err := conn.WriteToUDP([]byte(sipData), targetAddr)
	if err != nil {
		log.Printf("Failed to send SIP to UDP: %v", err)
	}
}

// sendSIPToUDPAddr sends SIP message to a specific UDP address
func sendSIPToUDPAddr(sipData string, targetAddr *net.UDPAddr) {
	udpMutex.Lock()
	conn := udpConn
	udpMutex.Unlock()

	if conn == nil {
		return
	}

	if targetAddr == nil {
		return
	}

	log.Printf("Sending SIP to UDP %s:\n%s", targetAddr.String(), sipData)
	_, err := conn.WriteToUDP([]byte(sipData), targetAddr)
	if err != nil {
		log.Printf("Failed to send SIP to UDP: %v", err)
	}
}

// ==================== Auth Manager ====================
// AuthManager manages TC authentication keys
type AuthManager struct {
	tcAESKey string
	tsAESKey string
	mutex    sync.RWMutex
}

// NewAuthManager creates a new AuthManager
func NewAuthManager() *AuthManager {
	return &AuthManager{
		tcAESKey: GenerateAESKey(),
	}
}

// GetTCAESKey returns the TC AES key
func (am *AuthManager) GetTCAESKey() string {
	am.mutex.RLock()
	defer am.mutex.RUnlock()
	return am.tcAESKey
}

// GetTSAESKey returns the TS AES key
func (am *AuthManager) GetTSAESKey() string {
	am.mutex.RLock()
	defer am.mutex.RUnlock()
	return am.tsAESKey
}

// SetTSAESKey sets the TS AES key
func (am *AuthManager) SetTSAESKey(key string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	am.tsAESKey = key
}

// Authenticate performs authentication with TS server
func (am *AuthManager) Authenticate(conn net.Conn) error {
	// Pack and send authentication request
	packed, err := TCPackConnectReqData(am.GetTCAESKey())
	if err != nil {
		return fmt.Errorf("failed to pack connect request: %w", err)
	}

	_, err = conn.Write(packed)
	if err != nil {
		return fmt.Errorf("failed to send connect request: %w", err)
	}

	// Read response header
	_, length, err := TCReadDataHead(conn)
	if err != nil {
		return fmt.Errorf("failed to read response header: %w", err)
	}

	if length <= 0 || length >= 512 {
		return errors.New("invalid response length")
	}

	// Read encrypted response data
	encryptedResp, err := ReadTunnelData(conn, length)
	if err != nil {
		return fmt.Errorf("failed to read response data: %w", err)
	}

	// Decrypt response using tc_aeskey
	decrypted, err := AESDecryptPKCS5(encryptedResp, []byte(am.GetTCAESKey()))
	if err != nil {
		return fmt.Errorf("failed to decrypt response: %w", err)
	}

	// Parse JSON response to extract ts_aeskey
	var data map[string]interface{}
	if err := json.Unmarshal(decrypted, &data); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	tsKey, ok := data["ts_aeskey"].(string)
	if !ok {
		return errors.New("ts_aeskey not found in response")
	}

	if len(tsKey) != 16 {
		return errors.New("invalid ts_aeskey length")
	}

	am.SetTSAESKey(tsKey)
	return nil
}

// ConnectToServer connects to TS server with retry
func ConnectToServer(retryCount int) (net.Conn, error) {
	var conn net.Conn
	var err error

	for i := 0; i <= retryCount; i++ {
		conn, err = net.Dial("tcp", fmt.Sprintf("%s:%d", ServerIP, ServerPort))
		if err == nil {
			return conn, nil
		}
		if i < retryCount {
			// Could add a sleep here for retry delay
		}
	}

	return nil, fmt.Errorf("failed to connect after %d attempts: %w", retryCount+1, err)
}

// AuthenticateWithRetry performs authentication with retry
func (am *AuthManager) AuthenticateWithRetry(conn net.Conn, retryCount int) error {
	// First authentication attempt
	err := am.Authenticate(conn)
	if err == nil {
		return nil
	}

	// Retry if requested
	for i := 0; i < retryCount; i++ {
		err = am.Authenticate(conn)
		if err == nil {
			return nil
		}
	}

	return err
}

// ==================== Port Manager ====================
const (
	TCPortStart = 20000
	TCPortMax   = 40000
)

// PortMappingNode represents a port mapping for a call
type PortMappingNode struct {
	CallID  string
	PBXAddr string

	// Audio ports
	TCAudioRTP   int
	TCAudioRTCP  int
	TSAudioRTP   int
	TSAudioRTCP  int
	PBXAudioRTP  int
	PBXAudioRTCP int

	// Video ports
	TCVideoRTP   int
	TCVideoRTCP  int
	TSVideoRTP   int
	TSVideoRTCP  int
	PBXVideoRTP  int
	PBXVideoRTCP int

	// Thread control
	ShouldExit    bool
	AudioRTPChan  chan bool
	AudioRTCPChan chan bool
	VideoRTPChan  chan bool
	VideoRTCPChan chan bool

	CreatedAt time.Time

	// Mutex to protect PBX port updates
	mutex sync.RWMutex
}

// PortManager manages port mappings
type PortManager struct {
	mappings          map[string]*PortMappingNode
	nextAvailablePort int
	mutex             sync.RWMutex
	portMutex         sync.Mutex
}

// NewPortManager creates a new PortManager instance
func NewPortManager() *PortManager {
	return &PortManager{
		mappings:          make(map[string]*PortMappingNode),
		nextAvailablePort: TCPortStart,
	}
}

// AllocatePortPair allocates a consecutive RTP/RTCP port pair
func (pm *PortManager) AllocatePortPair() (int, int, error) {
	pm.portMutex.Lock()
	defer pm.portMutex.Unlock()

	if pm.nextAvailablePort+1 >= TCPortMax {
		return 0, 0, errors.New("port limit reached")
	}

	rtpPort := pm.nextAvailablePort
	rtcpPort := pm.nextAvailablePort + 1
	pm.nextAvailablePort += 2

	return rtpPort, rtcpPort, nil
}

// AllocatePortsForCall allocates ports for a call (audio + optional video)
func (pm *PortManager) AllocatePortsForCall(hasVideo bool) (audioRTP, audioRTCP, videoRTP, videoRTCP int, err error) {
	// Allocate audio ports
	audioRTP, audioRTCP, err = pm.AllocatePortPair()
	if err != nil {
		return 0, 0, 0, 0, err
	}

	// Allocate video ports if needed
	if hasVideo {
		videoRTP, videoRTCP, err = pm.AllocatePortPair()
		if err != nil {
			return 0, 0, 0, 0, err
		}
	}

	return audioRTP, audioRTCP, videoRTP, videoRTCP, nil
}

// AddMapping adds a new port mapping
func (pm *PortManager) AddMapping(node *PortMappingNode) error {
	if node == nil || node.CallID == "" {
		return errors.New("invalid node or call ID")
	}

	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Check if mapping already exists
	if _, exists := pm.mappings[node.CallID]; exists {
		return nil // Already exists, not an error
	}

	node.CreatedAt = time.Now()
	pm.mappings[node.CallID] = node
	return nil
}

// FindMapping finds a port mapping by Call-ID
func (pm *PortManager) FindMapping(callID string) *PortMappingNode {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	return pm.mappings[callID]
}

// RemoveMapping removes a port mapping by Call-ID
func (pm *PortManager) RemoveMapping(callID string) (*PortMappingNode, error) {
	pm.mutex.Lock()
	node, exists := pm.mappings[callID]
	if !exists {
		pm.mutex.Unlock()
		return nil, errors.New("mapping not found")
	}
	delete(pm.mappings, callID)
	pm.mutex.Unlock()

	// Signal threads to exit
	if node != nil {
		log.Printf("[PortManager] Cleaning up mapping for Call-ID: %s (Audio RTP: %d, RTCP: %d)",
			callID, node.TCAudioRTP, node.TCAudioRTCP)

		node.ShouldExit = true

		// Close channels to wake up goroutines (ignore panic if already closed)
		safeCloseChannel := func(ch chan bool) {
			defer func() {
				recover() // Ignore panic if channel is already closed
			}()
			if ch != nil {
				close(ch)
			}
		}

		safeCloseChannel(node.AudioRTPChan)
		safeCloseChannel(node.AudioRTCPChan)
		safeCloseChannel(node.VideoRTPChan)
		safeCloseChannel(node.VideoRTCPChan)

		// Wait for goroutines to exit (with socket timeout of 500ms, threads will exit within 600ms)
		log.Printf("[PortManager] Waiting %v for goroutines to exit...", GoroutineExitTimeout)
		time.Sleep(GoroutineExitTimeout)
		log.Printf("[PortManager] Port cleanup completed for Call-ID: %s", callID)
	}

	return node, nil
}

// GetAllMappings returns all port mappings
func (pm *PortManager) GetAllMappings() []*PortMappingNode {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	mappings := make([]*PortMappingNode, 0, len(pm.mappings))
	for _, node := range pm.mappings {
		mappings = append(mappings, node)
	}
	return mappings
}

// Count returns the number of active mappings
func (pm *PortManager) Count() int {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return len(pm.mappings)
}

// StopAll stops and removes all port mappings
func (pm *PortManager) StopAll(tsAESKey string) {
	pm.mutex.Lock()
	callIDs := make([]string, 0, len(pm.mappings))
	for callID := range pm.mappings {
		callIDs = append(callIDs, callID)
	}
	pm.mutex.Unlock()

	// Release all mappings
	for _, callID := range callIDs {
		mapping := pm.FindMapping(callID)
		if mapping != nil {
			// Collect TS ports to release
			var ports []int
			if mapping.TSAudioRTP > 0 {
				ports = append(ports, mapping.TSAudioRTP)
			}
			if mapping.TSAudioRTCP > 0 {
				ports = append(ports, mapping.TSAudioRTCP)
			}
			if mapping.TSVideoRTP > 0 {
				ports = append(ports, mapping.TSVideoRTP)
			}
			if mapping.TSVideoRTCP > 0 {
				ports = append(ports, mapping.TSVideoRTCP)
			}

			// Release ports on server
			if len(ports) > 0 {
				ReleaseRTPPorts(ports, tsAESKey)
			}
		}

		// Remove mapping
		pm.RemoveMapping(callID)
	}

	// Wait for goroutines to exit
	time.Sleep(GoroutineExitTimeout + 100*time.Millisecond)
}
