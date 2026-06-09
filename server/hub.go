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
	// A state frame is sub-KB; a write that can't drain in this long means a
	// stalled/dead peer. Kept tight so a bad conn frees its broadcast goroutine
	// quickly (the ping loop reaps the connection itself).
	writeTimeout = 3 * time.Second

	// DoS knobs.
	maxRooms      = 10000         // hard cap on concurrent rooms in the hub
	staleRoomTTL  = 4 * time.Hour // a room with no activity for this long gets evicted
	sweepInterval = 15 * time.Minute

	// Per-conn rate limit. 20 messages/sec sustained is ~4× peak realistic
	// usage; burst 60 covers quick double-clicks and short bursts.
	rateLimitPerSec = 20
	rateLimitBurst  = 60

	// A given participant can't be nudged more than once per this window, so a
	// nudge can't be spammed into an annoyance. Comfortably longer than the
	// client's ~0.6s shake animation.
	nudgeCooldown = 2 * time.Second
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
	if c.ws == nil {
		return nil
	}
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
	lastNudge   map[string]time.Time   // participantID → last time they were nudged (cooldown)
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
		room:      NewRoom(id, deck),
		conns:     make(map[string]*Conn),
		pending:   make(map[string]*time.Timer),
		lastNudge: make(map[string]time.Time),
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

// JoinOrRejoin atomically resolves a (re)join and registers c as the active
// conn for the resolved participant. It holds hub.mu for the entire decision so
// that a concurrent grace expiry (which also takes hub.mu) can never interleave:
// either the participant is reused before the reaper runs, or the reaper has
// already removed them and we fall through to creating a fresh participant —
// never the stranded-ghost / duplicate states the previous split-lock path
// allowed.
//
//   - rejoinID names a still-present participant → reuse it, cancel its grace
//     timer, mark connected.
//   - otherwise → create a fresh participant (its vote/identity start clean,
//     which is the expected outcome once a grace window has lapsed).
//
// Returns the resolved participant ID and any older conn for that participant
// that must be closed by the caller after locks are released.
func (h *Hub) JoinOrRejoin(c *Conn, roomID, rejoinID, name string, spectator bool) (participantID string, kicked *Conn, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.rooms[roomID]
	if !ok {
		return "", nil, ErrParticipantNotFound // room gone between create and join
	}

	if rejoinID != "" && e.room.Reattach(rejoinID, name) {
		participantID = rejoinID
	} else {
		p, addErr := e.room.AddParticipant(name, spectator)
		if addErr != nil {
			return "", nil, addErr
		}
		participantID = p.ID
	}

	if timer, ok := e.pending[participantID]; ok {
		timer.Stop()
		delete(e.pending, participantID)
	}
	c.roomID = roomID
	c.participantID = participantID
	kicked = e.conns[participantID]
	e.conns[participantID] = c
	return participantID, kicked, nil
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

	// If the participant reconnected while this timer was firing, a conn is now
	// registered for them (JoinOrRejoin runs under the same hub.mu). Keep them.
	if _, hasConn := e.conns[participantID]; hasConn {
		h.mu.Unlock()
		return
	}

	// Remove the participant while still holding hub.mu so that a concurrent
	// JoinOrRejoin observes a consistent state: it either sees the participant
	// present (and reuses it before we get here) or absent (and creates a fresh
	// one). The previous code removed outside the lock, letting a rejoin reuse a
	// participant we were about to delete — stranding the connection.
	e.room.RemoveParticipant(participantID)
	delete(e.lastNudge, participantID)
	if len(e.conns) == 0 && len(e.pending) == 0 {
		delete(h.rooms, roomID)
		roomGone = true
	}
	room := e.room
	h.mu.Unlock()

	if roomGone {
		log.Printf("room gc'd: %s", roomID)
		return
	}

	// Removing a voter can newly satisfy the auto-reveal condition: if the
	// removed participant was the last one yet to vote, everyone still present
	// has now voted. Re-evaluate so the round doesn't get stranded at
	// "all-but-one voted" with no one left to trigger it. ScheduleReveal
	// broadcasts on success, so only broadcast ourselves otherwise.
	if room.ShouldAutoReveal() {
		h.ScheduleReveal(roomID, revealCountdown)
	} else {
		h.Broadcast(roomID)
	}
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

	// Compute the shared room view once, then personalize it per viewer (only
	// the viewer's own pre-reveal vote differs). Sends fan out concurrently so
	// one client with a stalled write can't hold up delivery to everyone else
	// in the room for up to writeTimeout; per-conn writes are still serialized
	// by Conn.sendMu, and For returns either the shared (read-only) base or a
	// private copy, so concurrent reads are safe.
	snap := room.Snapshot()
	var wg sync.WaitGroup
	wg.Add(len(conns))
	for _, c := range conns {
		go func(c *Conn) {
			defer wg.Done()
			state := snap.For(c.participantID)
			if err := c.Send(Outbound{Type: MsgState, Payload: state}); err != nil {
				log.Printf("send to %s failed: %v", redactID(c.participantID), err)
			}
		}(c)
	}
	wg.Wait()
}

// Nudge delivers an ephemeral "buzz" for targetID to everyone in the room,
// subject to a per-target cooldown. It is intentionally NOT room state: nothing
// is persisted, it's a fire-and-forget event. Silently drops if the room is
// gone, the target isn't currently connected, or the cooldown hasn't elapsed.
func (h *Hub) Nudge(roomID, fromID, targetID string) {
	h.mu.Lock()
	e, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	if _, online := e.conns[targetID]; !online {
		h.mu.Unlock()
		return // can't buzz someone who isn't here
	}
	now := time.Now()
	if last, seen := e.lastNudge[targetID]; seen && now.Sub(last) < nudgeCooldown {
		h.mu.Unlock()
		return
	}
	e.lastNudge[targetID] = now
	conns := make([]*Conn, 0, len(e.conns))
	for _, c := range e.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()

	evt := Outbound{Type: MsgNudge, Payload: NudgeEvent{TargetID: targetID, FromID: fromID}}
	for _, c := range conns {
		if err := c.Send(evt); err != nil {
			log.Printf("nudge send to %s failed: %v", redactID(c.participantID), err)
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
