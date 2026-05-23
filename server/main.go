package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
)

const defaultGraceMs = 15000

func main() {
	graceMs := defaultGraceMs
	if v := os.Getenv("GRACE_TTL_MS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			graceMs = parsed
		}
	}
	hub := NewHub(time.Duration(graceMs) * time.Millisecond)
	log.Printf("grace ttl: %dms", graceMs)

	acceptOpts := buildAcceptOptions()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler(hub, acceptOpts))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("storypoker server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func buildAcceptOptions() *websocket.AcceptOptions {
	if os.Getenv("ALLOW_ALL_ORIGINS") == "1" {
		log.Print("ws origin policy: allow-all (ALLOW_ALL_ORIGINS=1)")
		return &websocket.AcceptOptions{InsecureSkipVerify: true}
	}
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		parts := strings.Split(raw, ",")
		patterns := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				patterns = append(patterns, p)
			}
		}
		log.Printf("ws origin policy: allowlist %v", patterns)
		return &websocket.AcceptOptions{OriginPatterns: patterns}
	}
	log.Print("ws origin policy: strict same-origin (set ALLOWED_ORIGINS or ALLOW_ALL_ORIGINS)")
	return &websocket.AcceptOptions{}
}

func wsHandler(hub *Hub, opts *websocket.AcceptOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, opts)
		if err != nil {
			log.Printf("ws accept failed: %v", err)
			return
		}
		defer ws.Close(websocket.StatusInternalError, "internal error")

		conn := &Conn{ws: ws}
		ctx := r.Context()

		pingCtx, cancelPing := context.WithCancel(ctx)
		defer cancelPing()
		go pingLoop(pingCtx, ws)

		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				var ce websocket.CloseError
				if errors.As(err, &ce) {
					log.Printf("ws closed: code=%d reason=%q", ce.Code, ce.Reason)
				} else {
					log.Printf("ws read err: %v", err)
				}
				break
			}

			var in Inbound
			if err := json.Unmarshal(data, &in); err != nil {
				sendError(conn, "invalid json")
				continue
			}
			handleMessage(hub, conn, in)
		}

		hub.Detach(conn)
		ws.Close(websocket.StatusNormalClosure, "")
	}
}

func handleMessage(hub *Hub, c *Conn, in Inbound) {
	switch in.Type {
	case MsgJoin:
		handleJoin(hub, c, in.Payload)
	case MsgVote:
		handleVote(hub, c, in.Payload)
	case MsgReveal:
		handleReveal(hub, c)
	case MsgReset:
		handleReset(hub, c)
	case MsgRevote:
		handleRevote(hub, c)
	case MsgSetStory:
		handleSetStory(hub, c, in.Payload)
	case MsgSetAutoReveal:
		handleSetAutoReveal(hub, c, in.Payload)
	default:
		sendError(c, "unknown message type: "+in.Type)
	}
}

func handleJoin(hub *Hub, c *Conn, raw json.RawMessage) {
	if c.roomID != "" {
		sendError(c, "already joined")
		return
	}
	var p JoinPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		sendError(c, "bad join payload")
		return
	}
	if p.RoomID == "" || p.Name == "" {
		sendError(c, "roomId and name required")
		return
	}

	if !p.Create && hub.Room(p.RoomID) == nil {
		sendError(c, "room not found")
		return
	}

	deck := SanitizeDeck(p.Deck)
	room := hub.GetOrCreateRoom(p.RoomID, deck)

	var participantID string
	if p.RejoinID != "" && room.HasParticipant(p.RejoinID) {
		participantID = p.RejoinID
		room.SetConnected(participantID, true)
	} else {
		participantID = room.AddParticipant(p.Name, p.Spectator).ID
	}

	c.roomID = p.RoomID
	c.participantID = participantID
	if kicked := hub.Attach(c); kicked != nil {
		_ = kicked.ws.Close(websocket.StatusGoingAway, "replaced by newer connection")
	}

	if err := c.Send(Outbound{Type: MsgJoined, Payload: JoinedPayload{ParticipantID: participantID}}); err != nil {
		log.Printf("send joined failed: %v", err)
	}
	hub.Broadcast(p.RoomID)
}

func handleVote(hub *Hub, c *Conn, raw json.RawMessage) {
	if c.roomID == "" {
		sendError(c, "not in a room")
		return
	}
	var v VotePayload
	if err := json.Unmarshal(raw, &v); err != nil {
		sendError(c, "bad vote payload")
		return
	}
	room := hub.Room(c.roomID)
	if room == nil {
		sendError(c, "room not found")
		return
	}
	if err := room.SetVote(c.participantID, v.Card); err != nil {
		sendError(c, err.Error())
		return
	}
	hub.Broadcast(c.roomID)
}

func handleReveal(hub *Hub, c *Conn) {
	if c.roomID == "" {
		sendError(c, "not in a room")
		return
	}
	room := hub.Room(c.roomID)
	if room == nil {
		sendError(c, "room not found")
		return
	}
	room.Reveal()
	hub.Broadcast(c.roomID)
}

func handleReset(hub *Hub, c *Conn) {
	if c.roomID == "" {
		sendError(c, "not in a room")
		return
	}
	room := hub.Room(c.roomID)
	if room == nil {
		sendError(c, "room not found")
		return
	}
	room.Reset()
	hub.Broadcast(c.roomID)
}

func handleRevote(hub *Hub, c *Conn) {
	if c.roomID == "" {
		sendError(c, "not in a room")
		return
	}
	room := hub.Room(c.roomID)
	if room == nil {
		sendError(c, "room not found")
		return
	}
	room.Revote()
	hub.Broadcast(c.roomID)
}

func handleSetStory(hub *Hub, c *Conn, raw json.RawMessage) {
	if c.roomID == "" {
		sendError(c, "not in a room")
		return
	}
	var p SetStoryPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		sendError(c, "bad setStory payload")
		return
	}
	room := hub.Room(c.roomID)
	if room == nil {
		sendError(c, "room not found")
		return
	}
	room.SetStory(p.Story)
	hub.Broadcast(c.roomID)
}

func handleSetAutoReveal(hub *Hub, c *Conn, raw json.RawMessage) {
	if c.roomID == "" {
		sendError(c, "not in a room")
		return
	}
	var p SetAutoRevealPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		sendError(c, "bad setAutoReveal payload")
		return
	}
	room := hub.Room(c.roomID)
	if room == nil {
		sendError(c, "room not found")
		return
	}
	room.SetAutoReveal(p.Enabled)
	hub.Broadcast(c.roomID)
}

func pingLoop(ctx context.Context, ws *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := ws.Ping(pCtx)
			cancel()
			if err != nil {
				_ = ws.Close(websocket.StatusGoingAway, "ping timeout")
				return
			}
		}
	}
}

func sendError(c *Conn, msg string) {
	if err := c.Send(Outbound{Type: MsgError, Payload: ErrorPayload{Message: msg}}); err != nil {
		log.Printf("send error failed: %v", err)
	}
}
