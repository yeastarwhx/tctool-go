package main

import (
	"fmt"
	"log"
	"strings"
)

// Variables for SIP configuration (need to take address)
var (
	LocalIP     = "127.0.0.1"
	ServerSDPIP = "127.0.0.1"
)

// IsSIPRequest checks if message is a SIP request (not a response)
func IsSIPRequest(msg string) bool {
	return !strings.HasPrefix(msg, "SIP/")
}

// IsSIP200OK checks if message is SIP 200 OK response
func IsSIP200OK(msg string) bool {
	return strings.HasPrefix(msg, "SIP/2.0 200")
}

// IsINVITERequest checks if message is INVITE request
func IsINVITERequest(msg string) bool {
	return strings.HasPrefix(msg, "INVITE")
}

// IsBYERequest checks if message is BYE request
func IsBYERequest(msg string) bool {
	return strings.HasPrefix(msg, "BYE")
}

// IsCANCELRequest checks if message is CANCEL request
func IsCANCELRequest(msg string) bool {
	return strings.HasPrefix(msg, "CANCEL")
}

// IsREGISTERRequest checks if message is REGISTER request
func IsREGISTERRequest(msg string) bool {
	return strings.HasPrefix(msg, "REGISTER")
}

// AddLinkusTypeHeader adds LinkusType: LCS header to SIP message
func AddLinkusTypeHeader(sipMsg string) string {
	// Find the position after the first line (Request-Line or Status-Line)
	firstLineEnd := strings.Index(sipMsg, "\r\n")
	if firstLineEnd == -1 {
		// Try with just \n
		firstLineEnd = strings.Index(sipMsg, "\n")
		if firstLineEnd == -1 {
			return sipMsg
		}
		// Insert header after first line
		return sipMsg[:firstLineEnd+1] + "LinkusType: LTS\r\n" + sipMsg[firstLineEnd+1:]
	}

	// Insert header after first line (after \r\n)
	return sipMsg[:firstLineEnd+2] + "LinkusType: LTS\r\n" + sipMsg[firstLineEnd+2:]
}

// ModifyViaPort modifies the port in Via header(s) to the specified port
// Via header format: Via: SIP/2.0/UDP 192.168.1.100:5060;branch=xxx
func ModifyViaPort(sipMsg string, newPort int) string {
	lines := strings.Split(sipMsg, "\r\n")
	var result []string

	for _, line := range lines {
		// Check if this is a Via header line
		if strings.HasPrefix(line, "Via:") || strings.HasPrefix(line, "v:") {
			// Find the port part (look for IP:port pattern before semicolon or end)
			// Pattern: IP:oldPort or IP:oldPort;params

			// Find the position of transport protocol (UDP/TCP/TLS)
			transportIdx := strings.Index(line, "SIP/2.0/")
			if transportIdx == -1 {
				result = append(result, line)
				continue
			}

			// Find the address part after transport
			addressStart := transportIdx + len("SIP/2.0/UDP ")
			if idx := strings.Index(line[transportIdx:], "TCP"); idx != -1 {
				addressStart = transportIdx + len("SIP/2.0/TCP ")
			} else if idx := strings.Index(line[transportIdx:], "TLS"); idx != -1 {
				addressStart = transportIdx + len("SIP/2.0/TLS ")
			}

			// Find where the address part ends (semicolon, space, or end of line)
			remaining := line[addressStart:]
			endIdx := len(remaining)
			if idx := strings.Index(remaining, ";"); idx != -1 {
				endIdx = idx
			}
			if idx := strings.Index(remaining, " "); idx != -1 && idx < endIdx {
				endIdx = idx
			}

			addressPart := remaining[:endIdx]
			afterPart := remaining[endIdx:]

			// Replace port in address part
			// Look for :port pattern
			if colonIdx := strings.LastIndex(addressPart, ":"); colonIdx != -1 {
				// Found port, replace it
				ipPart := addressPart[:colonIdx]
				newLine := line[:addressStart] + ipPart + fmt.Sprintf(":%d", newPort) + afterPart
				result = append(result, newLine)
			} else {
				// No port specified, add it
				newLine := line[:addressStart] + addressPart + fmt.Sprintf(":%d", newPort) + afterPart
				result = append(result, newLine)
			}
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\r\n")
}

// HandleINVITEFromUDP handles INVITE request from UDP (PBX to Server)
func HandleINVITEFromUDP(sipMsg string, pm *PortManager, tsAESKey string, serverType string) (string, error) {
	// Parse Call-ID
	callID, err := ParseCallID(sipMsg)
	if err != nil {
		return sipMsg, fmt.Errorf("failed to parse Call-ID: %w", err)
	}

	// Check if mapping already exists
	mapping := pm.FindMapping(callID)
	if mapping == nil {
		// Parse SDP to check if video is present and get PBX ports
		sdpInfo, err := ParseSDPInfo(sipMsg)
		if err != nil {
			log.Printf("Failed to parse SDP info: %v", err)
			return sipMsg, err
		}

		hasVideo := false
		if sdpInfo.VideoRTP > 0 {
			hasVideo = true
		}

		// Auto-fill RTCP ports if not specified
		audioRTCP := sdpInfo.AudioRTCP
		if audioRTCP == 0 && sdpInfo.AudioRTP > 0 {
			audioRTCP = sdpInfo.AudioRTP + 1
		}
		videoRTCP := sdpInfo.VideoRTCP
		if videoRTCP == 0 && sdpInfo.VideoRTP > 0 {
			videoRTCP = sdpInfo.VideoRTP + 1
		}

		// Request RTP ports from tunnel server
		tsAudioRTP, tsAudioRTCP, tsVideoRTP, tsVideoRTCP, err := RequestRTPPorts(hasVideo, tsAESKey)
		if err != nil {
			log.Printf("[INVITE from UDP] 请求 RTP 端口失败 (callID=%s): %v", callID, err)
			return sipMsg, err
		}

		// Allocate local TC ports
		tcAudioRTP, tcAudioRTCP, tcVideoRTP, tcVideoRTCP, err := pm.AllocatePortsForCall(hasVideo)
		if err != nil {
			return sipMsg, err
		}

		// Create new mapping with both TS and TC ports
		// NOTE: In INVITE from UDP (PBX->Server), the SDP contains caller's ports, not PBX ports
		// Real PBX ports will be updated when we receive 200 OK from TCP
		mapping = &PortMappingNode{
			CallID:        callID,
			ServerType:    serverType,
			TCAudioRTP:    tcAudioRTP,
			TCAudioRTCP:   tcAudioRTCP,
			TCVideoRTP:    tcVideoRTP,
			TCVideoRTCP:   tcVideoRTCP,
			TSAudioRTP:    tsAudioRTP,
			TSAudioRTCP:   tsAudioRTCP,
			TSVideoRTP:    tsVideoRTP,
			TSVideoRTCP:   tsVideoRTCP,
			PBXAudioRTP:   0,  // Will be updated in 200 OK response
			PBXAudioRTCP:  0,  // Will be updated in 200 OK response
			PBXVideoRTP:   0,  // Will be updated in 200 OK response
			PBXVideoRTCP:  0,  // Will be updated in 200 OK response
			PBXAddr:       "", // Will be updated in 200 OK response
			AudioRTPChan:  make(chan bool, 1),
			AudioRTCPChan: make(chan bool, 1),
			VideoRTPChan:  make(chan bool, 1),
			VideoRTCPChan: make(chan bool, 1),
		}

		pm.AddMapping(mapping)
		mapping = pm.FindMapping(callID)

		// Start RTP forwarding for this call
		if mapping != nil {
			err := StartPortForwarding(mapping, tsAESKey)
			if err != nil {
				log.Printf("Failed to start RTP forwarding: %v", err)
			}
		}
	}

	if mapping != nil && mapping.TSAudioRTP > 0 {
		// Modify SIP packet
		modified, err := ModifySIPPacket(
			sipMsg,
			nil,                 // Don't modify Contact host
			0,                   // Don't modify Contact port
			mapping.TSAudioRTP,  // New SDP audio RTP port
			mapping.TSAudioRTCP, // New SDP audio RTCP port
			mapping.TSVideoRTP,  // New SDP video RTP port
			mapping.TSVideoRTCP, // New SDP video RTCP port
			ServerSDPIP,         // New SDP connection address
		)
		if err != nil {
			return sipMsg, err
		}
		return modified, nil
	}

	return sipMsg, nil
}

// Handle200OKFromUDP handles 200 OK response from UDP (PBX to Server)
func Handle200OKFromUDP(sipMsg string, pm *PortManager) (string, error) {
	// Parse Call-ID
	callID, err := ParseCallID(sipMsg)
	if err != nil {
		return sipMsg, fmt.Errorf("failed to parse Call-ID: %w", err)
	}

	mapping := pm.FindMapping(callID)
	if mapping != nil {
		// Modify SIP packet
		modified, err := ModifySIPPacket(
			sipMsg,
			nil,                 // Don't modify Contact host
			0,                   // Don't modify Contact port
			mapping.TSAudioRTP,  // New SDP audio RTP port
			mapping.TSAudioRTCP, // New SDP audio RTCP port
			mapping.TSVideoRTP,  // New SDP video RTP port
			mapping.TSVideoRTCP, // New SDP video RTCP port
			ServerSDPIP,         // New SDP connection address
		)
		if err != nil {
			return sipMsg, err
		}
		return modified, nil
	}

	return sipMsg, nil
}

// HandleINVITEFromTCP handles INVITE request from TCP (Server to PBX)
func HandleINVITEFromTCP(sipMsg string, pm *PortManager, tsAESKey string, serverType string) (string, error) {
	// Parse Call-ID
	callID, err := ParseCallID(sipMsg)
	if err != nil {
		return sipMsg, fmt.Errorf("failed to parse Call-ID: %w", err)
	}

	// Parse SDP
	sdpInfo, err := ParseSDPInfo(sipMsg)
	if err != nil {
		return sipMsg, fmt.Errorf("failed to parse SDP: %w", err)
	}

	// Auto-fill RTCP ports if not specified
	audioRTCP := sdpInfo.AudioRTCP
	if audioRTCP == 0 && sdpInfo.AudioRTP > 0 {
		audioRTCP = sdpInfo.AudioRTP + 1
	}
	videoRTCP := sdpInfo.VideoRTCP
	if videoRTCP == 0 && sdpInfo.VideoRTP > 0 {
		videoRTCP = sdpInfo.VideoRTP + 1
	}

	hasVideo := sdpInfo.VideoRTP > 0

	// Request RTP ports from tunnel server
	tsAudioRTP, tsAudioRTCP, tsVideoRTP, tsVideoRTCP, err := RequestRTPPorts(hasVideo, tsAESKey)
	if err != nil {
		log.Printf("[INVITE from TCP] 请求 RTP 端口失败 (callID=%s): %v", callID, err)
		return sipMsg, err
	}

	// Allocate local TC ports
	tcAudioRTP, tcAudioRTCP, tcVideoRTP, tcVideoRTCP, err := pm.AllocatePortsForCall(hasVideo)
	if err != nil {
		return sipMsg, err
	}

	// Create mapping with both TS and TC ports
	mapping := &PortMappingNode{
		CallID:        callID,
		ServerType:    serverType,
		PBXAddr:       sdpInfo.ConnectionAddr,
		TCAudioRTP:    tcAudioRTP,
		TCAudioRTCP:   tcAudioRTCP,
		TCVideoRTP:    tcVideoRTP,
		TCVideoRTCP:   tcVideoRTCP,
		TSAudioRTP:    tsAudioRTP,
		TSAudioRTCP:   tsAudioRTCP,
		TSVideoRTP:    tsVideoRTP,
		TSVideoRTCP:   tsVideoRTCP,
		PBXAudioRTP:   sdpInfo.AudioRTP,
		PBXAudioRTCP:  audioRTCP,
		PBXVideoRTP:   sdpInfo.VideoRTP,
		PBXVideoRTCP:  videoRTCP,
		AudioRTPChan:  make(chan bool, 1),
		AudioRTCPChan: make(chan bool, 1),
		VideoRTPChan:  make(chan bool, 1),
		VideoRTCPChan: make(chan bool, 1),
	}

	pm.AddMapping(mapping)
	mapping = pm.FindMapping(callID)

	// Start RTP forwarding for this call
	if mapping != nil {
		err := StartPortForwarding(mapping, tsAESKey)
		if err != nil {
			log.Printf("Failed to start RTP forwarding: %v", err)
		}

		// Modify SIP packet
		modified, err := ModifySIPPacket(
			sipMsg,
			&LocalIP,            // Modify Contact host
			LocalUDPPort,        // Modify Contact port
			mapping.TCAudioRTP,  // New SDP audio RTP port
			mapping.TCAudioRTCP, // New SDP audio RTCP port
			mapping.TCVideoRTP,  // New SDP video RTP port
			mapping.TCVideoRTCP, // New SDP video RTCP port
			LocalIP,             // New SDP connection address
		)
		if err != nil {
			return sipMsg, err
		}
		return modified, nil
	}

	return sipMsg, nil
}

// Handle200OKFromTCP handles 200 OK response from TCP (Server to PBX)
func Handle200OKFromTCP(sipMsg string, pm *PortManager) (string, error) {
	// Parse Call-ID
	callID, err := ParseCallID(sipMsg)
	if err != nil {
		return sipMsg, fmt.Errorf("failed to parse Call-ID: %w", err)
	}

	mapping := pm.FindMapping(callID)
	if mapping != nil {
		// Parse SDP to update PBX ports
		sdpInfo, err := ParseSDPInfo(sipMsg)
		if err == nil {
			// Update mapping with PBX ports (use write lock)
			mapping.mutex.Lock()
			mapping.PBXAudioRTP = sdpInfo.AudioRTP
			mapping.PBXAudioRTCP = sdpInfo.AudioRTCP
			if mapping.PBXAudioRTCP == 0 && mapping.PBXAudioRTP > 0 {
				mapping.PBXAudioRTCP = mapping.PBXAudioRTP + 1
			}
			mapping.PBXVideoRTP = sdpInfo.VideoRTP
			mapping.PBXVideoRTCP = sdpInfo.VideoRTCP
			if mapping.PBXVideoRTCP == 0 && mapping.PBXVideoRTP > 0 {
				mapping.PBXVideoRTCP = mapping.PBXVideoRTP + 1
			}
			if sdpInfo.ConnectionAddr != "" {
				mapping.PBXAddr = sdpInfo.ConnectionAddr
			}
			mapping.mutex.Unlock()
		}

		// Modify SIP packet
		modified, err := ModifySIPPacket(
			sipMsg,
			&LocalIP,            // Modify Contact host
			LocalUDPPort,        // Modify Contact port
			mapping.TCAudioRTP,  // New SDP audio RTP port
			mapping.TCAudioRTCP, // New SDP audio RTCP port
			mapping.TCVideoRTP,  // New SDP video RTP port
			mapping.TCVideoRTCP, // New SDP video RTCP port
			LocalIP,             // New SDP connection address
		)
		if err != nil {
			return sipMsg, err
		}
		return modified, nil
	}

	return sipMsg, nil
}

// HandleCallTermination handles call termination (BYE or CANCEL)
func HandleCallTermination(sipMsg string, pm *PortManager, tsAESKey string) {
	callID, err := ParseCallID(sipMsg)
	if err != nil {
		return
	}

	// Get mapping before removing
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
			err := ReleaseRTPPorts(ports, tsAESKey)
			if err != nil {
				log.Printf("Failed to release RTP ports: %v", err)
			}
		}
	}

	pm.RemoveMapping(callID)
}
