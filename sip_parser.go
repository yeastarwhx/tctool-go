package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/emiago/sipgo/sip"
)

// SDPInfo contains SDP information extracted from SIP message
type SDPInfo struct {
	AudioRTP       int
	AudioRTCP      int
	VideoRTP       int
	VideoRTCP      int
	ConnectionAddr string
}

// ParseCallID extracts Call-ID from SIP message using sipgo
func ParseCallID(sipMsg string) (string, error) {
	msg, err := sip.ParseMessage([]byte(sipMsg))
	if err != nil {
		return "", fmt.Errorf("failed to parse SIP message: %w", err)
	}

	callID := msg.CallID()
	if callID == nil {
		return "", errors.New("Call-ID header not found")
	}

	return callID.Value(), nil
}

// ParseSDPInfo extracts SDP information from SIP message using sipgo
func ParseSDPInfo(sipMsg string) (*SDPInfo, error) {
	msg, err := sip.ParseMessage([]byte(sipMsg))
	if err != nil {
		return nil, fmt.Errorf("failed to parse SIP message: %w", err)
	}

	// Get message body (SDP content)
	body := msg.Body()
	if len(body) == 0 {
		return nil, errors.New("no SDP content in message")
	}

	sdpInfo := &SDPInfo{}
	lines := strings.Split(string(body), "\n")

	var currentMediaType string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse connection address (c=IN IP4 192.168.1.1)
		if strings.HasPrefix(line, "c=") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				sdpInfo.ConnectionAddr = parts[2]
			}
		}

		// Parse media line (m=audio 20000 RTP/AVP 0 8)
		if strings.HasPrefix(line, "m=") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				currentMediaType = parts[0][2:] // Remove "m="
				port, err := strconv.Atoi(parts[1])
				if err == nil {
					if currentMediaType == "audio" {
						sdpInfo.AudioRTP = port
					} else if currentMediaType == "video" {
						sdpInfo.VideoRTP = port
					}
				}
			}
		}

		// Parse RTCP attribute (a=rtcp:20001)
		if strings.HasPrefix(line, "a=rtcp:") {
			portStr := strings.TrimPrefix(line, "a=rtcp:")
			// Handle "a=rtcp:20001 IN IP4 192.168.1.1" format
			parts := strings.Fields(portStr)
			if len(parts) > 0 {
				port, err := strconv.Atoi(parts[0])
				if err == nil {
					if currentMediaType == "audio" {
						sdpInfo.AudioRTCP = port
					} else if currentMediaType == "video" {
						sdpInfo.VideoRTCP = port
					}
				}
			}
		}
	}

	return sdpInfo, nil
}

// ModifySIPPacket modifies SIP packet's Contact and SDP fields
func ModifySIPPacket(
	sipMsg string,
	contactHost *string,
	contactPort int,
	audioRTP int,
	audioRTCP int,
	videoRTP int,
	videoRTCP int,
	sdpAddr string,
) (string, error) {
	msg, err := sip.ParseMessage([]byte(sipMsg))
	if err != nil {
		return "", fmt.Errorf("failed to parse SIP message: %w", err)
	}

	// Modify Contact header if contactHost is provided
	if contactHost != nil && *contactHost != "" {
		// Get existing contact header using GetHeaders
		contactHeaders := msg.GetHeaders("Contact")
		if len(contactHeaders) > 0 {
			// Parse the first Contact header
			contactHdr, ok := contactHeaders[0].(*sip.ContactHeader)
			if ok {
				// Create modified contact address
				newAddr := contactHdr.Address.Clone()
				newAddr.Host = *contactHost
				if contactPort > 0 {
					newAddr.Port = contactPort
				}

				// Create new Contact header
				newContact := &sip.ContactHeader{
					DisplayName: contactHdr.DisplayName,
					Address:     *newAddr,
					Params:      contactHdr.Params,
				}

				// Build new SIP message with modified Contact
				msg = rebuildMessageWithContact(msg, newContact)
			}
		}
	}

	// Modify SDP content
	body := msg.Body()
	if len(body) > 0 {
		newBody := modifySDPContent(
			string(body),
			audioRTP,
			audioRTCP,
			videoRTP,
			videoRTCP,
			sdpAddr,
		)
		msg.SetBody([]byte(newBody))
	}

	return msg.String(), nil
}

// rebuildMessageWithContact rebuilds the message string with new Contact header
func rebuildMessageWithContact(msg sip.Message, newContact *sip.ContactHeader) sip.Message {
	// Get the original message string
	msgStr := msg.String()

	// Find and replace Contact header
	lines := strings.Split(msgStr, "\r\n")
	var newLines []string
	contactReplaced := false

	for _, line := range lines {
		if strings.HasPrefix(line, "Contact:") || strings.HasPrefix(line, "m:") {
			if !contactReplaced {
				newLines = append(newLines, "Contact: "+newContact.Value())
				contactReplaced = true
			}
		} else {
			newLines = append(newLines, line)
		}
	}

	// Parse the modified message
	newMsg, err := sip.ParseMessage([]byte(strings.Join(newLines, "\r\n")))
	if err != nil {
		return msg // Return original if parsing fails
	}

	return newMsg
}

// modifySDPContent modifies SDP content with new ports and address
func modifySDPContent(
	sdpContent string,
	audioRTP int,
	audioRTCP int,
	videoRTP int,
	videoRTCP int,
	sdpAddr string,
) string {
	lines := strings.Split(sdpContent, "\n")
	var newLines []string
	var currentMediaType string

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			newLines = append(newLines, line)
			continue
		}

		// Modify connection address (c=IN IP4 192.168.1.1)
		if strings.HasPrefix(trimmedLine, "c=") && sdpAddr != "" {
			parts := strings.Fields(trimmedLine)
			if len(parts) >= 3 {
				parts[2] = sdpAddr
				newLines = append(newLines, strings.Join(parts, " "))
				continue
			}
		}

		// Modify media line (m=audio 20000 RTP/AVP 0 8)
		if strings.HasPrefix(trimmedLine, "m=") {
			parts := strings.Fields(trimmedLine)
			if len(parts) >= 2 {
				currentMediaType = parts[0][2:] // Remove "m="
				if currentMediaType == "audio" && audioRTP > 0 {
					parts[1] = strconv.Itoa(audioRTP)
				} else if currentMediaType == "video" && videoRTP > 0 {
					parts[1] = strconv.Itoa(videoRTP)
				}
				newLines = append(newLines, strings.Join(parts, " "))
				continue
			}
		}

		// Modify RTCP attribute (a=rtcp:20001)
		if strings.HasPrefix(trimmedLine, "a=rtcp:") {
			parts := strings.Fields(trimmedLine)
			if len(parts) > 0 {
				if currentMediaType == "audio" && audioRTCP > 0 {
					parts[0] = "a=rtcp:" + strconv.Itoa(audioRTCP)
				} else if currentMediaType == "video" && videoRTCP > 0 {
					parts[0] = "a=rtcp:" + strconv.Itoa(videoRTCP)
				}
				newLines = append(newLines, strings.Join(parts, " "))
				continue
			}
		}

		// Keep other lines unchanged
		newLines = append(newLines, line)
	}

	return strings.Join(newLines, "\n")
}
