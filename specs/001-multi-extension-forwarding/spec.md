# Feature Specification: Multi-Extension High-Performance Forwarding System

**Feature Branch**: `001-multi-extension-forwarding`
**Created**: 2025-11-10
**Status**: Draft
**Input**: Transform single-client SIP/RTP proxy into high-performance multi-concurrent system supporting 5000+ extensions with per-extension TCP connections

## Clarifications

### Session 2025-11-10

- Q: Extension parsing priority when From, To, and Contact all contain extension numbers → A: INVITE messages must distinguish caller/callee roles: For outgoing INVITE (caller), use From field; For INVITE responses (100 Trying, 180 Ringing, 200 OK - callee), use To field. Non-INVITE messages use priority From > To > Contact. If parsed extension is not purely numeric (e.g., "unknown"), try next field in priority order. All responses from server route to single SIPP source address/port.
- Q: TCP connection failure automatic reconnection strategy → A: Use exponential backoff retry (1s, 2s, 4s) with maximum 3 attempts. If all retries fail, mark extension connection as failed and require new SIP message to trigger reconnection. This balances fast recovery with server overload prevention in high-concurrency scenarios.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Extension-Based Connection Isolation (Priority: P1)

When a SIP INVITE message arrives from the local network, the system must extract the extension number (分机号) from the SIP message headers and establish a dedicated TCP connection to the tunnel server for that specific extension. Each extension operates independently with its own encrypted tunnel, ensuring isolation and dedicated resources.

**Why this priority**: This is the foundational capability that enables multi-extension support. Without per-extension connection isolation, the system cannot scale beyond single-client mode.

**Independent Test**: Can be fully tested by sending SIP INVITE messages from 3 different extensions (e.g., 1001, 1002, 1003) and verifying that 3 separate TCP connections are established to the tunnel server, each handling only its own extension's traffic.

**Acceptance Scenarios**:

1. **Given** system is running, **When** SIP INVITE from extension 1001 arrives, **Then** system creates TCP connection for extension 1001 and forwards the message
2. **Given** extension 1001 connection exists, **When** SIP INVITE from extension 1002 arrives, **Then** system creates separate TCP connection for extension 1002 without affecting 1001
3. **Given** multiple extensions are active, **When** SIP message for extension 1001 arrives, **Then** system routes it through extension 1001's dedicated TCP connection
4. **Given** extension TCP connection exists, **When** duplicate INVITE for same extension arrives, **Then** system reuses existing TCP connection instead of creating new one

---

### User Story 2 - Extension Number Parsing & Validation (Priority: P1)

The system must reliably extract extension numbers from SIP message headers with role-aware parsing logic. For INVITE messages, the system distinguishes between caller (outgoing INVITE using From header) and callee (INVITE responses using To header). For non-INVITE messages, parsing follows From > To > Contact priority. The system validates that extracted extensions are purely numeric, falling back to next priority field if non-numeric values (e.g., "unknown") are encountered. All SIP traffic originates from and returns to a single SIPP tool UDP port.

**Why this priority**: Accurate extension parsing with caller/callee role awareness is critical for routing decisions in SIPP tool integration scenarios. Incorrect parsing could route call responses to wrong extensions or cause connection leaks.

**Independent Test**: Send SIP messages with various header formats and message types (INVITE, 200 OK, ACK, BYE) and verify correct extension extraction based on message type and role, including fallback logic for non-numeric extensions.

**Acceptance Scenarios**:

1. **Given** outgoing SIP INVITE with From header "sip:1001@192.168.1.10", **When** system parses message, **Then** extension 1001 is extracted from From field
2. **Given** incoming SIP 200 OK response with To header "sip:1002@pbx.com", **When** system parses message, **Then** extension 1002 is extracted from To field
3. **Given** SIP message with From="sip:unknown@host" and To="sip:1003@host", **When** system parses, **Then** system skips non-numeric "unknown" and extracts 1003 from To field
4. **Given** non-INVITE message (ACK/BYE) with From="sip:1001@host", **When** system parses, **Then** extension 1001 is extracted from From field using priority logic
5. **Given** SIP message with missing or all non-numeric extension fields, **When** system attempts to parse, **Then** error is logged and message is rejected with appropriate SIP response

---

### User Story 3 - Concurrent Extension Scaling to 5000+ (Priority: P1)

The system must efficiently manage 5000+ concurrent extension connections with minimal resource overhead. Connection pooling, goroutine management, and memory allocation must be optimized to handle high concurrency without degradation.

**Why this priority**: This is the core scalability requirement. The system must prove it can handle the target load of 5000+ extensions without performance issues.

**Independent Test**: Simulate 5000 concurrent extensions each sending periodic SIP messages and RTP media, monitor resource usage (CPU, memory, goroutines), and verify all messages are forwarded correctly within latency requirements.

**Acceptance Scenarios**:

1. **Given** system is idle, **When** 5000 extensions register simultaneously, **Then** all connections are established within 30 seconds and system remains responsive
2. **Given** 5000 active extensions, **When** all extensions send SIP messages concurrently, **Then** messages are forwarded with <100ms latency per message
3. **Given** 5000 active extensions, **When** monitoring system resources, **Then** memory usage remains below 2GB and CPU usage below 80%
4. **Given** 5000 active extensions with active calls, **When** RTP media is forwarded, **Then** media latency remains <10ms per packet

---

### User Story 4 - Extension Connection Lifecycle Management (Priority: P2)

Each extension's TCP connection must be managed through its complete lifecycle: creation on first SIP message, reuse for subsequent messages, keepalive/heartbeat to maintain connection, and cleanup when extension becomes inactive.

**Why this priority**: Proper lifecycle management prevents resource leaks and ensures connection availability. This builds on the P1 isolation capability.

**Independent Test**: Monitor a single extension through its lifecycle (register → make calls → idle period → cleanup) and verify connection is maintained during activity and cleaned up after idle timeout.

**Acceptance Scenarios**:

1. **Given** extension 1001 sends first SIP message, **When** TCP connection is created, **Then** connection manager tracks creation timestamp and activity
2. **Given** extension 1001 connection is idle for 5 minutes, **When** heartbeat check runs, **Then** keepalive message is sent to maintain connection
3. **Given** extension 1001 has been inactive for 30 minutes, **When** cleanup process runs, **Then** TCP connection is closed and resources are released
4. **Given** extension 1001 connection was cleaned up, **When** new SIP message arrives for 1001, **Then** new TCP connection is created automatically

---

### User Story 5 - Graceful Degradation Under Load (Priority: P2)

When system approaches capacity limits (>5000 extensions or resource constraints), it must degrade gracefully by rejecting new extensions with clear error messages while maintaining quality of service for existing extensions.

**Why this priority**: Graceful degradation prevents catastrophic failure and maintains service quality for established connections.

**Independent Test**: Load system with 5000 extensions, attempt to add 100 more, verify new connections are rejected with SIP error responses while existing 5000 extensions continue operating normally.

**Acceptance Scenarios**:

1. **Given** 5000 extensions are active (at capacity), **When** new extension attempts registration, **Then** SIP 503 Service Unavailable is returned with Retry-After header
2. **Given** system is at capacity, **When** existing extension sends SIP message, **Then** message is forwarded normally without degradation
3. **Given** system approaches memory limit (90% of 2GB), **When** new extension attempts connection, **Then** connection is rejected and warning is logged
4. **Given** system is overloaded, **When** administrator requests status, **Then** current extension count and resource usage are reported

---

### Edge Cases

- What happens when extension number appears in multiple SIP header fields with different values?
  - System uses role-aware parsing logic: INVITE caller uses From, INVITE responses use To, non-INVITE uses From > To > Contact priority
- How does system handle non-numeric extension values (e.g., "unknown")?
  - System skips non-numeric values and tries next field in priority order; rejects message if all fields non-numeric
- How does system route responses from server back to SIPP tool?
  - All server responses route to single SIPP source address/port regardless of extension
- How does system handle malformed SIP messages that cannot be parsed?
  - System logs error and rejects message with appropriate SIP error response
- What happens when TCP connection for an extension fails mid-call?
  - Automatic reconnection triggered with exponential backoff (1s, 2s, 4s, max 3 attempts); if all fail, mark failed and wait for new message
- How does system handle TCP connection reconnection without dropping active calls?
  - Reconnection attempts preserve extension state; active port mappings maintained; new messages queued during reconnection
- What happens when server returns authentication failure for a specific extension?
  - Connection marked as failed, error logged, subsequent messages for that extension trigger retry
- How does system handle port exhaustion when all 20000 ports are allocated?
  - New port allocation requests fail, logged as error, may trigger extension connection cleanup
- What happens when extension sends burst of messages faster than TCP can transmit?
  - Messages queued in TCP send buffer; if buffer full, backpressure applied with error logging
- How does system prevent memory leaks when extensions reconnect frequently?
  - Proper cleanup on connection close, defer patterns, wait groups track goroutine lifecycle

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST extract extension number from SIP message headers using role-aware parsing: For outgoing INVITE messages, extract from From header; For INVITE responses (100 Trying, 180 Ringing, 200 OK), extract from To header; For non-INVITE messages, use priority From > To > Contact
- **FR-001a**: System MUST validate extracted extension is purely numeric (3-5 digits); if non-numeric (e.g., "unknown"), system MUST try next field in priority order
- **FR-001b**: System MUST route all responses from tunnel server to single SIPP source UDP address/port regardless of extension number
- **FR-002**: System MUST create dedicated TCP connection to tunnel server for each unique extension number
- **FR-003**: System MUST route all SIP/RTP messages for an extension through its dedicated TCP connection
- **FR-004**: System MUST support minimum 5000 concurrent extension connections
- **FR-005**: System MUST reuse existing TCP connection when multiple messages arrive for same extension
- **FR-006**: System MUST track extension connection state (active, idle, disconnected)
- **FR-007**: System MUST implement connection pooling to manage TCP connections efficiently
- **FR-008**: System MUST authenticate each extension's TCP connection independently with tunnel server
- **FR-009**: System MUST maintain separate AES encryption keys per extension connection
- **FR-010**: System MUST implement heartbeat mechanism to keep extension connections alive
- **FR-011**: System MUST cleanup inactive extension connections after configurable timeout (default: 30 minutes)
- **FR-012**: System MUST reject new connections when capacity limit is reached with SIP 503 response
- **FR-013**: System MUST log extension connection lifecycle events (create, reuse, cleanup, errors)
- **FR-014**: System MUST handle TCP connection failures with automatic reconnection using exponential backoff strategy (retry delays: 1s, 2s, 4s) with maximum 3 attempts; if all retries fail, mark connection as failed and require new SIP message to trigger reconnection
- **FR-014a**: System MUST preserve extension state and active port mappings during reconnection attempts
- **FR-014b**: System MUST queue new messages for extension during reconnection attempts (up to 10 messages or 30 seconds timeout)
- **FR-015**: System MUST preserve existing SIP message integrity and protocol compliance requirements
- **FR-016**: System MUST maintain existing port management and RTP forwarding per extension
- **FR-017**: System MUST validate extension number format before creating connections (numeric, 3-5 digits)
- **FR-018**: System MUST reject SIP messages with missing or all non-numeric extension numbers

### Key Entities

- **Extension**: Represents a unique extension number (分机号) with dedicated TCP connection, authentication state, and port mappings
- **ExtensionConnection**: Manages TCP connection lifecycle, encryption keys, heartbeat state, and activity timestamps for one extension
- **ConnectionPool**: Manages collection of all extension connections with creation, lookup, reuse, and cleanup operations
- **ExtensionAuthManager**: Handles per-extension authentication with tunnel server and AES key management
- **ExtensionPortManager**: Manages port allocations scoped to specific extension (each extension has independent port range)

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: System successfully handles 5000 concurrent extensions with all SIP messages forwarded correctly
- **SC-002**: Extension connection creation completes within 500ms from first SIP message receipt
- **SC-003**: SIP message forwarding latency remains below 100ms under 5000 concurrent extensions
- **SC-004**: RTP packet forwarding latency remains below 10ms per packet under load
- **SC-005**: System memory usage remains below 2GB with 5000 active extensions
- **SC-006**: System CPU usage remains below 80% under full load (5000 extensions with active calls)
- **SC-007**: Extension number parsing accuracy is 100% for valid SIP messages
- **SC-008**: Connection reuse rate is >95% for extensions with multiple SIP messages
- **SC-009**: Zero TCP connection leaks over 24-hour continuous operation test
- **SC-010**: Graceful degradation handles 100+ connection attempts beyond capacity without affecting existing extensions
- **SC-011**: Inactive extension cleanup recovers resources within 30 minutes of last activity
- **SC-012**: System continues operating correctly when individual extension connections fail (fault isolation); reconnection succeeds within 7 seconds (1s+2s+4s) for 95% of transient failures

## Assumptions

- Extension numbers are numeric identifiers (e.g., 1001, 1002) with 3-5 digits
- Extension number appears in standard SIP headers (From/To/Contact) in URI format
- SIPP tool generates all SIP messages from single UDP source port, system routes all server responses back to this single source
- Non-numeric extension values (e.g., "unknown") may appear in SIP headers and must be handled with fallback logic
- INVITE messages require caller/callee role distinction for correct extension extraction
- Tunnel server supports multiple concurrent TCP connections from same client IP
- Server-side port allocation API supports per-connection port requests
- Network bandwidth supports 5000 concurrent RTP streams (estimated: 500 Kbps per stream = 2.5 Gbps total)
- Existing PJSUA library can handle receiving from multiple local extensions on same UDP port
- Extension activity timeout of 30 minutes is acceptable for connection cleanup
- Maximum capacity of 5000 extensions with potential to extend to 10000 in future
- Each extension generates average of 2-5 SIP messages per minute during active calls
- Server-side authentication supports per-connection credentials/keys

## Technical Constraints

- MUST maintain compatibility with existing tunnel server protocol
- MUST preserve all existing security and encryption requirements (AES-128-CBC, RSA-OAEP)
- MUST maintain all existing concurrency safety patterns (mutex locks, goroutine management)
- MUST maintain existing graceful shutdown behavior (cleanup all extension connections)
- MUST NOT break existing SIP protocol compliance and message integrity
- MUST operate within existing port range constraints (20000-40000, allocated per extension)
- MUST maintain existing RTP forwarding performance (<10ms latency per packet)

## Out of Scope

- Load balancing across multiple tunnel servers (single server only)
- Extension provisioning or registration management (assumes extensions pre-configured)
- SIP registrar functionality (only proxy/forwarding)
- Extension authentication with local PBX (only tunnel server authentication)
- Web UI or monitoring dashboard for extension status
- Automatic extension discovery or dynamic configuration
- Support for non-numeric extension formats
- Migration of existing single-client connections to multi-extension mode
