package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const writeTimeout = 5 * time.Second

type Conn struct {
	ws            *websocket.Conn
	participantID string
	roomID        string
	sendMu        sync.Mutex
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
	room    *Room
	conns   map[string]*Conn       // participantID → active conn
	pending map[string]*time.Timer // participantID → grace timer
}

func NewHub(graceTTL time.Duration) *Hub {
	return &Hub{
		rooms:    make(map[string]*roomEntry),
		graceTTL: graceTTL,
	}
}

func (h *Hub) GetOrCreateRoom(id string, deck []Card) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e, ok := h.rooms[id]; ok {
		return e.room
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

	state := room.Snapshot()
	msg := Outbound{Type: MsgState, Payload: state}
	for _, c := range conns {
		if err := c.Send(msg); err != nil {
			log.Printf("send to %s failed: %v", c.participantID, err)
		}
	}
}
