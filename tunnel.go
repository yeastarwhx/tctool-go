package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

const (
	SocketTimeout        = 500 * time.Millisecond
	GoroutineExitTimeout = 600 * time.Millisecond
	RTPIdleTimeout       = 5 * time.Minute // 5分钟无RTP数据则超时退出
)

// ==================== Tunnel Data & Encryption ====================
// Constants
const (
	TCHeadLen         = 32
	TunnelDataHeadLen = 16
)

// Tunnel data types
const (
	TunnelDataTypeSIP           = 0
	TunnelDataTypeConnectReq    = 1
	TunnelDataTypeConnectRsp    = 2
	TunnelDataTypeHeartbeat     = 3
	TunnelDataTypeDisconnectReq = 4
)

// TC Tunnel types
const (
	TunnelCtrlConnect     = 1 // Control: Connection
	TunnelSIPLinkus       = 2 // SIP: Linkus
	TunnelSIPIPPhone      = 3 // SIP: IP Phone
	TunnelMediaRTPAlloc   = 4 // Media: RTP allocation
	TunnelMediaRTPRelease = 5 // Media: RTP release
)

// RSA Public Key for encryption
const TunnelDataPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDIvZaeZD+zzv9UeeiHAGV707Th
gFIpQMLX27jgLMB5iUejFdnXrMvFRaAdYr5Hf7is5YVgBwTHPAF72BDh370UiAmE
HUYoTlmMFCCV/4Y8qYaDN4GclhoaKehcZ9psWaNnA3EnVsqDepgEZJCHhgiuN/4V
ZLBAH98WitqHE3SS+wIDAQAB
-----END PUBLIC KEY-----`

// TCData represents the tunnel client data structure
type TCData struct {
	Type    [4]byte
	Len     [4]byte
	Reserve [24]byte
	Data    [4096]byte
}

// TunnelData represents the tunnel data structure
type TunnelData struct {
	Type    [4]byte
	Len     [4]byte
	Reserve [8]byte
	Data    [4096]byte
}

// AESEncryptPKCS5 encrypts data using AES-128-CBC with PKCS5 padding
func AESEncryptPKCS5(data []byte, key []byte) ([]byte, error) {
	if len(data) == 0 || len(key) == 0 {
		return nil, errors.New("invalid data or key")
	}

	block, err := aes.NewCipher(key[:16])
	if err != nil {
		return nil, err
	}

	// PKCS5 padding
	blockSize := aes.BlockSize
	padding := blockSize - len(data)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	data = append(data, padtext...)

	// Create IV from key
	iv := make([]byte, aes.BlockSize)
	copy(iv, key[:aes.BlockSize])

	ciphertext := make([]byte, len(data))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, data)

	return ciphertext, nil
}

// AESDecryptPKCS5 decrypts data using AES-128-CBC with PKCS5 padding
func AESDecryptPKCS5(data []byte, key []byte) ([]byte, error) {
	if len(data) == 0 || len(key) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("invalid encrypted data or key")
	}

	block, err := aes.NewCipher(key[:16])
	if err != nil {
		return nil, err
	}

	// Create IV from key
	iv := make([]byte, aes.BlockSize)
	copy(iv, key[:aes.BlockSize])

	plaintext := make([]byte, len(data))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, data)

	// Remove PKCS5 padding
	length := len(plaintext)
	if length == 0 {
		return nil, errors.New("empty plaintext")
	}
	unpadding := int(plaintext[length-1])
	if unpadding > length || unpadding > aes.BlockSize {
		return nil, errors.New("invalid padding")
	}

	return plaintext[:length-unpadding], nil
}

// AESDecryptRTP decrypts RTP data (special handling for binary data)
func AESDecryptRTP(data []byte, key []byte) ([]byte, error) {
	if len(data) == 0 || len(key) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("invalid encrypted data or key")
	}

	block, err := aes.NewCipher(key[:16])
	if err != nil {
		return nil, err
	}

	// Create IV from key
	iv := make([]byte, aes.BlockSize)
	copy(iv, key[:aes.BlockSize])

	plaintext := make([]byte, len(data))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, data)

	// Remove PKCS5 padding (for binary data)
	length := len(plaintext)
	if length == 0 {
		return nil, errors.New("empty plaintext")
	}
	padLen := int(plaintext[length-1])
	if padLen <= 0 || padLen > aes.BlockSize {
		return nil, errors.New("invalid padding")
	}
	outLen := length - padLen
	if outLen < 0 {
		return nil, errors.New("invalid padding length")
	}

	return plaintext[:outLen], nil
}

// RSAEncryptOAEP encrypts data using RSA with OAEP padding
func RSAEncryptOAEP(data []byte, publicKeyPEM string) ([]byte, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, errors.New("failed to parse PEM block containing the public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	pubKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}

	// RSA OAEP encryption with SHA-1 (to match OpenSSL default)
	hash := sha1.New()
	ciphertext, err := rsa.EncryptOAEP(hash, rand.Reader, pubKey, data, nil)
	if err != nil {
		return nil, err
	}

	return ciphertext, nil
}

// TCPackConnectReqData packs TC connection request data
func TCPackConnectReqData(aesKey string) ([]byte, error) {
	if aesKey == "" {
		return nil, errors.New("aeskey is empty")
	}

	// Build JSON request
	sendData := fmt.Sprintf(`{"tc_aeskey": "%s"}`, aesKey)

	// RSA encrypt
	encrypted, err := RSAEncryptOAEP([]byte(sendData), TunnelDataPublicKey)
	if err != nil {
		return nil, err
	}

	// Pack data
	return tcPackData(TunnelCtrlConnect, encrypted)
}

// TCPackSIPData packs SIP data
func TCPackSIPData(data []byte) ([]byte, error) {
	return TCPackData(TunnelSIPLinkus, data)
}

// TCPackData packs data with specified tunnel type (exported version)
func TCPackData(tunnelType int, data []byte) ([]byte, error) {
	return tcPackData(tunnelType, data)
}

// tcPackData is an internal function to pack TC data
func tcPackData(tunnelType int, data []byte) ([]byte, error) {
	td := &TCData{}
	binary.BigEndian.PutUint32(td.Type[:], uint32(tunnelType))
	binary.BigEndian.PutUint32(td.Len[:], uint32(len(data)))

	// Copy data
	if len(data) > len(td.Data) {
		return nil, errors.New("data too large")
	}
	copy(td.Data[:], data)

	// Build result
	result := make([]byte, TCHeadLen+len(data))
	copy(result[0:4], td.Type[:])
	copy(result[4:8], td.Len[:])
	copy(result[8:32], td.Reserve[:])
	copy(result[32:], data)

	return result, nil
}

// TCReadDataHead reads TC data header from connection
func TCReadDataHead(reader io.Reader) (int, int, error) {
	headBuf := make([]byte, TCHeadLen)
	_, err := io.ReadFull(reader, headBuf)
	if err != nil {
		return 0, 0, err
	}

	tunnelType := int(binary.BigEndian.Uint32(headBuf[0:4]))
	length := int(binary.BigEndian.Uint32(headBuf[4:8]))

	return tunnelType, length, nil
}

// ReadTunnelData reads exact length of data from connection
func ReadTunnelData(reader io.Reader, length int) ([]byte, error) {
	if length <= 0 {
		return nil, errors.New("invalid length")
	}

	buf := make([]byte, length)
	_, err := io.ReadFull(reader, buf)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

// GenerateAESKey generates a random 16-byte AES key
func GenerateAESKey() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%04x%04x%04x%04x",
		binary.BigEndian.Uint16(b[0:2]),
		binary.BigEndian.Uint16(b[2:4]),
		binary.BigEndian.Uint16(b[4:6]),
		binary.BigEndian.Uint16(b[6:8]))
}

// PackTunnelConnectReqData packs tunnel connection request (for legacy support)
func PackTunnelConnectReqData(sn, uuid, aeskey string) ([]byte, error) {
	if sn == "" || uuid == "" || aeskey == "" {
		return nil, errors.New("invalid parameters")
	}

	sendData := fmt.Sprintf("%s:%s:%s:%d", sn, uuid, aeskey, time.Now().Unix())
	encrypted, err := RSAEncryptOAEP([]byte(sendData), TunnelDataPublicKey)
	if err != nil {
		return nil, err
	}

	return packTunnelData(TunnelDataTypeConnectReq, encrypted)
}

// packTunnelData is an internal function to pack tunnel data
func packTunnelData(dataType int, data []byte) ([]byte, error) {
	td := &TunnelData{}
	binary.BigEndian.PutUint32(td.Type[:], uint32(dataType))
	binary.BigEndian.PutUint32(td.Len[:], uint32(len(data)))

	if len(data) > len(td.Data) {
		return nil, errors.New("data too large")
	}
	copy(td.Data[:], data)

	result := make([]byte, TunnelDataHeadLen+len(data))
	copy(result[0:4], td.Type[:])
	copy(result[4:8], td.Len[:])
	copy(result[8:16], td.Reserve[:])
	copy(result[16:], data)

	return result, nil
}

// ==================== RTP Port Management ====================

// RequestRTPPorts requests RTP ports from tunnel server
func RequestRTPPorts(hasVideo bool, tsAESKey string) (audioRTP, audioRTCP, videoRTP, videoRTCP int, err error) {
	// Build JSON request
	reqMsg := fmt.Sprintf(`{"has_video": %d}`, boolToInt(hasVideo))

	// Send request and receive response
	respMsg, err := sendRequestAndReceiveResponse(TunnelMediaRTPAlloc, reqMsg, tsAESKey)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to send RTP request: %w", err)
	}

	// Parse JSON response
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(respMsg), &data); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to parse JSON: %w", err)
	}

	errcode := int(data["errcode"].(float64))
	if errcode != 0 {
		return 0, 0, 0, 0, fmt.Errorf("server returned error: %d", errcode)
	}

	// Parse audio ports
	audioRTP = int(data["audio_rtp"].(float64))
	audioRTCP = int(data["audio_rtcp"].(float64))

	// Parse video ports if has video
	if hasVideo {
		videoRTP = int(data["video_rtp"].(float64))
		videoRTCP = int(data["video_rtcp"].(float64))
	}

	return audioRTP, audioRTCP, videoRTP, videoRTCP, nil
}

// ReleaseRTPPorts releases RTP ports on tunnel server
func ReleaseRTPPorts(ports []int, tsAESKey string) error {
	if len(ports) == 0 {
		return nil
	}

	// Build JSON request
	portsStr := ""
	for i, port := range ports {
		if i > 0 {
			portsStr += ", "
		}
		portsStr += fmt.Sprintf("%d", port)
	}
	reqMsg := fmt.Sprintf(`{"release_ports": [%s]}`, portsStr)

	// Send request and receive response
	respMsg, err := sendRequestAndReceiveResponse(TunnelMediaRTPRelease, reqMsg, tsAESKey)
	if err != nil {
		return fmt.Errorf("failed to send release request: %w", err)
	}

	// Parse JSON response
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(respMsg), &data); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	errcode := int(data["errcode"].(float64))
	if errcode != 0 {
		return fmt.Errorf("server returned error: %d", errcode)
	}

	return nil
}

// sendRequestAndReceiveResponse sends encrypted JSON request to TS and receives response
func sendRequestAndReceiveResponse(tunnelType int, requestJSON string, tsAESKey string) (string, error) {
	// Create TCP connection to server
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", ServerIP, ServerPort))
	if err != nil {
		return "", fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	// Encrypt JSON request with ts_aeskey
	encrypted, err := AESEncryptPKCS5([]byte(requestJSON), []byte(tsAESKey))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt request: %w", err)
	}

	// Pack data with tunnel type
	packed, err := TCPackData(tunnelType, encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to pack request: %w", err)
	}

	// Send request
	_, err = conn.Write(packed)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	// Read response header
	tunnelTypeResp, length, err := TCReadDataHead(conn)
	if err != nil {
		return "", fmt.Errorf("failed to read response header: %w", err)
	}

	_ = tunnelTypeResp // Ignore for now

	if length <= 0 || length >= 4096 {
		return "", fmt.Errorf("invalid response length: %d", length)
	}

	// Read encrypted response data
	encryptedResp, err := ReadTunnelData(conn, length)
	if err != nil {
		return "", fmt.Errorf("failed to read response data: %w", err)
	}

	// Decrypt response using ts_aeskey
	decrypted, err := AESDecryptPKCS5(encryptedResp, []byte(tsAESKey))
	if err != nil {
		return "", fmt.Errorf("failed to decrypt response: %w", err)
	}

	return string(decrypted), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ==================== RTP Forwarding ====================

// PortType constants for identifying which port is being forwarded
const (
	PortTypeAudioRTP  = "audio_rtp"
	PortTypeAudioRTCP = "audio_rtcp"
	PortTypeVideoRTP  = "video_rtp"
	PortTypeVideoRTCP = "video_rtcp"
)

// StartPortForwarding starts RTP/RTCP forwarding for a port mapping node
func StartPortForwarding(node *PortMappingNode, tsAESKey string) error {
	if node == nil {
		return fmt.Errorf("node is nil")
	}

	// Start audio RTP forwarding
	if node.TCAudioRTP > 0 {
		go handlePortForwarding(node, node.TCAudioRTP, PortTypeAudioRTP, tsAESKey)
	}

	// Start audio RTCP forwarding
	if node.TCAudioRTCP > 0 {
		go handlePortForwarding(node, node.TCAudioRTCP, PortTypeAudioRTCP, tsAESKey)
	}

	// Start video RTP forwarding
	if node.TCVideoRTP > 0 {
		go handlePortForwarding(node, node.TCVideoRTP, PortTypeVideoRTP, tsAESKey)
	}

	// Start video RTCP forwarding
	if node.TCVideoRTCP > 0 {
		go handlePortForwarding(node, node.TCVideoRTCP, PortTypeVideoRTCP, tsAESKey)
	}

	return nil
}

// handlePortForwarding handles RTP/RTCP packet forwarding for a specific port
// Automatically exits if no data received for RTPIdleTimeout (5 minutes)
func handlePortForwarding(node *PortMappingNode, localPort int,
	portType string, tsAESKey string) {

	// Create UDP socket
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", localPort))
	if err != nil {
		log.Printf("[%s] 解析 UDP 地址失败 port=%d: %v", portType, localPort, err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("[%s] 绑定 UDP socket 失败 port=%d: %v", portType, localPort, err)
		return
	}
	defer func() {
		conn.Close()
		log.Printf("[%s] UDP socket closed for port %d", portType, localPort)
	}()

	// Initialize last activity time
	lastActivityTime := time.Now()

	// Set socket timeout
	conn.SetReadDeadline(time.Now().Add(SocketTimeout))

	buf := make([]byte, BufSize)
	var sourcePort int
	var sourceIP string

	for !node.ShouldExit {
		// Check idle timeout (5 minutes without data)
		if time.Since(lastActivityTime) > RTPIdleTimeout {
			log.Printf("[%s] RTP端口 %d 空闲超过 %v，自动退出线程",
				portType, localPort, RTPIdleTimeout)
			break
		}

		// Update deadline
		conn.SetReadDeadline(time.Now().Add(SocketTimeout))

		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if node.ShouldExit {
				break
			}
			continue
		}

		if n <= 0 {
			continue
		}

		// Update activity time when data is received
		node.mutex.Lock()
		node.LastActivityAt = time.Now()
		lastActivityTime = node.LastActivityAt
		node.mutex.Unlock()

		clientIP := clientAddr.IP.String()
		clientPort := clientAddr.Port

		// Check if from local client (not from tunnel server)
		// If the packet is NOT from the tunnel server IP, it's from local client
		isLocalClient := clientIP != ServerIP

		if isLocalClient {
			// Packet from local client, forward to server

			// Record source on first packet
			if sourcePort == 0 {
				sourcePort = clientPort
				sourceIP = clientIP
				log.Printf("[%s] 首次收到RTP数据 port=%d from %s:%d",
					portType, localPort, sourceIP, sourcePort)
			}

			// Read the latest PBX info and port mappings from node
			// This ensures we use updated values after 200 OK is received
			node.mutex.RLock()
			pbxIP := node.PBXAddr
			serverType := node.ServerType
			var tsFwdPort, pbxPort int

			// Determine which port this is and get corresponding TS and PBX ports
			switch portType {
			case PortTypeAudioRTP:
				tsFwdPort = node.TSAudioRTP
				pbxPort = node.PBXAudioRTP
			case PortTypeAudioRTCP:
				tsFwdPort = node.TSAudioRTCP
				pbxPort = node.PBXAudioRTCP
			case PortTypeVideoRTP:
				tsFwdPort = node.TSVideoRTP
				pbxPort = node.PBXVideoRTP
			case PortTypeVideoRTCP:
				tsFwdPort = node.TSVideoRTCP
				pbxPort = node.PBXVideoRTCP
			}
			node.mutex.RUnlock()

			// Skip if PBX port is not yet set (waiting for 200 OK)
			if pbxPort == 0 || pbxIP == "" {
				continue
			}
			// Only set pbxIP to 127.0.0.1 if server type is not "sbc"
			if serverType != "sbc" {
				pbxIP = "127.0.0.1"
			}
			// Build RTP packet header
			// Format: #pbx_ip=%s#ts_fwd_port=%d#pbx_port=%d#
			// - pbx_ip:       PBX IP address (from SDP c=IN IP4 line)
			// - ts_fwd_port:  TS forwarding port (port TS uses to send packets)
			// - pbx_port:     PBX receiving port (port where PBX receives packets)
			header := fmt.Sprintf("#pbx_ip=%s#ts_fwd_port=%d#pbx_port=%d#",
				pbxIP, tsFwdPort, pbxPort)

			// Ensure header is exactly 80 bytes
			headerBytes := make([]byte, HeaderSize)
			copy(headerBytes, header)
			log.Printf("[%s] 转发 RTP 数据到服务器 port=%d pbx_ip=%s ts_fwd_port=%d pbx_port=%d size=%d", portType, localPort, pbxIP, tsFwdPort, pbxPort, n)

			// Create combined buffer (header + RTP data)
			combined := make([]byte, HeaderSize+n)
			copy(combined[0:HeaderSize], headerBytes)
			copy(combined[HeaderSize:], buf[:n])

			// Encrypt the entire combined buffer
			encrypted, err := AESEncryptPKCS5(combined, []byte(tsAESKey))
			if err != nil {
				log.Printf("[%s] RTP 数据加密失败: %v", portType, err)
				continue
			}

			// Send to tunnel server
			serverAddr, err := net.ResolveUDPAddr("udp",
				fmt.Sprintf("%s:%d", ServerIP, ServerPort))
			if err != nil {
				continue
			}

			_, err = conn.WriteToUDP(encrypted, serverAddr)
			if err != nil {
				log.Printf("[%s] 发送到服务器失败: %v", portType, err)
			}

		} else {
			// Packet from server, forward to original client
			if sourcePort > 0 && sourceIP != "" {
				// Decrypt RTP data
				decrypted, err := AESDecryptRTP(buf[:n], []byte(tsAESKey))
				if err != nil {
					continue
				}

				// Forward to original local client
				localClientAddr, err := net.ResolveUDPAddr("udp",
					fmt.Sprintf("%s:%d", sourceIP, sourcePort))
				if err != nil {
					continue
				}

				_, err = conn.WriteToUDP(decrypted, localClientAddr)
				if err != nil {
					log.Printf("[%s] 发送到客户端失败: %v", portType, err)
				}
			}
		}
	}

	// Thread exit: Clean up port mapping if idle timeout occurred
	if time.Since(lastActivityTime) > RTPIdleTimeout {
		log.Printf("[%s] 因空闲超时退出，正在清理端口映射 CallID=%s",
			portType, node.CallID)
		// Note: Actual cleanup happens in PortManager.RemoveMapping
		// which is called when BYE/CANCEL is received or by idle cleanup goroutine
	}
}
