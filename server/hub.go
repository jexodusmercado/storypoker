package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/time/rate"
)

const (
	writeTimeout = 5 * time.Second

	// DoS knobs.
	maxRooms      = 10000         // hard cap on concurrent rooms in the hub
	staleRoomTTL  = 4 * time.Hour // a room with no activity for this long gets evicted
	sweepInterval = 15 * time.Minute

	// Per-conn rate limit. 20 messages/sec sustained is ~4× peak realistic
	// usage; burst 60 covers quick double-clicks and short bursts.
	rateLimitPerSec = 20
	rateLimitBurst  = 60
)

type Conn struct {
	ws            *websocket.Conn
	participantID string
	roomID        string
	sendMu        sync.Mutex
	limiter       *rate.Limiter
}

func NewConn(ws *websocket.Conn) *Conn {
	return &Conn{
		ws:      ws,
		limiter: rate.NewLimiter(rate.Limit(rateLimitPerSec), rateLimitBurst),
	}
}

func (c *Conn) Send(msg Outbound) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, data)
}

type Hub struct {
	mu       sync.Mutex
	rooms    map[string]*roomEntry
	graceTTL time.Duration
}

type roomEntry struct {
	room        *Room
	conns       map[string]*Conn       // participantID → active conn
	pending     map[string]*time.Timer // participantID → grace timer
	revealTimer *time.Timer            // 3s countdown timer; nil unless counting down
}

func NewHub(graceTTL time.Duration) *Hub {
	h := &Hub{
		rooms:    make(map[string]*roomEntry),
		graceTTL: graceTTL,
	}
	go h.sweepStaleRoomsLoop()
	return h
}

// GetOrCreateRoom returns the existing room with that id, or creates a new one.
// Returns nil if the hub is at the maxRooms cap and creation would be required.
// Existing rooms are returned regardless of the cap (the cap protects against
// unbounded creation, not against joining).
func (h *Hub) GetOrCreateRoom(id string, deck []Card) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e, ok := h.rooms[id]; ok {
		return e.room
	}
	if len(h.rooms) >= maxRooms {
		log.Printf("hub at capacity (%d rooms); refusing to create new room", len(h.rooms))
		return nil
	}
	e := &roomEntry{
		room:    NewRoom(id, deck),
		conns:   make(map[string]*Conn),
		pending: make(map[string]*time.Timer),
	}
	h.rooms[id] = e
	log.Printf("room created: %s", id)
	return e.room
}

func (h *Hub) Room(id string) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e, ok := h.rooms[id]; ok {
		return e.room
	}
	return nil
}

// Attach registers c as the active conn for c.participantID in c.roomID.
// If a pending grace timer exists for this participant, it is canceled.
// If another conn was already active for this participant, it is returned
// so the caller can close it after releasing the hub mutex.
func (h *Hub) Attach(c *Conn) (kicked *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.rooms[c.roomID]
	if !ok {
		return nil
	}
	if timer, ok := e.pending[c.participantID]; ok {
		timer.Stop()
		delete(e.pending, c.participantID)
	}
	kicked = e.conns[c.participantID]
	e.conns[c.participantID] = c
	return kicked
}

// Detach is called when a conn's read loop ends. If this conn is still the
// active conn for its participant, the participant enters the grace window.
// Otherwise (already replaced by a newer conn) this is a no-op.
func (h *Hub) Detach(c *Conn) {
	if c.roomID == "" || c.participantID == "" {
		return
	}

	h.mu.Lock()
	e, ok := h.rooms[c.roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	if e.conns[c.participantID] != c {
		h.mu.Unlock()
		return
	}
	delete(e.conns, c.participantID)

	pid := c.participantID
	rid := c.roomID
	room := e.room

	e.pending[pid] = time.AfterFunc(h.graceTTL, func() {
		h.expireGrace(rid, pid)
	})
	h.mu.Unlock()

	room.SetConnected(pid, false)
	h.Broadcast(c.roomID)
}

func (h *Hub) expireGrace(roomID, participantID string) {
	var room *Room
	var roomGone bool

	h.mu.Lock()
	e, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	if _, pending := e.pending[participantID]; !pending {
		h.mu.Unlock()
		return
	}
	delete(e.pending, participantID)
	room = e.room
	if len(e.conns) == 0 && len(e.pending) == 0 {
		delete(h.rooms, roomID)
		roomGone = true
	}
	h.mu.Unlock()

	room.RemoveParticipant(participantID)

	if roomGone {
		log.Printf("room gc'd: %s", roomID)
		return
	}
	h.Broadcast(roomID)
}

// ScheduleReveal starts the reveal countdown for a room. If a reveal is
// already counting down or completed, or no one has voted, it's a no-op and
// returns false. Broadcasts the new state immediately on success.
func (h *Hub) ScheduleReveal(roomID string, delay time.Duration) bool {
	h.mu.Lock()
	e, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return false
	}
	if e.revealTimer != nil {
		h.mu.Unlock()
		return false
	}
	room := e.room
	at := time.Now().Add(delay).UnixMilli()
	if !room.StartReveal(at) {
		h.mu.Unlock()
		return false
	}
	rid := roomID
	e.revealTimer = time.AfterFunc(delay, func() {
		h.finalizeReveal(rid)
	})
	h.mu.Unlock()
	h.Broadcast(roomID)
	return true
}

// CancelReveal stops a pending countdown (if any) and clears RevealAt. Used
// by Reset and Revote to make sure the timer never fires after the round
// state has been wiped.
func (h *Hub) CancelReveal(roomID string) {
	h.mu.Lock()
	e, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	if e.revealTimer != nil {
		e.revealTimer.Stop()
		e.revealTimer = nil
	}
	room := e.room
	h.mu.Unlock()
	room.CancelReveal()
}

func (h *Hub) finalizeReveal(roomID string) {
	h.mu.Lock()
	e, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	e.revealTimer = nil
	room := e.room
	h.mu.Unlock()

	if room.CompleteReveal() {
		h.Broadcast(roomID)
	}
}

func (h *Hub) Broadcast(roomID string) {
	h.mu.Lock()
	e, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	conns := make([]*Conn, 0, len(e.conns))
	for _, c := range e.conns {
		conns = append(conns, c)
	}
	room := e.room
	h.mu.Unlock()

	// Each conn gets its own snapshot so the viewer sees their own vote
	// pre-reveal while everyone else's vote stays stripped to nil.
	for _, c := range conns {
		state := room.SnapshotFor(c.participantID)
		msg := Outbound{Type: MsgState, Payload: state}
		if err := c.Send(msg); err != nil {
			log.Printf("send to %s failed: %v", redactID(c.participantID), err)
		}
	}
}

// redactID returns a short prefix of a participant ID for logs. Full IDs
// double as rejoin session tokens, so we keep them out of log streams that
// might be exported (Railway log drains, etc.).
func redactID(id string) string {
	if len(id) <= 6 {
		return "…"
	}
	return id[:6] + "…"
}

// sweepStaleRoomsLoop runs forever, evicting rooms that haven't seen activity
// in staleRoomTTL. Catches "forgotten browser tab" zombies — rooms with an
// active ws connection but no user interaction for hours.
func (h *Hub) sweepStaleRoomsLoop() {
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for range t.C {
		h.sweepStaleRooms()
	}
}

func (h *Hub) sweepStaleRooms() {
	cutoff := time.Now().Add(-staleRoomTTL)

	h.mu.Lock()
	type victim struct {
		id    string
		entry *roomEntry
	}
	victims := make([]victim, 0)
	for id, e := range h.rooms {
		if e.room.LastActivityAt().Before(cutoff) {
			victims = append(victims, victim{id: id, entry: e})
		}
	}
	for _, v := range victims {
		// Stop timers and close conns under the hub mu so no one else can
		// observe a half-deleted entry.
		if v.entry.revealTimer != nil {
			v.entry.revealTimer.Stop()
		}
		for _, timer := range v.entry.pending {
			timer.Stop()
		}
		for _, c := range v.entry.conns {
			_ = c.ws.Close(websocket.StatusGoingAway, "session expired")
		}
		delete(h.rooms, v.id)
	}
	h.mu.Unlock()

	for _, v := range victims {
		log.Printf("room evicted (stale): %s", v.id)
	}
}
