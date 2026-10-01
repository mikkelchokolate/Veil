package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/hysteria2"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

// Internal Hysteria2 HTTP authentication callback (#1173).
//
// apernet/hysteria's auth.type=http contract: the daemon POSTs
// {"addr": "<client ip:port>", "auth": "<payload>", "tx": <bps>} to the URL in
// auth.http.url on EVERY session admission and requires HTTP 200 with
// {"ok": true, "id": "<session identity>"}; any other status or body is an
// auth failure, so the endpoint fails closed by construction. The payload
// format is identical to userpass ("username:password"), so the callback
// authenticates the exact credential set the renderer would have emitted and
// then enforces the per-client device/ip limits static auth cannot express.
//
// Transport security: the listener binds 127.40.0.1:61001 — inside the only
// loopback band veil-hysteria2@.service units may dial (runtimeports). The
// daemon-facing URL carries a per-inbound derived secret in its path
// (hysteria2.HTTPAuthURL) so only a unit that read its own rendered config
// can reach a working auth decision; the secret is never exposed via API.

const (
	// maxHy2AuthBodyBytes bounds the auth request body — it is a three-field
	// JSON object, so anything larger is malformed.
	maxHy2AuthBodyBytes int64 = 8 << 10
	// defaultHy2AuthSessionTTL bounds pending-admission bookkeeping: an
	// admission that never reaches the /online table (dead connection, lost
	// daemon update) holds its slot for at most this long before the entry
	// expires and frees the slot. Sessions that DO reach the table are then
	// tracked for as long as /online still contains them — no wall-clock TTL
	// applies to a live session (#1180).
	defaultHy2AuthSessionTTL = 30 * time.Second
	// maxHy2AuthInFlight bounds concurrent auth admissions — each can run
	// credential decryption and a /online stats read for a remote-triggered
	// request, so unbounded concurrency is an amplification surface (#1206).
	maxHy2AuthInFlight = 32
	// maxHy2AuthRebindAttempts/hy2AuthRebindBackoff bound the listener's
	// self-recovery after a Serve failure: enough retries to ride out a
	// transient error without a spin loop — a persistent failure converges
	// on the next reload instead (#1206).
	maxHy2AuthRebindAttempts = 3
	hy2AuthRebindBackoff     = time.Second
)

type hy2AuthRequest struct {
	Addr string `json:"addr"`
	Auth string `json:"auth"`
	Tx   uint64 `json:"tx"`
}

type hy2AuthResponse struct {
	OK bool   `json:"ok"`
	ID string `json:"id,omitempty"`
}

// hy2PendingAdmission pins the client's /online watermark observed when the
// session was admitted: the admission is assumed absorbed into the daemon's
// table only once the observed count moves above that watermark.
type hy2PendingAdmission struct {
	expires       time.Time
	onlineAtAdmit int64
}

// hy2LiveSession records one admitted session for ipLimit accounting, keyed
// by the session tuple (inbound + the client addr the daemon reports). An
// entry is "pending" until the /online count can credibly contain it
// (watermark promotion), then "registered"; a pending entry that never
// promotes dies at `expires` — the admission evidently failed to register.
// Registered entries carry no wall-clock TTL: they survive for exactly as
// long as the /online count still has room for them, which is what makes
// ipLimit a LIVE-session bound instead of an admission-history window — the
// defect behind #1180. `admittedAt` is the last auth sighting of the tuple
// and orders retirement candidates when the count shrinks.
type hy2LiveSession struct {
	ip            netip.Addr
	admittedAt    time.Time
	expires       time.Time
	onlineAtAdmit int64
	registered    bool
}

// hy2AdmissionTracker enforces deviceLimit and ipLimit at session admission
// against the daemon's live /online session table. Both limits share one
// mutex and one commit point: every check evaluates first, and bookkeeping
// is written only after ALL set limits pass, so an admission denied by one
// limit can never squat on the other limit's slots (#1180 secondary defect).
//
// /online is authoritative for the client's live session COUNT but anonymous
// about which sessions it counts, so the trackers reconcile per-session
// records (keyed by the unique 4-tuple the daemon reports) against it:
//
//   - pending entries (deviceLimit) bridge the auth-ok → /online gap for a
//     short TTL, credited against watermark absorption exactly as before.
//   - live entries (ipLimit) carry the source IP. A pending entry whose
//     admission watermark is below the current count is promoted to
//     registered — the count plausibly contains it — limited to the count
//     slots registered entries do not already fill. A pending entry that is
//     never promoted dies at its TTL (dead admission, slot freed).
//   - when the count drops below the number of registered entries, the
//     excess are dead sessions. The table cannot name which, so the tracker
//     retires records that lose the least information first — an entry whose
//     IP a sibling entry still covers cannot shrink the tracked IP set, then
//     the longest-silent entry is the best guess for a dead session. A wrong
//     retirement can only un-track a live session's IP (a bounded residual
//     vs the exact per-IP session table the stats API does not expose);
//     keeping a dead entry only squats on its own already-seen IP.
//   - count sessions with no matching record (panel restart while clients
//     stayed connected) are unaccounted live sessions: they occupy
//     unknown-IP slots and deny new IPs fail-closed until they disconnect.
type hy2AdmissionTracker struct {
	mu       sync.Mutex
	ttl      time.Duration
	now      func() time.Time
	pending  map[string]map[string]hy2PendingAdmission
	sessions map[string]map[string]*hy2LiveSession
}

func newHy2AdmissionTracker(ttl time.Duration, now func() time.Time) *hy2AdmissionTracker {
	if now == nil {
		now = time.Now
	}
	return &hy2AdmissionTracker{
		ttl:      ttl,
		now:      now,
		pending:  make(map[string]map[string]hy2PendingAdmission),
		sessions: make(map[string]map[string]*hy2LiveSession),
	}
}

// admit reports whether clientID may admit a session under the given limits.
// key is the session's unique tuple (inbound + addr), ip its parsed source
// address (required only when ipLimit > 0), and online the client's folded
// /online session count read just before this call. On true the admission is
// committed; on false the denied admission itself recorded nothing — (false,
// reason) names the limit that denied — though the ipLimit gate's reconcile
// pass may still promote or retire OTHER pre-existing records.
func (t *hy2AdmissionTracker) admit(clientID, key string, ip netip.Addr, online int64, deviceLimit, ipLimit int) (bool, string) {
	if deviceLimit < 1 && ipLimit < 1 {
		return true, ""
	}
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()

	pending, ok := t.pending[clientID]
	if !ok {
		pending = make(map[string]hy2PendingAdmission)
		t.pending[clientID] = pending
	}
	sessions, ok := t.sessions[clientID]
	if !ok {
		sessions = make(map[string]*hy2LiveSession)
		t.sessions[clientID] = sessions
	}
	// Empty submaps must not outlive their last entry: a client whose
	// admissions all expired or reconciled away would otherwise pin a map
	// allocation until the client is deleted (#1206). Deferred so commit
	// writes below still land in the parent maps.
	defer func() {
		if len(pending) == 0 {
			delete(t.pending, clientID)
		}
		if len(sessions) == 0 {
			delete(t.sessions, clientID)
		}
	}()
	// Lazy expiry keeps the pending map bounded without a background reaper.
	for seen, entry := range pending {
		if !entry.expires.After(now) {
			delete(pending, seen)
		}
	}

	// A re-auth for a tuple the tracker still believes pending or live is the
	// same QUIC connection, never a new slot: refresh and admit without
	// re-running the limits.
	pendingTracked := false
	if entry, tracked := pending[key]; tracked && entry.expires.After(now) {
		pendingTracked = true
	}
	sessionTracked := false
	if s := sessions[key]; s != nil && (s.registered || s.expires.After(now)) {
		sessionTracked = true
	}
	if pendingTracked || sessionTracked {
		if deviceLimit > 0 {
			entry, tracked := pending[key]
			if !tracked {
				entry.onlineAtAdmit = online
				if s := sessions[key]; s != nil {
					entry.onlineAtAdmit = s.onlineAtAdmit
				}
			}
			entry.expires = now.Add(t.ttl)
			pending[key] = entry
		}
		if s := sessions[key]; s != nil {
			s.admittedAt = now
			s.expires = now.Add(t.ttl)
		}
		return true, ""
	}

	// --- deviceLimit evaluation (no commit yet) ---
	// Entries admitted at a watermark below the current /online count may
	// already be absorbed into that count — but online credits at most one
	// session per entry, so the absorbed allowance is min(#below, online).
	// Everything else is still invisible to the daemon table and must count
	// as pending: a fast registration cannot double-count (its entry falls
	// inside the absorbed allowance) and a slow one cannot reopen the slot
	// (its entry stays pending regardless of age). Residuals: absorption is
	// identity-agnostic — any /online increase credits low-watermark entries
	// without matching which session registered — and an entry whose session
	// dies before ever registering squats on its slot until the TTL; both
	// land on the fail-closed side within the TTL window.
	if deviceLimit > 0 {
		var below, live int64
		for _, entry := range pending {
			live++
			if entry.onlineAtAdmit < online {
				below++
			}
		}
		pendingEffective := live - min(below, online)
		if online+pendingEffective >= int64(deviceLimit) {
			return false, "device_limit"
		}
	}

	// --- ipLimit evaluation (no commit yet) ---
	if ipLimit > 0 {
		t.reconcileSessionsLocked(sessions, online, now)
		liveIPs := make(map[netip.Addr]struct{}, len(sessions))
		var registered int64
		for _, s := range sessions {
			liveIPs[s.ip] = struct{}{}
			if s.registered {
				registered++
			}
		}
		// Sessions the daemon counts but the tracker never recorded (panel
		// restart, tracker reset) occupy unknown-IP slots — denying a new IP
		// fail-closed is strictly correct because any of them could be a
		// distinct address already at the limit.
		unaccounted := online - registered
		if unaccounted < 0 {
			unaccounted = 0
		}
		if _, covered := liveIPs[ip]; !covered && int64(len(liveIPs))+unaccounted >= int64(ipLimit) {
			return false, "ip_limit"
		}
	}

	// Every set limit passed — only now commit.
	if deviceLimit > 0 {
		pending[key] = hy2PendingAdmission{expires: now.Add(t.ttl), onlineAtAdmit: online}
	}
	if ipLimit > 0 {
		sessions[key] = &hy2LiveSession{
			ip:            ip,
			admittedAt:    now,
			expires:       now.Add(t.ttl),
			onlineAtAdmit: online,
		}
	}
	return true, ""
}

// reconcileSessionsLocked retires session records the /online count can no
// longer account for, promotes pending records the count plausibly contains,
// and expires pending records that evidently never registered. Call under
// the tracker mutex.
func (t *hy2AdmissionTracker) reconcileSessionsLocked(sessions map[string]*hy2LiveSession, online int64, now time.Time) {
	// Pending records whose TTL already lapsed are dead admissions — expire
	// them BEFORE promotion so a stale record cannot consume a registration
	// slot ahead of a still-live pending peer: promoted records are never
	// swept, so a promoted corpse would pin its IP forever.
	for key, s := range sessions {
		if !s.registered && !s.expires.After(now) {
			delete(sessions, key)
		}
	}
	var registered int64
	for _, s := range sessions {
		if s.registered {
			registered++
		}
	}
	// Registration credit: pending entries admitted below the current count
	// may already be inside it. Promote the oldest candidates first, but only
	// into count slots the registered set does not already fill — anything
	// promoted past the count would be a guessed, not evidenced, liveness.
	if slots := online - registered; slots > 0 {
		var promotable []*hy2LiveSession
		for _, s := range sessions {
			if !s.registered && s.onlineAtAdmit < online {
				promotable = append(promotable, s)
			}
		}
		sort.Slice(promotable, func(i, j int) bool { return promotable[i].admittedAt.Before(promotable[j].admittedAt) })
		for i := int64(0); i < slots && i < int64(len(promotable)); i++ {
			promotable[i].registered = true
			registered++
		}
	}
	// The count shrank below the registered set: exactly (registered -
	// online) recorded sessions are dead, but the table cannot say which.
	// Retire records that cost the least information first — an entry whose
	// IP a sibling still covers cannot shrink the tracked IP set — and after
	// that the longest-silent records, the best available proxy for a dead
	// session. Wrong retirements are the documented residual: they can only
	// free an IP this client already used, never admit a never-seen one.
	if registered > online {
		freq := make(map[netip.Addr]int, len(sessions))
		for _, s := range sessions {
			freq[s.ip]++
		}
		type candidate struct {
			key     string
			session *hy2LiveSession
		}
		registeredEntries := make([]candidate, 0, len(sessions))
		for key, s := range sessions {
			if s.registered {
				registeredEntries = append(registeredEntries, candidate{key: key, session: s})
			}
		}
		sort.Slice(registeredEntries, func(i, j int) bool {
			di := freq[registeredEntries[i].session.ip] > 1
			dj := freq[registeredEntries[j].session.ip] > 1
			if di != dj {
				return di
			}
			return registeredEntries[i].session.admittedAt.Before(registeredEntries[j].session.admittedAt)
		})
		retire := registered - online
		for i := int64(0); i < retire && i < int64(len(registeredEntries)); i++ {
			freq[registeredEntries[i].session.ip]--
			delete(sessions, registeredEntries[i].key)
		}
	}
}

// hy2SessionInbound extracts the inbound half of a session key —
// "<inbound>\x00<addr>" — for inbound-scoped pruning.
func hy2SessionInbound(key string) string {
	if i := strings.IndexByte(key, 0); i >= 0 {
		return key[:i]
	}
	return key
}

// dropClient removes every admission record for a client that left the store
// (delete) — its pending and session maps would otherwise live for the
// process lifetime since nothing reconciles a nonexistent client (#1206).
func (t *hy2AdmissionTracker) dropClient(clientID string) {
	t.mu.Lock()
	delete(t.pending, clientID)
	delete(t.sessions, clientID)
	t.mu.Unlock()
}

// dropInbound removes one client's session tuples for a single inbound — the
// binding-detach counterpart of dropClient: the client's sessions on OTHER
// inbounds must stay tracked (#1206).
func (t *hy2AdmissionTracker) dropInbound(clientID, inboundName string) {
	prefix := inboundName + "\x00"
	t.mu.Lock()
	if pending := t.pending[clientID]; pending != nil {
		for key := range pending {
			if strings.HasPrefix(key, prefix) {
				delete(pending, key)
			}
		}
		if len(pending) == 0 {
			delete(t.pending, clientID)
		}
	}
	if sessions := t.sessions[clientID]; sessions != nil {
		for key := range sessions {
			if strings.HasPrefix(key, prefix) {
				delete(sessions, key)
			}
		}
		if len(sessions) == 0 {
			delete(t.sessions, clientID)
		}
	}
	t.mu.Unlock()
}

// retainClients drops tracker state for every client not in keep — the
// wholesale-table replacement (rollback/restore) counterpart of dropClient.
func (t *hy2AdmissionTracker) retainClients(keep map[string]struct{}) {
	t.mu.Lock()
	for clientID := range t.pending {
		if _, ok := keep[clientID]; !ok {
			delete(t.pending, clientID)
		}
	}
	for clientID := range t.sessions {
		if _, ok := keep[clientID]; !ok {
			delete(t.sessions, clientID)
		}
	}
	t.mu.Unlock()
}

// retainInbounds drops session tuples whose inbound prefix is not a live
// inbound name — the inbound-delete counterpart of dropClient, mirrored on
// pruneHy2AuthSecretsLocked (#1206).
func (t *hy2AdmissionTracker) retainInbounds(keep map[string]struct{}) {
	t.mu.Lock()
	for clientID, pending := range t.pending {
		for key := range pending {
			if _, ok := keep[hy2SessionInbound(key)]; !ok {
				delete(pending, key)
			}
		}
		if len(pending) == 0 {
			delete(t.pending, clientID)
		}
	}
	for clientID, sessions := range t.sessions {
		for key := range sessions {
			if _, ok := keep[hy2SessionInbound(key)]; !ok {
				delete(sessions, key)
			}
		}
		if len(sessions) == 0 {
			delete(t.sessions, clientID)
		}
	}
	t.mu.Unlock()
}

// hy2AuthSecretEntry is the memoized Argon2 path secret for one inbound,
// validated by every input the derivation consumes — the shared password AND
// the per-install CredentialDerivationSecret (#1205). Keyed in
// s.hy2AuthSecrets by inbound name; a rotation of either input overwrites
// the entry, so the map stays bounded by the inbound count.
type hy2AuthSecretEntry struct {
	derivationSecret string
	password         string
	secret           string
}

// hy2AuthServer bundles the listener and server so Close can shut both down.
type hy2AuthServer struct {
	server   *http.Server
	listener net.Listener
}

// pruneHy2AuthSecretsLocked drops cached path secrets for inbound names that
// no longer exist — the cache is keyed by name, so deleting an inbound would
// otherwise leave its derived secret resident until restart. Call under s.mu
// wherever the inbound set is replaced.
func (s *managementState) pruneHy2AuthSecretsLocked() {
	live := make(map[string]struct{}, len(s.inbounds))
	for _, in := range s.inbounds {
		live[in.Name] = struct{}{}
	}
	s.hy2AuthSecrets.Range(func(key, _ any) bool {
		if _, ok := live[key.(string)]; !ok {
			s.hy2AuthSecrets.Delete(key)
		}
		return true
	})
}

// ensureHy2AuthLocked binds the internal auth listener once for the lifetime
// of the management state (it survives restores: it only reads live state
// per request). Caller must hold s.clientLifecycleMu. A bind failure is
// logged and leaves the endpoint down: rendered configs referencing it fail
// closed at the daemon (connection refused denies every session) instead of
// silently skipping limit enforcement. A Serve failure after a successful
// bind clears s.hy2Auth and retries via recoverHy2AuthListener (#1206).
func (s *managementState) ensureHy2AuthLocked() {
	if s.hy2Auth != nil {
		return
	}
	if s.hy2Limiter == nil {
		s.hy2Limiter = newHy2AdmissionTracker(defaultHy2AuthSessionTTL, nil)
	}
	addr := s.hy2AuthListenAddr
	if addr == "" {
		addr = runtimeports.Hysteria2HTTPAuthAddress()
	}
	mux := http.NewServeMux()
	mux.HandleFunc(hysteria2.HTTPAuthPathPrefix, s.serveHy2Auth)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("hy2auth: cannot bind internal auth listener %s — device/ip limit enforcement will fail closed: %v", addr, err)
		return
	}
	s.hy2Auth = &hy2AuthServer{server: srv, listener: ln}
	s.hy2AuthInflight = make(chan struct{}, maxHy2AuthInFlight)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("hy2auth: internal auth listener stopped unexpectedly: %v", err)
			s.recoverHy2AuthListener(srv)
		}
	}()
	log.Printf("hy2auth: internal Hysteria2 auth listener bound on %s", addr)
}

// serveHy2Auth bounds concurrent admissions: each request can run credential
// decryption and a /online stats read, and the only legitimate caller is the
// loopback unit, so excess concurrency is pressure, not parallelism (#1206).
// Overflow denies instead of queueing — the daemon treats a deny as an auth
// failure and retries.
func (s *managementState) serveHy2Auth(w http.ResponseWriter, r *http.Request) {
	if inflight := s.hy2AuthInflight; inflight != nil {
		select {
		case inflight <- struct{}{}:
			defer func() { <-inflight }()
		default:
			writeJSON(w, hy2AuthResponse{OK: false})
			return
		}
	}
	s.handleHy2Auth(w, r)
}

// recoverHy2AuthListener rebinds the internal auth listener after its Serve
// loop died unexpectedly (#1206). The failed server is only detached when
// s.hy2Auth still points at it — a Close/detach or a reload that already
// swapped in a replacement wins — then the bind retries a few times; a
// persistent failure leaves s.hy2Auth nil so the next reload or the next
// Serve error retries again (the endpoint fails closed meanwhile).
func (s *managementState) recoverHy2AuthListener(failed *http.Server) {
	s.mu.Lock()
	s.clientLifecycleMu.Lock()
	if s.hy2Auth == nil || s.hy2Auth.server != failed || s.clientSubsystemStopping {
		s.clientLifecycleMu.Unlock()
		s.mu.Unlock()
		return
	}
	s.hy2Auth = nil
	s.clientLifecycleMu.Unlock()
	s.mu.Unlock()
	// http.Server.Serve always closes the listener on return; Close() the
	// server so any in-flight connection to the dead listener drains.
	_ = failed.Close()

	for attempt := 1; attempt <= maxHy2AuthRebindAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(hy2AuthRebindBackoff)
		}
		s.mu.Lock()
		s.clientLifecycleMu.Lock()
		if s.clientSubsystemStopping || s.hy2Auth != nil {
			s.clientLifecycleMu.Unlock()
			s.mu.Unlock()
			return
		}
		s.ensureHy2AuthLocked()
		rebound := s.hy2Auth != nil
		s.clientLifecycleMu.Unlock()
		s.mu.Unlock()
		if rebound {
			return
		}
	}
	log.Printf("hy2auth: auth listener still unbound after %d rebind attempts — next reload or state change retries", maxHy2AuthRebindAttempts)
}

// pruneHy2AdmissionClient drops all admission-tracker state for a client
// that left the store (client delete). Without it a deleted client's
// pending/session records linger for the panel's lifetime (#1206).
func (s *managementState) pruneHy2AdmissionClient(clientID string) {
	if s.hy2Limiter != nil {
		s.hy2Limiter.dropClient(clientID)
	}
}

// pruneHy2AdmissionInbound drops one client's tracker tuples for a single
// inbound — the binding-detach counterpart of pruneHy2AdmissionClient.
func (s *managementState) pruneHy2AdmissionInbound(clientID, inboundID string) {
	if s.hy2Limiter != nil {
		s.hy2Limiter.dropInbound(clientID, inboundID)
	}
}

// pruneHy2AdmissionInboundsLocked drops tracker tuples whose inbound prefix
// no longer names a live inbound. Call under s.mu wherever the inbound set
// is replaced — alongside pruneHy2AuthSecretsLocked (#1206).
func (s *managementState) pruneHy2AdmissionInboundsLocked() {
	if s.hy2Limiter == nil {
		return
	}
	live := make(map[string]struct{}, len(s.inbounds))
	for _, in := range s.inbounds {
		live[in.Name] = struct{}{}
	}
	s.hy2Limiter.retainInbounds(live)
}

// pruneHy2AdmissionToLiveClients retains tracker state only for client IDs
// still in the store. Call after wholesale client-table replacement
// (rollback, backup restore, subsystem reload) — per-mutation deletions use
// the cheaper targeted drops (#1206). Touches only clientLifecycleMu-guarded
// fields plus a repo read, so initClientSubsystem may call it too.
func (s *managementState) pruneHy2AdmissionToLiveClients() {
	if s.hy2Limiter == nil || s.clientRepo == nil {
		return
	}
	clients, err := s.clientRepo.AllClients()
	if err != nil {
		log.Printf("hy2auth: cannot prune admission tracker for live clients: %v", err)
		return
	}
	keep := make(map[string]struct{}, len(clients))
	for _, c := range clients {
		keep[c.ID] = struct{}{}
	}
	s.hy2Limiter.retainClients(keep)
}

// stopHy2AuthLocked detaches the listener under s.mu; the caller closes it
// after releasing the mutex.
func (s *managementState) detachHy2AuthLocked() *hy2AuthServer {
	current := s.hy2Auth
	s.hy2Auth = nil
	return current
}

// handleHy2Auth serves the internal Hysteria2 auth callback. It is mounted
// ONLY on the dedicated 127.40/16 listener — never on the public API mux,
// where the endpoint-capability table would (correctly) fail it closed and
// session auth would never let a bare daemon through anyway. An unknown
// inbound or wrong per-inbound secret is a deny, never an explanation.
func (s *managementState) handleHy2Auth(w http.ResponseWriter, r *http.Request) {
	deny := func() {
		// The daemon treats any non-200 as a hard auth failure too; 200 +
		// ok:false keeps the wire contract explicit either way.
		writeJSON(w, hy2AuthResponse{OK: false})
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, hysteria2.HTTPAuthPathPrefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		deny()
		return
	}
	inboundName, providedSecret := parts[0], parts[1]

	s.mu.Lock()
	var inbound Inbound
	found := false
	for _, in := range s.inbounds {
		if in.Name == inboundName {
			inbound = in
			found = true
			break
		}
	}
	settings := s.settings
	svc := s.clientService
	repo := s.clientRepo
	stopping := s.clientSubsystemStopping
	limiter := s.hy2Limiter
	online := s.hy2AuthOnline
	s.mu.Unlock()

	if !found || stopping || svc == nil || repo == nil || limiter == nil {
		deny()
		return
	}
	if inbound.Protocol != "hysteria2" || !inbound.Enabled {
		deny()
		return
	}
	// The Argon2 derivation consumes (inbound.Name, shared password,
	// per-install derivation secret), so a cache entry is exact only when all
	// inputs match — a rotation of the shared password or the management-key
	// derivative (#1205) replaces the stored entry instead of accumulating
	// stale ones, bounding the map by the inbound count.
	password := hysteria2.SharedPassword(settings, inbound)
	var expectedSecret string
	if cached, ok := s.hy2AuthSecrets.Load(inbound.Name); ok {
		if entry, ok := cached.(hy2AuthSecretEntry); ok &&
			subtle.ConstantTimeCompare([]byte(entry.password), []byte(password)) == 1 &&
			subtle.ConstantTimeCompare([]byte(entry.derivationSecret), []byte(settings.CredentialDerivationSecret)) == 1 {
			expectedSecret = entry.secret
		}
	}
	if expectedSecret == "" {
		expectedSecret = hysteria2.HTTPAuthSecret(settings, inbound)
		s.hy2AuthSecrets.Store(inbound.Name, hy2AuthSecretEntry{
			derivationSecret: settings.CredentialDerivationSecret, password: password, secret: expectedSecret})
	}
	if subtle.ConstantTimeCompare([]byte(providedSecret), []byte(expectedSecret)) != 1 {
		deny()
		return
	}

	var req hy2AuthRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxHy2AuthBodyBytes)).Decode(&req); err != nil {
		deny()
		return
	}

	// Rebuild the effective credential view exactly like the render path:
	// migrated legacy profiles are suppressed, normalized runtime
	// credentials replace same-named legacy entries, and an enabled binding
	// with no active credential fails the whole resolution closed. ALL of it
	// runs unlocked — repository reads, marker loads, decryption, and
	// serialization must never serialize on s.mu, because this is the
	// remote-facing per-admission path (#1206).
	effective := inbound
	if len(effective.Profiles) > 0 {
		markers, markErr := legacyProfileMarkers(repo)
		if markErr != nil {
			log.Printf("event=hy2_auth_marker_error inbound=%q err=%q (failing closed)", inbound.Name, markErr)
			deny()
			return
		}
		suppressMigratedLegacyProfiles(markers, &effective)
	}
	// Render-path parity (#1200): any binding row — even disabled,
	// expired, depleted, or credential-less — marks the inbound as
	// credential-managed, so an empty merged table must fail closed via
	// HadClientProfiles instead of accepting the shared inbound
	// password.
	if repo != nil {
		count, countErr := repo.CountBindingsForInbound(inbound.Name)
		if countErr != nil {
			log.Printf("event=hy2_auth_credential_error inbound=%q err=%q (failing closed)", inbound.Name, countErr)
			deny()
			return
		}
		effective.HasClientBindings = count > 0
	}
	normalized, err := svc.CredentialsForInbound(inbound.Name)
	if err != nil {
		log.Printf("event=hy2_auth_credential_error inbound=%q err=%q (failing closed)", inbound.Name, err)
		deny()
		return
	}
	effective.RuntimeCredentials = make([]model.RuntimeCredential, 0, len(normalized))
	normalizedByUser := make(map[string]client.BindingCredential, len(normalized))
	for _, cred := range normalized {
		effective.RuntimeCredentials = append(effective.RuntimeCredentials, model.RuntimeCredential{
			Name: cred.Name, Username: cred.Username, Password: cred.Password,
		})
		normalizedByUser[strings.ToLower(cred.Username)] = cred
	}
	merged, err := clientaccess.BuildClientCredentials(effective)
	if err != nil {
		log.Printf("event=hy2_auth_credential_error inbound=%q err=%q (failing closed)", inbound.Name, err)
		deny()
		return
	}

	// Mirror upstream extras/auth semantics: with a non-empty user table the
	// payload must be "user:password"; with none (and no profile history —
	// the fail-closed signal) the whole payload is the shared password.
	if len(merged) == 0 && !effective.HadClientProfiles() {
		expected := hysteria2.SharedPassword(settings, inbound)
		if expected == "" || subtle.ConstantTimeCompare([]byte(req.Auth), []byte(expected)) != 1 {
			deny()
			return
		}
		writeJSON(w, hy2AuthResponse{OK: true})
		return
	}
	user, pass, ok := splitHy2UserPass(req.Auth)
	if !ok {
		deny()
		return
	}
	credentialByUser := make(map[string]string, len(merged))
	for _, cred := range merged {
		credentialByUser[strings.ToLower(cred.Username)] = cred.Password
	}
	expectedPass, exists := credentialByUser[user]
	if !exists || subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPass)) != 1 {
		deny()
		return
	}

	// Limits only attach to normalized bindings — legacy embedded profiles
	// cannot carry them, and an unknown user table entry already failed the
	// credential check above. Both limits are enforced against the daemon's
	// LIVE /online session table: the /online read is required whenever any
	// limit is set, so a stats outage fails closed for deviceLimit and
	// ipLimit alike (an unreadable table can never prove the limit holds).
	norm, isNormalized := normalizedByUser[user]
	if isNormalized && norm.ClientID != "" {
		deviceLimit := 0
		if norm.DeviceLimit != nil {
			deviceLimit = *norm.DeviceLimit
		}
		ipLimit := 0
		if norm.IPLimit != nil {
			ipLimit = *norm.IPLimit
		}
		if deviceLimit > 0 || ipLimit > 0 {
			live, err := s.hy2ClientOnlineSessions(r.Context(), settings, norm.ClientID, online)
			if err != nil {
				log.Printf("event=hy2_auth_online_error inbound=%q client=%q err=%q (failing closed)", inbound.Name, norm.ClientID, err)
				deny()
				return
			}
			var ip netip.Addr
			if ipLimit > 0 {
				var ok bool
				ip, ok = parseHy2AuthAddr(req.Addr)
				if !ok {
					log.Printf("event=hy2_auth_deny reason=unparseable_addr inbound=%q client=%q addr=%q", inbound.Name, norm.ClientID, req.Addr)
					deny()
					return
				}
			}
			// live + pending-admissions vs every set limit in one atomic
			// check — a burst of concurrent auths cannot all win on the same
			// stale /online read, and a deny by either limit commits nothing.
			admitted, reason := limiter.admit(norm.ClientID, inbound.Name+"\x00"+req.Addr, ip, live, deviceLimit, ipLimit)
			if !admitted {
				log.Printf("event=hy2_auth_deny reason=%s inbound=%q client=%q addr=%q online=%d", reason, inbound.Name, norm.ClientID, req.Addr, live)
				deny()
				return
			}
		}
	}
	writeJSON(w, hy2AuthResponse{OK: true, ID: user})
}

// splitHy2UserPass mirrors upstream extras/auth splitUserPass: the payload is
// "username:password", the username is case-insensitive, and a missing
// separator rejects outright.
func splitHy2UserPass(auth string) (user, pass string, ok bool) {
	parts := strings.SplitN(auth, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.ToLower(parts[0]), parts[1], true
}

// parseHy2AuthAddr accepts the daemon's addr field: "ip:port" for the real
// path, bare "ip" tolerated for robustness.
func parseHy2AuthAddr(addr string) (netip.Addr, bool) {
	if addrPort, err := netip.ParseAddrPort(addr); err == nil {
		return addrPort.Addr(), true
	}
	if ip, err := netip.ParseAddr(addr); err == nil {
		return ip, true
	}
	return netip.Addr{}, false
}

// hy2ClientOnlineSessions counts the client's live sessions across every
// enabled hysteria2 inbound it has an enabled binding on — deviceLimit is a
// per-client contract, so the count must aggregate every inbound the client
// could be multiplexed over (#1173). Counts come from each unit's /online
// table, folded identity->binding the same way the traffic collector does.
func (s *managementState) hy2ClientOnlineSessions(ctx context.Context, settings model.Settings, clientID string, online func(context.Context, model.Settings, model.Inbound, map[string]string) (map[string]int64, []string, error)) (int64, error) {
	s.mu.Lock()
	inbounds := append([]Inbound(nil), s.inbounds...)
	repo := s.clientRepo
	s.mu.Unlock()
	if repo == nil {
		return 0, errors.New("client store is not available")
	}
	bindings, err := repo.BindingsForClient(clientID)
	if err != nil {
		return 0, err
	}
	byInbound := make(map[string][]string)
	targets := make(map[string]Inbound)
	for _, binding := range bindings {
		if !binding.Enabled {
			continue
		}
		for _, in := range inbounds {
			if in.Name == binding.InboundID && in.Enabled && in.Protocol == "hysteria2" {
				byInbound[in.Name] = append(byInbound[in.Name], binding.ID)
				targets[in.Name] = in
			}
		}
	}
	if len(byInbound) == 0 {
		return 0, nil
	}
	allBindings, err := repo.AllBindings()
	if err != nil {
		return 0, err
	}
	allClients, err := repo.AllClients()
	if err != nil {
		return 0, err
	}
	if online == nil {
		online = s.hy2ReadOnline
	}
	var total int64
	for name, bindingIDs := range byInbound {
		in := targets[name]
		// Identity folding uses the RAW profile list: a migrated profile's
		// username still maps to its StableClientID binding so live sessions
		// admitted before the migration keep counting toward the right
		// client, and trafficIdentityMap resolves migrated usernames before
		// generic name aliases (#1225). No marker reads needed here — the
		// stats fold only cares which binding a reported username serves.
		identities := trafficIdentityMap(name, in.Profiles, allBindings, allClients, time.Now().Unix())
		counts, _, err := online(ctx, settings, in, identities)
		if err != nil {
			return 0, fmt.Errorf("online sessions for inbound %s: %w", name, err)
		}
		for _, bindingID := range bindingIDs {
			total += counts[bindingID]
		}
	}
	return total, nil
}

// hy2ReadOnline is the production online reader: it builds the same
// authenticated stats provider the traffic collector uses — preferring the
// live rendered unit's own listen/secret — and returns the folded
// bindingID->session-count table.
func (s *managementState) hy2ReadOnline(ctx context.Context, settings model.Settings, inbound model.Inbound, identities map[string]string) (map[string]int64, []string, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/traffic", inbound.Port)
	secret := hysteria2.TrafficStatsSecret(settings, inbound)
	// Mirror buildTrafficProvidersLocked: the running unit may be serving a
	// config rendered from a newer revision than the snapshot this state was
	// loaded from — read its published artifact so the provider authenticates
	// against what the unit actually serves.
	s.mu.Lock()
	liveConfig := filepath.Join(s.liveRoot, "hysteria2", inbound.Name+".yaml")
	s.mu.Unlock()
	if listen, renderedSecret, ok, err := hysteria2.RenderedTrafficStats(liveConfig); err == nil && ok {
		endpoint = "http://" + listen + "/traffic"
		secret = renderedSecret
	}
	provider := hysteria2.NewAuthenticatedStatsProvider("hysteria2:"+inbound.Name, endpoint, secret, identities)
	return provider.Online(ctx)
}
