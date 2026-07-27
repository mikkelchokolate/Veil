package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

const maxSSEConnectionsPerIdentity = 32

type sseSnapshot struct {
	apply   []byte
	traffic []byte
}

type sseBroadcaster struct {
	state   *managementState
	mu      sync.Mutex
	subs    map[chan sseSnapshot]string
	counts  map[string]int
	latest  sseSnapshot
	stop    chan struct{}
	once    sync.Once
	initial sync.Once
}

func newSSEBroadcaster(state *managementState) *sseBroadcaster {
	hub := &sseBroadcaster{state: state, subs: make(map[chan sseSnapshot]string), counts: make(map[string]int), stop: make(chan struct{})}
	go hub.run()
	return hub
}

func (h *sseBroadcaster) run() {
	h.initial.Do(h.refresh)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.refresh()
		case <-h.stop:
			return
		}
	}
}

func (h *sseBroadcaster) refresh() {
	snapshot := h.buildSnapshot()
	h.mu.Lock()
	h.latest = snapshot
	for subscriber := range h.subs {
		select {
		case subscriber <- snapshot:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- snapshot:
			default:
			}
		}
	}
	h.mu.Unlock()
}

func (h *sseBroadcaster) buildSnapshot() sseSnapshot {
	h.state.mu.Lock()
	h.state.clientLifecycleMu.RLock()
	view := h.state.applyStateViewLocked()
	trafficStore := h.state.trafficStore
	clientRepo := h.state.clientRepo
	clientService := h.state.clientService
	h.state.mu.Unlock()
	defer h.state.clientLifecycleMu.RUnlock()
	applyData, _ := json.Marshal(map[string]any{
		"at": time.Now().Unix(), "desiredRevision": view.DesiredRevision,
		"appliedRevision": view.AppliedRevision, "state": view.State,
		"activeJobId": view.ActiveJobID, "lastError": view.LastError,
	})
	snapshot := sseSnapshot{apply: []byte(fmt.Sprintf("event: apply\ndata: %s\n\n", applyData))}
	if trafficStore == nil || (clientRepo == nil && clientService == nil) {
		return snapshot
	}
	var clientIDs []string
	if clientRepo != nil {
		clients, _, err := clientRepo.List(client.ListFilter{PageSize: 1000})
		if err != nil {
			return snapshot
		}
		for _, current := range clients {
			clientIDs = append(clientIDs, current.ID)
		}
	} else {
		clients, _, err := clientService.List(client.ListFilter{PageSize: 1000})
		if err != nil {
			return snapshot
		}
		for _, current := range clients {
			clientIDs = append(clientIDs, current.ID)
		}
	}
	traffic := map[string]any{"at": time.Now().Unix(), "clients": map[string]any{}}
	for _, clientID := range clientIDs {
		upload, download, err := trafficStore.TotalsForClient(clientID)
		if err != nil {
			continue
		}
		traffic["clients"].(map[string]any)[clientID] = map[string]int64{"upload": upload, "download": download}
	}
	trafficData, _ := json.Marshal(traffic)
	snapshot.traffic = []byte(fmt.Sprintf("event: traffic\ndata: %s\n\n", trafficData))
	return snapshot
}

func (h *sseBroadcaster) subscribe(identity string) (<-chan sseSnapshot, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.counts[identity] >= maxSSEConnectionsPerIdentity {
		return nil, nil, errors.New("SSE connection limit exceeded")
	}
	channel := make(chan sseSnapshot, 1)
	h.subs[channel] = identity
	h.counts[identity]++
	if len(h.latest.apply) > 0 || len(h.latest.traffic) > 0 {
		channel <- h.latest
	}
	return channel, func() {
		h.mu.Lock()
		if _, ok := h.subs[channel]; ok {
			delete(h.subs, channel)
			h.counts[identity]--
			if h.counts[identity] == 0 {
				delete(h.counts, identity)
			}
			close(channel)
		}
		h.mu.Unlock()
	}, nil
}

func (h *sseBroadcaster) Close() {
	h.once.Do(func() { close(h.stop) })
}

func (s *managementState) sharedSSEBroadcaster() *sseBroadcaster {
	s.mu.Lock()
	if s.sse == nil {
		s.sse = newSSEBroadcaster(s)
	}
	hub := s.sse
	s.mu.Unlock()
	hub.initial.Do(hub.refresh)
	return hub
}

func parseSSETypes(raw string) map[string]bool {
	result := map[string]bool{}
	if strings.TrimSpace(raw) == "" {
		result["apply"] = true
		result["traffic"] = true
		return result
	}
	for _, item := range strings.Split(raw, ",") {
		switch strings.TrimSpace(item) {
		case "apply":
			result["apply"] = true
		case "traffic":
			result["traffic"] = true
		}
	}
	return result
}

func (s *managementState) serveSharedSSE(w http.ResponseWriter, r *http.Request, types map[string]bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	identity := clientIP(r)
	if username, ok := r.Context().Value(contextKeyUsername).(string); ok && username != "" {
		identity = username + "|" + identity
	}
	updates, release, err := s.sharedSSEBroadcaster().subscribe(identity)
	if err != nil {
		writeError(w, "too many SSE connections", http.StatusTooManyRequests)
		return
	}
	defer release()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case snapshot, ok := <-updates:
			if !ok {
				return
			}
			if types["apply"] && len(snapshot.apply) > 0 {
				if _, err := w.Write(snapshot.apply); err != nil {
					return
				}
			}
			if types["traffic"] && len(snapshot.traffic) > 0 {
				if _, err := w.Write(snapshot.traffic); err != nil {
					return
				}
			}
			if err := controller.Flush(); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}
