# Implementation Plan: Multi-Extension High-Performance Forwarding System

**Branch**: `001-multi-extension-forwarding` | **Date**: 2025-11-10 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-multi-extension-forwarding/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Transform tctool-go from single-client SIP/RTP proxy to high-performance multi-concurrent system supporting 5000+ extensions. Each extension (分机号) will have dedicated TCP connection to tunnel server with independent authentication and encryption keys. System must parse extension numbers from SIP headers, implement connection pooling, maintain per-extension port mappings, and provide graceful degradation under load while preserving all existing security and protocol compliance requirements.

## Technical Context

**Language/Version**: Go 1.21+ (existing codebase in Go)
**Primary Dependencies**:
- PJSUA library (existing, for SIP handling)
- Go standard library: net, crypto/aes, crypto/rsa, encoding/json
- sync primitives: sync.RWMutex, sync.Mutex, sync.WaitGroup
- No new external dependencies required

**Storage**: In-memory connection pool with extension state (no persistent storage)
**Testing**: Go testing framework (`go test`), race detector (`go test -race`)
**Target Platform**: Linux/Windows server (existing deployment platform)
**Project Type**: Single project (network service daemon)
**Performance Goals**:
- 5000+ concurrent extensions
- SIP forwarding latency <100ms
- RTP forwarding latency <10ms per packet
- Connection creation <500ms

**Constraints**:
- Memory usage <2GB with 5000 extensions
- CPU usage <80% under full load
- Maintain existing port range (20000-40000)
- Zero connection leaks over 24h operation

**Scale/Scope**:
- 5000 concurrent extensions (target)
- 10000 potential extensions (future)
- Existing codebase: ~700 lines across main.go, tunnel.go, sipmsg.go, pjsip.go

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### ✅ I. Network Security & Encryption (NON-NEGOTIABLE)
- **Compliance**: Each extension will have separate AES encryption keys via independent authentication
- **Status**: PASS - Per-extension encryption keys maintain security isolation
- **Impact**: FR-008, FR-009 ensure independent key management per extension

### ✅ II. Concurrency Safety (NON-NEGOTIABLE)
- **Compliance**: Connection pool will use sync.RWMutex for concurrent extension lookup/creation
- **Status**: PASS - Extension connection state protected with mutex locks
- **Impact**: ConnectionPool and ExtensionConnection require proper synchronization

### ✅ III. Graceful Shutdown & Resource Cleanup (NON-NEGOTIABLE)
- **Compliance**: Shutdown must iterate all extension connections and cleanup resources
- **Status**: PASS - Extends existing shutdown pattern to multi-extension context
- **Impact**: FR-011 (cleanup inactive connections), shutdown handler must cleanup all extensions

### ✅ IV. Port Management & RTP Forwarding
- **Compliance**: Each extension will have independent port allocations tracked separately
- **Status**: PASS - Port manager refactored to support per-extension port tracking
- **Impact**: ExtensionPortManager manages port ranges scoped to extensions

### ✅ V. SIP Message Integrity & Protocol Compliance
- **Compliance**: Extension parsing adds layer before existing SIP handling
- **Status**: PASS - Extension extraction doesn't modify SIP message integrity
- **Impact**: FR-001 (extension parsing) occurs before existing SIP protocol handling

### ✅ VI. Error Handling & Logging
- **Compliance**: Per-extension errors logged with extension context
- **Status**: PASS - FR-013 mandates logging extension lifecycle events
- **Impact**: Error handling includes extension identifier in log messages

### ✅ VII. Testing & Validation
- **Compliance**: Concurrent extension handling requires race detector testing
- **Status**: PASS - Test plan includes race testing with 5000 concurrent extensions
- **Impact**: SC-009 (24h zero-leak test) validates resource cleanup

### Performance Standards Compliance

**Latency Requirements**: ✅ PASS
- SIP forwarding <100ms (SC-003)
- RTP forwarding <10ms (SC-004)

**Throughput Requirements**: ⚠️ EXTENSION REQUIRED
- Constitution targets 10+ calls, feature requires 5000+ extensions
- **Justification**: Multi-extension support is explicit requirement to scale beyond current 10-call limit
- **Status**: ACCEPTABLE - Feature purpose is to extend throughput capabilities

**Resource Constraints**: ✅ PASS
- Memory <2GB (SC-005)
- Bounded goroutine count (4 per extension + 2 base = ~20,002 goroutines max)

## Project Structure

### Documentation (this feature)

```text
specs/001-multi-extension-forwarding/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   └── internal-api.md  # Internal API for connection pool management
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
F:/tctool-go/
├── main.go                    # Entry point, signal handling, goroutine coordination
├── extension.go               # NEW: Extension connection pool and lifecycle management
├── ext_parser.go              # NEW: SIP extension number parsing and validation
├── tunnel.go                  # Existing: Tunnel protocol, encryption, RTP forwarding
├── sipmsg.go                  # Existing: SIP message handling (will be refactored for extension routing)
├── pjsip.go                   # Existing: PJSUA integration
├── build.sh                   # Build script
└── tests/
    ├── extension_test.go      # NEW: Extension pool unit tests
    ├── ext_parser_test.go     # NEW: Extension parsing tests
    ├── integration_test.go    # NEW: Multi-extension integration tests
    └── race_test.go           # NEW: Race condition testing
```

**Structure Decision**: Single project structure maintained. All Go source files in root directory following existing pattern. New files added for extension management (`extension.go` for connection pool, `ext_parser.go` for SIP parsing). Existing files (main.go, sipmsg.go) will be refactored to route messages through extension-specific connections. Tests directory added for comprehensive testing including race detection.

## Complexity Tracking

> No constitution violations - all gates passed. This section intentionally left empty.
