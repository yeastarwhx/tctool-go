# Tasks: Multi-Extension High-Performance Forwarding System

**Input**: Design documents from `/specs/001-multi-extension-forwarding/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Tests are NOT requested in this feature specification. Focus on implementation and manual testing per quickstart.md.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Single project**: All `.go` files in repository root `F:/tctool-go/`
- **Tests**: `F:/tctool-go/tests/` directory
- Paths shown below use absolute paths for clarity

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [X] T001 Create tests directory structure at F:/tctool-go/tests/
- [X] T002 Update build.sh to include new source files (extension.go, ext_parser.go)
- [X] T003 [P] Add error definitions for extension management in F:/tctool-go/extension.go header section
- [X] T004 [P] Add constants for extension configuration in F:/tctool-go/extension.go (MAX_EXTENSIONS=5000, IDLE_TIMEOUT=30min, PORT_RANGE=20000-40000)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 Define Extension struct with Number and IsValid fields in F:/tctool-go/extension.go
- [X] T006 Define ConnectionState enum (DISCONNECTED, CONNECTING, AUTHENTICATED, ACTIVE, IDLE, CLEANUP) in F:/tctool-go/extension.go
- [X] T007 Define AuthState enum (UNAUTHENTICATED, AUTHENTICATING, AUTHENTICATED, FAILED) in F:/tctool-go/extension.go
- [X] T008 [P] Define ExtensionConnection struct with all fields (Extension, TCPConn, State, timestamps, channels, mutex) in F:/tctool-go/extension.go
- [X] T009 [P] Define ConnectionPool struct with sync.Map, capacity, count, mutex in F:/tctool-go/extension.go
- [X] T010 [P] Define ExtensionAuthManager struct with keys, state, retry count, mutex in F:/tctool-go/extension.go
- [X] T011 [P] Define ExtensionPortManager struct with mappings, port range, mutex in F:/tctool-go/extension.go

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Extension-Based Connection Isolation (Priority: P1) 🎯 MVP

**Goal**: Extract extension from SIP messages and create dedicated TCP connections per extension

**Independent Test**: Send SIP INVITE from 3 different extensions (1001, 1002, 1003) and verify 3 separate TCP connections established

### Implementation for User Story 1

- [ ] T012 [P] [US1] Implement Extension validation function ValidateExtension(number string) in F:/tctool-go/ext_parser.go (regex: ^[0-9]{3,5}$)
- [ ] T013 [P] [US1] Implement ConnectionPool constructor NewConnectionPool(capacity int) in F:/tctool-go/extension.go
- [ ] T014 [US1] Implement ConnectionPool.GetOrCreate(ext Extension) with capacity check and sync.Map operations in F:/tctool-go/extension.go
- [ ] T015 [US1] Implement ConnectionPool.Get(extNumber string) for lock-free lookup in F:/tctool-go/extension.go
- [ ] T016 [P] [US1] Implement ExtensionConnection constructor NewExtensionConnection(ext Extension) in F:/tctool-go/extension.go
- [ ] T017 [P] [US1] Implement ExtensionAuthManager constructor NewExtensionAuthManager(ext Extension) in F:/tctool-go/extension.go
- [ ] T018 [US1] Implement ExtensionAuthManager.GenerateTCAESKey() using crypto/rand in F:/tctool-go/extension.go
- [ ] T019 [US1] Implement ExtensionAuthManager.Authenticate(conn net.Conn) with RSA-OAEP encryption in F:/tctool-go/extension.go
- [ ] T020 [US1] Implement ExtensionConnection state transition logic (DISCONNECTED → CONNECTING → AUTHENTICATED → ACTIVE) in F:/tctool-go/extension.go
- [ ] T021 [US1] Implement ExtensionConnection.SendSIP(msg string, aesKey string) with AES encryption and TCP write in F:/tctool-go/extension.go
- [ ] T022 [US1] Implement ExtensionConnection.UpdateActivity() to update LastActivity timestamp in F:/tctool-go/extension.go
- [ ] T023 [US1] Refactor main.go to initialize ConnectionPool as global variable with capacity 5000
- [ ] T024 [US1] Refactor recvFromPJSIP() in main.go to use ConnectionPool.GetOrCreate() instead of global TCP connection
- [ ] T025 [US1] Update SIP message forwarding logic to route through per-extension TCP connection in main.go

**Checkpoint**: At this point, User Story 1 should be fully functional - multiple extensions create separate TCP connections

---

## Phase 4: User Story 2 - Extension Number Parsing & Validation (Priority: P1)

**Goal**: Implement role-aware SIP parsing with From/To/Contact priority and non-numeric fallback

**Independent Test**: Send INVITE, 200 OK, ACK, BYE messages with various header formats and verify correct extension extraction

### Implementation for User Story 2

- [ ] T026 [P] [US2] Implement IsSIPMethod(msg string, method string) helper in F:/tctool-go/ext_parser.go to detect message type
- [ ] T027 [P] [US2] Implement ExtractSIPHeader(msg string, headerName string) helper in F:/tctool-go/ext_parser.go to extract header value
- [ ] T028 [US2] Implement ParseSIPURI(uri string) to extract user part before @ symbol in F:/tctool-go/ext_parser.go
- [ ] T029 [US2] Implement IsNumeric(value string) validator for extension numbers in F:/tctool-go/ext_parser.go
- [ ] T030 [US2] Implement ExtractExtensionFromInvite(msg string) with caller/callee role logic in F:/tctool-go/ext_parser.go (outgoing INVITE uses From, responses use To)
- [ ] T031 [US2] Implement ExtractExtensionFromNonInvite(msg string) with From > To > Contact priority in F:/tctool-go/ext_parser.go
- [ ] T032 [US2] Implement ExtractExtension(msg string) as main entry point that dispatches to role-aware parsers in F:/tctool-go/ext_parser.go
- [ ] T033 [US2] Add non-numeric fallback logic to try next field if current field is "unknown" or non-numeric in F:/tctool-go/ext_parser.go
- [ ] T034 [US2] Implement error handling for messages with all non-numeric extensions (return ErrExtensionNotFound) in F:/tctool-go/ext_parser.go
- [ ] T035 [US2] Update recvFromPJSIP() in main.go to use ExtractExtension() for all SIP messages
- [ ] T036 [US2] Update recvFromTS() in main.go to ensure server responses route to single SIPP source port (preserve srcSIPAddr)
- [ ] T037 [US2] Add logging for extension parsing errors with SIP message context in main.go

**Checkpoint**: At this point, User Stories 1 AND 2 both work - role-aware parsing routes messages correctly

---

## Phase 5: User Story 3 - Concurrent Extension Scaling to 5000+ (Priority: P1)

**Goal**: Optimize connection pool, memory, and goroutine management for 5000+ concurrent extensions

**Independent Test**: Simulate 5000 extensions sending SIP messages, verify memory <2GB, CPU <80%, latency <100ms

### Implementation for User Story 3

- [ ] T038 [P] [US3] Implement ConnectionPool.GetCount() using atomic.Int32.Load() in F:/tctool-go/extension.go
- [ ] T039 [P] [US3] Implement ConnectionPool.IsFull() capacity check in F:/tctool-go/extension.go
- [ ] T040 [P] [US3] Implement ConnectionPool.GetAllExtensions() to list active extensions in F:/tctool-go/extension.go
- [ ] T041 [US3] Add capacity enforcement in ConnectionPool.GetOrCreate() to return ErrCapacityReached when full in F:/tctool-go/extension.go
- [ ] T042 [US3] Implement graceful degradation in recvFromPJSIP() to send SIP 503 Service Unavailable when capacity reached in main.go
- [ ] T043 [US3] Add Retry-After header (180 seconds) to SIP 503 response in main.go
- [ ] T044 [P] [US3] Optimize ExtensionConnection memory layout with proper struct field ordering in F:/tctool-go/extension.go
- [ ] T045 [P] [US3] Implement sync.Pool for SIP message buffers to reduce GC pressure in F:/tctool-go/extension.go
- [ ] T046 [US3] Add memory usage monitoring with runtime.ReadMemStats() in main.go (check at 90% of 2GB limit)
- [ ] T047 [US3] Add goroutine count monitoring with runtime.NumGoroutine() in main.go (warn if >25000)
- [ ] T048 [US3] Log capacity warnings when approaching limits (>4500 extensions or >1.8GB memory) in main.go

**Checkpoint**: All P1 user stories complete - system handles 5000 extensions with resource constraints

---

## Phase 6: User Story 4 - Extension Connection Lifecycle Management (Priority: P2)

**Goal**: Implement heartbeat, idle detection, and cleanup for inactive extensions

**Independent Test**: Monitor extension 1001 through lifecycle (active → idle → cleanup) and verify resource release

### Implementation for User Story 4

- [ ] T049 [P] [US4] Implement ExtensionConnection.GetIdleTime() to calculate time since LastActivity in F:/tctool-go/extension.go
- [ ] T050 [P] [US4] Implement ExtensionConnection.IsIdle() to check if idle >5 minutes in F:/tctool-go/extension.go
- [ ] T051 [US4] Implement ExtensionConnection.SendHeartbeat() to send keepalive to tunnel server in F:/tctool-go/extension.go
- [ ] T052 [US4] Implement heartbeatLoop() goroutine with 2-minute ticker and idle checks in F:/tctool-go/extension.go
- [ ] T053 [US4] Start heartbeat goroutine when connection transitions to IDLE state in F:/tctool-go/extension.go
- [ ] T054 [US4] Stop heartbeat goroutine when connection transitions to ACTIVE or CLEANUP in F:/tctool-go/extension.go
- [ ] T055 [P] [US4] Implement ExtensionPortManager.ReleaseAllPorts(tsAESKey string) to release server-side ports in F:/tctool-go/extension.go
- [ ] T056 [US4] Implement ExtensionConnection.Cleanup() to close TCP, stop goroutines, transition to DISCONNECTED in F:/tctool-go/extension.go
- [ ] T057 [US4] Implement ConnectionPool.Remove(extNumber string) with atomic count decrement in F:/tctool-go/extension.go
- [ ] T058 [US4] Implement ConnectionPool.CleanupInactive(idleTimeout time.Duration) to iterate and cleanup idle connections in F:/tctool-go/extension.go
- [ ] T059 [US4] Add cleanup timer goroutine in main.go with 5-minute ticker calling CleanupInactive(30*time.Minute)
- [ ] T060 [US4] Update graceful shutdown handler in main.go to call ConnectionPool.ShutdownAll()
- [ ] T061 [US4] Implement ConnectionPool.ShutdownAll() to cleanup all extensions and wait for goroutines in F:/tctool-go/extension.go
- [ ] T062 [US4] Add logging for lifecycle events (create, idle, heartbeat, cleanup) with extension context in F:/tctool-go/extension.go

**Checkpoint**: User Story 4 complete - lifecycle management prevents resource leaks

---

## Phase 7: User Story 5 - Graceful Degradation Under Load (Priority: P2)

**Goal**: Handle capacity limits, resource monitoring, and status reporting

**Independent Test**: Load 5000 extensions, attempt 100 more, verify rejections don't affect existing extensions

### Implementation for User Story 5

- [ ] T063 [P] [US5] Implement ResourceMonitor struct to track memory, CPU, goroutine counts in F:/tctool-go/extension.go
- [ ] T064 [P] [US5] Implement ResourceMonitor.CheckMemory() to compare against 2GB limit in F:/tctool-go/extension.go
- [ ] T065 [P] [US5] Implement ResourceMonitor.CheckCPU() to read CPU usage percentage in F:/tctool-go/extension.go
- [ ] T066 [US5] Add resource monitoring checks in ConnectionPool.GetOrCreate() before creating connections in F:/tctool-go/extension.go
- [ ] T067 [US5] Reject connections when memory usage >90% of 2GB limit in F:/tctool-go/extension.go
- [ ] T068 [US5] Implement GetStatus() function to return ConnectionPool metrics (count, capacity, memory, goroutines) in F:/tctool-go/extension.go
- [ ] T069 [US5] Add status logging every 5 minutes with current metrics in main.go
- [ ] T070 [US5] Ensure existing extensions continue forwarding when capacity reached (verify connection reuse logic) in F:/tctool-go/extension.go
- [ ] T071 [US5] Add integration test scenario for graceful degradation in F:/tctool-go/tests/integration_test.go

**Checkpoint**: All user stories complete - system degrades gracefully under load

---

## Phase 8: TCP Reconnection & Fault Tolerance (Cross-Cutting)

**Purpose**: Implement exponential backoff reconnection strategy from clarifications

- [ ] T072 [P] Define reconnection configuration constants in F:/tctool-go/extension.go (retry delays: 1s, 2s, 4s, max 3 attempts)
- [ ] T073 [P] Implement ExtensionConnection.Reconnect() with exponential backoff logic in F:/tctool-go/extension.go
- [ ] T074 Add message queue (buffered channel, capacity 10) for messages during reconnection in F:/tctool-go/extension.go
- [ ] T075 Implement queue timeout (30 seconds) with message dropping logic in F:/tctool-go/extension.go
- [ ] T076 Update ExtensionConnection.SendSIP() to queue messages if connection in CONNECTING state in F:/tctool-go/extension.go
- [ ] T077 Trigger reconnection on TCP write errors in ExtensionConnection.SendSIP() in F:/tctool-go/extension.go
- [ ] T078 Preserve ExtensionPortManager state during reconnection attempts in F:/tctool-go/extension.go
- [ ] T079 Mark connection as FAILED after 3 failed reconnection attempts in F:/tctool-go/extension.go
- [ ] T080 Add logging for reconnection attempts with backoff delays in F:/tctool-go/extension.go

---

## Phase 9: Port Management & RTP Forwarding (Cross-Cutting)

**Purpose**: Extend port management to support per-extension port allocations

- [ ] T081 [P] Implement ExtensionPortManager constructor NewExtensionPortManager(ext Extension) in F:/tctool-go/extension.go
- [ ] T082 [P] Implement ExtensionPortManager.AllocatePortPair() for sequential RTP/RTCP allocation in F:/tctool-go/extension.go
- [ ] T083 [P] Implement ExtensionPortManager.AllocatePortsForCall(hasVideo bool) in F:/tctool-go/extension.go
- [ ] T084 Update HandleINVITEFromUDP() in sipmsg.go to use ExtensionPortManager from ExtensionConnection
- [ ] T085 Update Handle200OKFromUDP() in sipmsg.go to use ExtensionPortManager from ExtensionConnection
- [ ] T086 Update HandleINVITEFromTCP() in sipmsg.go to use ExtensionPortManager from ExtensionConnection
- [ ] T087 Update Handle200OKFromTCP() in sipmsg.go to use ExtensionPortManager from ExtensionConnection
- [ ] T088 [P] Implement ExtensionPortManager.AddMapping(callID string, node *PortMappingNode) in F:/tctool-go/extension.go
- [ ] T089 [P] Implement ExtensionPortManager.FindMapping(callID string) in F:/tctool-go/extension.go
- [ ] T090 [P] Implement ExtensionPortManager.RemoveMapping(callID string) in F:/tctool-go/extension.go
- [ ] T091 Implement ExtensionPortManager.ReleaseCallPorts(callID string, tsAESKey string) in F:/tctool-go/extension.go
- [ ] T092 Update HandleCallTermination() in sipmsg.go to use ExtensionPortManager.ReleaseCallPorts()
- [ ] T093 Update StartPortForwarding() in tunnel.go to accept ExtensionConnection parameter
- [ ] T094 Update handlePortForwarding() in tunnel.go to route RTP to SIPP source port for all extensions

---

## Phase 10: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T095 [P] Add comprehensive error messages with extension context throughout codebase
- [ ] T096 [P] Ensure all mutex lock/unlock operations use defer pattern in F:/tctool-go/extension.go
- [ ] T097 [P] Add panic recovery in all goroutines (heartbeat, cleanup, forwarding) in F:/tctool-go/extension.go
- [ ] T098 Verify all WaitGroup operations properly track goroutine lifecycle in main.go
- [ ] T099 Add integration test for 3 concurrent extensions with full call flow in F:/tctool-go/tests/integration_test.go
- [ ] T100 Add race condition test running with `go test -race` in F:/tctool-go/tests/race_test.go
- [ ] T101 Add 24-hour leak test with periodic connection/cleanup cycles in F:/tctool-go/tests/leak_test.go
- [ ] T102 Run quickstart.md validation and update any outdated sections
- [ ] T103 Update README.md (if exists) with multi-extension architecture overview
- [ ] T104 Verify all logging includes extension number for traceability

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Story 1 (Phase 3)**: Depends on Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (Phase 4)**: Depends on Foundational (Phase 2) - Can run parallel with US1 but logically follows US1 for testing
- **User Story 3 (Phase 5)**: Depends on US1 and US2 completion - Builds on connection isolation and parsing
- **User Story 4 (Phase 6)**: Depends on US1 completion - Can run parallel with US3 and US5
- **User Story 5 (Phase 7)**: Depends on US3 completion - Requires capacity management from US3
- **TCP Reconnection (Phase 8)**: Depends on US1 completion - Can run parallel with US4, US5
- **Port Management (Phase 9)**: Depends on US1 completion - Can run parallel with US4, US5, Phase 8
- **Polish (Phase 10)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: ✅ Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P1)**: ✅ Can start after Foundational (Phase 2) - Logically follows US1 but can be parallel
- **User Story 3 (P1)**: ⚠️ Depends on US1 and US2 - Needs connection isolation and parsing working
- **User Story 4 (P2)**: ✅ Depends on US1 only - Can start after US1, parallel with US3/US5
- **User Story 5 (P2)**: ⚠️ Depends on US3 - Needs capacity management working

### Within Each User Story

- Models/structs before functions using them
- Constructors before operations
- Core operations before integration with existing code
- Refactoring main.go after new functionality works

### Parallel Opportunities

- **Phase 1 Setup**: All tasks marked [P] can run in parallel (T003, T004)
- **Phase 2 Foundational**: Tasks T008, T009, T010, T011 can run in parallel after T005-T007
- **Phase 3 US1**: Tasks T012, T013, T016, T017 can run in parallel; T014 depends on T013
- **Phase 4 US2**: Tasks T026, T027 can run in parallel at start
- **Phase 5 US3**: Tasks T038, T039, T040, T044, T045 can run in parallel
- **Phase 6 US4**: Tasks T049, T050, T055 can run in parallel at start
- **Phase 7 US5**: Tasks T063, T064, T065 can run in parallel at start
- **Phase 8 Reconnection**: Tasks T072, T073 can start in parallel
- **Phase 9 Port Mgmt**: Tasks T081, T082, T083, T088, T089, T090 can run in parallel
- **Phase 10 Polish**: Tasks T095, T096, T097 can run in parallel

---

## Parallel Example: User Story 1

```bash
# Launch these tasks in parallel:
T012 [P] [US1]: Implement Extension validation (ext_parser.go)
T013 [P] [US1]: Implement ConnectionPool constructor (extension.go)
T016 [P] [US1]: Implement ExtensionConnection constructor (extension.go)
T017 [P] [US1]: Implement ExtensionAuthManager constructor (extension.go)

# Then sequence:
T018 [US1]: Generate TC AES key (depends on T017)
T019 [US1]: Authenticate connection (depends on T018)
T014 [US1]: ConnectionPool.GetOrCreate (depends on T013, T016)
```

---

## Implementation Strategy

### MVP First (User Story 1 + 2 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 (connection isolation)
4. Complete Phase 4: User Story 2 (parsing)
5. **STOP and VALIDATE**: Test with 3 extensions, verify separate TCP connections
6. Deploy/demo if ready

**MVP Deliverable**: System can handle multiple extensions (3-10) with role-aware parsing and isolated connections

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 + 2 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 3 → Scale to 5000 extensions → Test → Deploy/Demo
4. Add User Story 4 → Lifecycle management → Test → Deploy/Demo
5. Add User Story 5 → Graceful degradation → Test → Deploy/Demo
6. Add Phases 8-9 → Fault tolerance and port management → Test → Deploy/Demo
7. Each increment adds value without breaking previous functionality

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together (Phase 1-2)
2. Once Foundational is done:
   - **Developer A**: User Story 1 (Phase 3) - Connection isolation
   - **Developer B**: User Story 2 (Phase 4) - Parsing (can start in parallel with A)
3. After US1 complete:
   - **Developer A**: User Story 4 (Phase 6) - Lifecycle management
   - **Developer B**: User Story 3 (Phase 5) - Scaling (needs US1+US2)
   - **Developer C**: Phase 8 - Reconnection logic
4. After US3 complete:
   - **Developer D**: User Story 5 (Phase 7) - Graceful degradation
5. Final phases (Port Management, Polish) can be distributed

---

## Notes

- [P] tasks = different files, no dependencies on incomplete work
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Run `go test -race` frequently to catch concurrency issues early
- Use quickstart.md for manual testing scenarios
- All file paths are absolute for clarity
- Focus on implementation; tests are manual per quickstart.md
