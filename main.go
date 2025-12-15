package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Configuration constants (immutable)
const (
	BufSize    = 4096
	HeaderSize = 80
)

// Configuration variables (can be set via command line flags)
var (
	LocalUDPPort   int
	ServerIP       string
	ServerPort     int
	DebugMode      bool // Control whether to output log messages
	PortRangeStart int  // Starting port for RTP/RTCP allocation
	PortRangeSize  int  // Size of port range from start
)

// Global state
var (
	udpConn        *net.UDPConn
	udpMutex       sync.Mutex
	connectionPool *ConnectionPool // Per-extension connection pool
)

func main() {
	// Define command line flags (no defaults for required params)
	localPort := flag.Int("local-port", 0, "Local UDP port for SIP (required)")
	localIP := flag.String("local-ip", "", "Local IP address for SIP Contact (required)")
	serverIP := flag.String("server", "", "Tunnel server IP address (required)")
	serverPort := flag.Int("port", 0, "Tunnel server port (required)")
	portRangeStart := flag.Int("rtp-start-port", 20000, "Starting port for RTP/RTCP allocation (default: 20000)")
	portRangeSize := flag.Int("rtp-port-range", 10000, "Port range size from start port (default: 10000)")
	debug := flag.Bool("debug", false, "Enable debug log output (default: false)")

	flag.Parse()

	// Validate required parameters
	if *localPort == 0 {
		fmt.Println("错误: 缺少必需参数 -local-port")
		fmt.Println("\n使用方法:")
		fmt.Println("  ./tctool -local-port=<端口> -local-ip=<IP地址> -server=<服务器IP> -port=<端口> [-rtp-start-port=<起始端口>] [-rtp-port-range=<范围>] [-debug]")
		fmt.Println("\n示例:")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090 -rtp-start-port=20000 -rtp-port-range=10000")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090 -debug")
		fmt.Println("\n运行 './tctool -h' 查看所有参数")
		os.Exit(1)
	}

	if *localIP == "" {
		fmt.Println("错误: 缺少必需参数 -local-ip")
		fmt.Println("\n使用方法:")
		fmt.Println("  ./tctool -local-port=<端口> -local-ip=<IP地址> -server=<服务器IP> -port=<端口> [-rtp-start-port=<起始端口>] [-rtp-port-range=<范围>] [-debug]")
		fmt.Println("\n示例:")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090 -rtp-start-port=20000 -rtp-port-range=10000")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090 -debug")
		fmt.Println("\n运行 './tctool -h' 查看所有参数")
		os.Exit(1)
	}

	if *serverIP == "" {
		fmt.Println("错误: 缺少必需参数 -server")
		fmt.Println("\n使用方法:")
		fmt.Println("  ./tctool -local-port=<端口> -local-ip=<IP地址> -server=<服务器IP> -port=<端口> [-rtp-start-port=<起始端口>] [-rtp-port-range=<范围>] [-debug]")
		fmt.Println("\n示例:")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090 -rtp-start-port=20000 -rtp-port-range=10000")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090 -debug")
		fmt.Println("\n运行 './tctool -h' 查看所有参数")
		os.Exit(1)
	}

	if *serverPort == 0 {
		fmt.Println("错误: 缺少必需参数 -port")
		fmt.Println("\n使用方法:")
		fmt.Println("  ./tctool -local-port=<端口> -local-ip=<IP地址> -server=<服务器IP> -port=<端口> [-rtp-start-port=<起始端口>] [-rtp-port-range=<范围>] [-debug]")
		fmt.Println("\n示例:")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090 -rtp-start-port=20000 -rtp-port-range=10000")
		fmt.Println("  ./tctool -local-port=5060 -local-ip=127.0.0.1 -server=172.16.17.22 -port=1090 -debug")
		fmt.Println("\n运行 './tctool -h' 查看所有参数")
		os.Exit(1)
	}

	// Set global configuration variables
	LocalUDPPort = *localPort
	LocalIP = *localIP
	ServerIP = *serverIP
	ServerPort = *serverPort
	DebugMode = *debug
	PortRangeStart = *portRangeStart
	PortRangeSize = *portRangeSize

	// Validate port range
	if PortRangeSize < 100 {
		fmt.Println("错误: RTP端口范围太小，至少需要100个端口")
		os.Exit(1)
	}
	portRangeEnd := PortRangeStart + PortRangeSize
	if portRangeEnd > 65535 {
		fmt.Println("错误: RTP端口范围超出有效范围 (最大65535)")
		os.Exit(1)
	}

	// Configure log output based on debug mode
	if !DebugMode {
		log.SetOutput(io.Discard) // Disable all log output
	}

	// Normal mode
	fmt.Println("========================================")
	fmt.Println("  Tunnel Client (TC) - Go Version")
	fmt.Println("========================================")
	fmt.Println("Local UDP Port: ", LocalUDPPort)
	fmt.Println("Local IP:       ", LocalIP)
	fmt.Println("Server IP:      ", ServerIP)
	fmt.Println("Server Port:    ", ServerPort)
	fmt.Println("RTP Port Range: ", fmt.Sprintf("%d-%d (%d ports)", PortRangeStart, portRangeEnd-1, PortRangeSize))
	fmt.Println("Debug Mode:     ", DebugMode)
	fmt.Println("========================================")

	// Initialize connection pool for multi-extension support
	connectionPool = NewConnectionPool(MAX_EXTENSIONS)
	fmt.Printf("Connection pool initialized (capacity: %d)\n", MAX_EXTENSIONS)

	// Create managers
	portManager := NewPortManager()
	authManager := NewAuthManager()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create shutdown context channel
	shutdownChan := make(chan struct{})

	// Create wait group for goroutines
	var wg sync.WaitGroup

	// Start UDP thread (receives SIP messages from local)
	wg.Add(1)
	go func() {
		defer wg.Done()
		recvFromLocal(portManager, authManager, shutdownChan)
	}()
	fmt.Println("UDP thread started (recvFromLocal)")

	// Start idle port mapping cleanup goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		cleanupIdlePortMappings(portManager, authManager, shutdownChan)
	}()
	fmt.Println("Idle port mapping cleanup goroutine started")

	// NOTE: No longer start global recvFromTS thread
	// Each extension now has its own TCP connection with dedicated reader goroutine
	// Connections are created on-demand when SIP messages arrive
	fmt.Println("Multi-extension mode: TCP connections created per extension on-demand")
	fmt.Println("Tunnel Client is running. Press Ctrl+C to exit.")

	// Wait for signal
	<-sigChan
	fmt.Println("\n收到关闭信号，正在优雅退出...")

	// Signal shutdown to all goroutines
	close(shutdownChan)

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

// recvFromLocal receives SIP messages from local network and forwards to tunnel server
func recvFromLocal(pm *PortManager, am *AuthManager, shutdownChan <-chan struct{}) {
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

	for {
		// Check shutdown signal
		select {
		case <-shutdownChan:
			return
		default:
		}

		// Set read timeout
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, srcAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// Check shutdown again after error
			select {
			case <-shutdownChan:
				return
			default:
			}
			log.Printf("UDP read error: %v", err)
			continue
		}

		if n > 16 {
			sipMsg := string(buf[:n])
			log.Printf(">>> Received SIP from Local %s\n%s", srcAddr.String(), sipMsg)
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
			log.Printf("########extension#########: %v\n\n", extNumber)
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
			var shouldAsyncCleanup bool
			var cleanupCallID string
			var cleanupAESKey string

			if IsINVITERequest(sipMsg) {
				modifiedMsg, _ = HandleINVITEFromUDP(sipMsg, pm, extConn.AuthManager.GetTSAESKey(), extConn.AuthManager.GetServerType())
			} else if IsSIP200OK(sipMsg) {
				modifiedMsg, _ = Handle200OKFromUDP(sipMsg, pm)
			} else if IsBYERequest(sipMsg) || IsCANCELRequest(sipMsg) {
				// Don't block on cleanup - just prepare to do it async after sending
				modifiedMsg = sipMsg
				callID, err := ParseCallID(sipMsg)
				if err == nil {
					shouldAsyncCleanup = true
					cleanupCallID = callID
					cleanupAESKey = extConn.AuthManager.GetTSAESKey()
				}
			} else {
				modifiedMsg = sipMsg
			}

			// Add LinkusType header for REGISTER and INVITE requests
			if IsREGISTERRequest(sipMsg) || IsINVITERequest(sipMsg) {
				modifiedMsg = AddLinkusTypeHeader(modifiedMsg)
			}

			log.Printf("<<< Send to TS [extension %s]\n%s", extNumber, modifiedMsg)
			// Send to extension's TCP connection FIRST (don't block on cleanup)
			err = extConn.SendSIP(modifiedMsg, extConn.AuthManager.GetTSAESKey())
			if err != nil {
				log.Printf("Failed to send SIP message for extension %s: %v", extNumber, err)
			}

			// AFTER sending, do async cleanup if it was BYE/CANCEL
			if shouldAsyncCleanup {
				go func(callID string, aesKey string) {
					HandleCallTermination(sipMsg, pm, aesKey)
				}(cleanupCallID, cleanupAESKey)
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
		conn, err = net.Dial("tcp", net.JoinHostPort(ServerIP, strconv.Itoa(ServerPort)))
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

// PortMappingNode represents a port mapping for a call
type PortMappingNode struct {
	CallID     string
	PBXAddr    string
	ServerType string // Server type from authentication response (e.g., "sbc")

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
	wg            sync.WaitGroup // Track goroutine lifecycle

	CreatedAt      time.Time
	LastActivityAt time.Time // 最后一次RTP数据活动时间

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
		nextAvailablePort: PortRangeStart,
	}
}

// AllocatePortPair allocates a consecutive RTP/RTCP port pair
func (pm *PortManager) AllocatePortPair() (int, int, error) {
	pm.portMutex.Lock()
	defer pm.portMutex.Unlock()

	// Use dynamic port range from configuration
	rangeEnd := PortRangeStart + PortRangeSize

	// Try to allocate a pair of consecutive ports
	maxAttempts := PortRangeSize / 2 // Try at most half the range
	attempts := 0

	for attempts < maxAttempts {
		// Ensure nextAvailablePort is within range
		if pm.nextAvailablePort < PortRangeStart || pm.nextAvailablePort >= rangeEnd {
			pm.nextAvailablePort = PortRangeStart
		}

		// Try to find an available RTP port
		rtpPort := allocateAvailablePort(pm.nextAvailablePort)
		if rtpPort == 0 {
			// No available port found in entire range
			return 0, 0, errors.New("no available ports in configured range")
		}

		// Check if the next port (RTCP) is also available
		rtcpPort := rtpPort + 1
		if rtcpPort >= rangeEnd {
			// RTCP port would be out of range, try next port
			pm.nextAvailablePort = rtpPort + 2
			if pm.nextAvailablePort >= rangeEnd {
				pm.nextAvailablePort = PortRangeStart
			}
			attempts++
			continue
		}

		if isPortAvailable(rtcpPort) {
			// Both ports available, update next available and return
			pm.nextAvailablePort = rtcpPort + 1
			if pm.nextAvailablePort >= rangeEnd {
				pm.nextAvailablePort = PortRangeStart
			}

			if DebugMode {
				log.Printf("[PortManager] Allocated port pair: RTP=%d, RTCP=%d", rtpPort, rtcpPort)
			}

			return rtpPort, rtcpPort, nil
		}

		// RTCP port not available, try next pair
		pm.nextAvailablePort = rtpPort + 2
		if pm.nextAvailablePort >= rangeEnd {
			pm.nextAvailablePort = PortRangeStart
		}
		attempts++
	}

	return 0, 0, errors.New("failed to allocate port pair after maximum attempts")
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

	now := time.Now()
	node.CreatedAt = now
	node.LastActivityAt = now // Initialize with creation time
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
		if DebugMode {
			log.Printf("[PortManager] Call-ID %s not found (may be already cleaned)", callID)
		}
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

		// Wait for all goroutines to exit gracefully using WaitGroup
		log.Printf("[PortManager] Waiting for goroutines to exit...")
		node.wg.Wait()
		log.Printf("[PortManager] All goroutines exited. Port cleanup completed for Call-ID: %s", callID)
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

		// Remove mapping (this will wait for goroutines using WaitGroup)
		pm.RemoveMapping(callID)
	}

	// No need for additional wait, RemoveMapping already waits for each node's goroutines
	log.Printf("[PortManager] All mappings stopped")
}

// cleanupIdlePortMappings periodically checks and cleans up idle port mappings
// that haven't received RTP data for more than RTPIdleTimeout (5 minutes)
func cleanupIdlePortMappings(pm *PortManager, am *AuthManager, shutdownChan <-chan struct{}) {
	// Check every minute
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-shutdownChan:
			return
		case <-ticker.C:
			// Get all mappings
			mappings := pm.GetAllMappings()

			for _, mapping := range mappings {
				// Check if mapping is idle
				mapping.mutex.RLock()
				lastActivity := mapping.LastActivityAt
				callID := mapping.CallID
				mapping.mutex.RUnlock()

				// Calculate idle time
				idleTime := time.Since(lastActivity)

				// If idle for more than RTPIdleTimeout, clean it up
				if idleTime > RTPIdleTimeout {
					log.Printf("[IdleCleanup] 端口映射空闲超过 %v (CallID=%s, idle=%v)，正在清理...",
						RTPIdleTimeout, callID, idleTime)

					// Get mapping again to collect ports
					node := pm.FindMapping(callID)
					if node != nil {
						// Collect TS ports to release
						var ports []int
						node.mutex.RLock()
						if node.TSAudioRTP > 0 {
							ports = append(ports, node.TSAudioRTP)
						}
						if node.TSAudioRTCP > 0 {
							ports = append(ports, node.TSAudioRTCP)
						}
						if node.TSVideoRTP > 0 {
							ports = append(ports, node.TSVideoRTP)
						}
						if node.TSVideoRTCP > 0 {
							ports = append(ports, node.TSVideoRTCP)
						}
						node.mutex.RUnlock()

						// Release ports on server
						if len(ports) > 0 {
							err := ReleaseRTPPorts(ports, am.GetTSAESKey())
							if err != nil {
								log.Printf("[IdleCleanup] 释放服务器端口失败 (CallID=%s): %v", callID, err)
							}
						}

						// Remove mapping (this will also trigger goroutine exit)
						pm.RemoveMapping(callID)
						log.Printf("[IdleCleanup] 端口映射已清理 (CallID=%s)", callID)
					}
				}
			}
		}
	}
}

// ==================== Port Availability Check ====================

// isPortAvailable checks if a UDP port is available for binding
func isPortAvailable(port int) bool {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return false
	}

	conn.Close()
	return true
}

// allocateAvailablePort finds an available port within the configured range
// Returns the allocated port or 0 if no port is available
func allocateAvailablePort(startFrom int) int {
	rangeEnd := PortRangeStart + PortRangeSize

	// Try from startFrom to end of range
	for port := startFrom; port < rangeEnd; port++ {
		if isPortAvailable(port) {
			return port
		}
	}

	// Wrap around: try from beginning to startFrom
	for port := PortRangeStart; port < startFrom; port++ {
		if isPortAvailable(port) {
			return port
		}
	}

	// No available port found
	return 0
}
