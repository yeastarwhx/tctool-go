package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Extension number validation regex: 3-10 digit numeric string
var extensionRegex = regexp.MustCompile(`^[0-9]{3,10}$`)

// ValidateExtension validates extension number format (3-5 digits numeric)
// Returns true if valid, false otherwise
func ValidateExtension(number string) bool {
	return extensionRegex.MatchString(strings.TrimSpace(number))
}

// NewExtension creates a new Extension with validation
func NewExtension(number string) (Extension, error) {
	number = strings.TrimSpace(number)
	isValid := ValidateExtension(number)

	if !isValid {
		return Extension{}, ErrInvalidExtensionFormat
	}

	return Extension{
		Number:  number,
		IsValid: isValid,
	}, nil
}

// ===================================================================
// SIP Message Parsing Functions (User Story 2)
// ===================================================================

// IsSIPMethod checks if a SIP message is of a specific method
func IsSIPMethod(msg string, method string) bool {
	return strings.HasPrefix(msg, method+" ")
}

// ExtractSIPHeader extracts the value of a SIP header from a message
// Returns empty string if header not found
func ExtractSIPHeader(msg string, headerName string) string {
	// Support both CRLF and LF-only messages
	var lines []string
	if strings.Contains(msg, "\r\n") {
		lines = strings.Split(msg, "\r\n")
	} else {
		lines = strings.Split(msg, "\n")
	}

	headerNameLower := strings.ToLower(headerName)

	// Try full header name first (case-insensitive)
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Find index of ':' to separate header and value
		if idx := strings.Index(line, ":"); idx != -1 {
			namePart := strings.ToLower(strings.TrimSpace(line[:idx]))
			if namePart == headerNameLower {
				value := strings.TrimSpace(line[idx+1:])
				return value
			}
		}
	}

	// Try compact form (e.g., "f:" for "From:")
	compactForm := strings.ToLower(getCompactForm(headerName))
	if compactForm != "" {
		for _, line := range lines {
			if line == "" {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), compactForm+":") {
				// extract after the first ':'
				if idx := strings.Index(line, ":"); idx != -1 {
					value := strings.TrimSpace(line[idx+1:])
					return value
				}
			}
		}
	}

	return ""
}

// getCompactForm returns the compact form of a SIP header name
func getCompactForm(headerName string) string {
	compactForms := map[string]string{
		"From":    "f",
		"To":      "t",
		"Contact": "m",
		"Via":     "v",
	}
	return compactForms[headerName]
}

// ParseSIPURI extracts the user part from a SIP URI (before @)
// Example: "sip:1001@192.168.1.1:5060" -> "1001"
// Also handles: "Display Name" <sip:1001@...> format
func ParseSIPURI(uri string) string {
	uri = strings.TrimSpace(uri)

	// Handle format: "Display Name" <sip:user@host>
	// Extract the URI from angle brackets if present
	if idx := strings.Index(uri, "<"); idx != -1 {
		endIdx := strings.Index(uri[idx:], ">")
		if endIdx != -1 {
			uri = uri[idx+1 : idx+endIdx]
		}
	}

	uri = strings.TrimSpace(uri)

	// Remove "sip:" or "sips:" prefix
	uri = strings.TrimPrefix(uri, "sip:")
	uri = strings.TrimPrefix(uri, "sips:")

	// Extract user part before @
	atIndex := strings.Index(uri, "@")
	if atIndex == -1 {
		return ""
	}

	userPart := uri[:atIndex]

	// Remove parameters (e.g., "1001;tag=..." -> "1001")
	if semicolonIndex := strings.Index(userPart, ";"); semicolonIndex != -1 {
		userPart = userPart[:semicolonIndex]
	}

	return strings.TrimSpace(userPart)
}

// IsNumeric checks if a string contains only digits
func IsNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// ExtractExtensionFromInvite extracts extension from INVITE messages with role awareness
// For outgoing INVITE (client is caller): use From header
// For incoming INVITE response (200 OK, 183, etc.): use To header (callee)
func ExtractExtensionFromInvite(msg string) string {
	// Check if this is a response (status line starts with "SIP/2.0")
	isResponse := strings.HasPrefix(msg, "SIP/2.0")

	if isResponse {
		// Response to INVITE: client is callee, use To header
		toHeader := ExtractSIPHeader(msg, "To")
		if toHeader != "" {
			userPart := ParseSIPURI(toHeader)
			if IsNumeric(userPart) && ValidateExtension(userPart) {
				return userPart
			}
		}
	} else {
		// Outgoing INVITE: client is caller, use From header
		fromHeader := ExtractSIPHeader(msg, "From")
		if fromHeader != "" {
			userPart := ParseSIPURI(fromHeader)
			if IsNumeric(userPart) && ValidateExtension(userPart) {
				return userPart
			}
		}
	}

	return ""
}

// ExtractExtensionFromNonInvite extracts extension from non-INVITE messages
// For responses (200 OK, etc.): use To header (request originator)
// For requests (BYE, CANCEL, etc.): use From header (request sender)
func ExtractExtensionFromNonInvite(msg string) string {
	// Check if this is a response (status line starts with "SIP/2.0")
	isResponse := strings.HasPrefix(msg, "SIP/2.0")

	if isResponse {
		// For responses, use To header first (request originator)
		toHeader := ExtractSIPHeader(msg, "To")
		if toHeader != "" {
			userPart := ParseSIPURI(toHeader)
			if IsNumeric(userPart) && ValidateExtension(userPart) {
				return userPart
			}
		}

		// Fallback to From header
		fromHeader := ExtractSIPHeader(msg, "From")
		if fromHeader != "" {
			userPart := ParseSIPURI(fromHeader)
			if IsNumeric(userPart) && ValidateExtension(userPart) {
				return userPart
			}
		}
	} else {
		// For requests, use From header first (request sender)
		fromHeader := ExtractSIPHeader(msg, "From")
		if fromHeader != "" {
			userPart := ParseSIPURI(fromHeader)
			if IsNumeric(userPart) && ValidateExtension(userPart) {
				return userPart
			}
		}

		// Fallback to To header
		toHeader := ExtractSIPHeader(msg, "To")
		if toHeader != "" {
			userPart := ParseSIPURI(toHeader)
			if IsNumeric(userPart) && ValidateExtension(userPart) {
				return userPart
			}
		}
	}

	// Try Contact header last
	contactHeader := ExtractSIPHeader(msg, "Contact")
	if contactHeader != "" {
		userPart := ParseSIPURI(contactHeader)
		if IsNumeric(userPart) && ValidateExtension(userPart) {
			return userPart
		}
	}

	return ""
}

// ExtractExtension is the main entry point for extracting extension from SIP messages
// Dispatches to role-aware parsers based on message type
func ExtractExtension(msg string) (string, error) {
	if msg == "" {
		return "", ErrMalformedSIPMessage
	}

	// Check if this is an INVITE-related message
	isInviteRequest := IsSIPMethod(msg, "INVITE")
	isInviteResponse := strings.HasPrefix(msg, "SIP/2.0") && strings.Contains(msg, "INVITE")

	var extNumber string

	if isInviteRequest || isInviteResponse {
		extNumber = ExtractExtensionFromInvite(msg)
	} else {
		extNumber = ExtractExtensionFromNonInvite(msg)
	}

	// Validate result
	if extNumber == "" {
		// Debug: extract first line to show message type
		firstLine := msg
		if idx := strings.Index(msg, "\r\n"); idx > 0 {
			firstLine = msg[:idx]
		} else if idx := strings.Index(msg, "\n"); idx > 0 {
			firstLine = msg[:idx]
		}
		return "", fmt.Errorf("extension number not found in SIP headers (message type: %s)", firstLine)
	}

	if !ValidateExtension(extNumber) {
		return "", ErrInvalidExtensionFormat
	}

	return extNumber, nil
}
