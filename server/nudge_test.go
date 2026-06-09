package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestNudgeSpamBroadcasts drives two real websocket clients through the handler
// and asserts that (a) a nudge reaches everyone in the room with the right
// {targetId, fromId} shape, and (b) rapid repeats are all delivered — i.e.
// there is no per-target cooldown, nudges are spammable.
func TestNudgeSpamBroadcasts(t *testing.T) {
	hub := NewHub(time.Hour)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler(hub, &websocket.AcceptOptions{InsecureSkipVerify: true}))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dial := func() *websocket.Conn {
		c, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return c
	}
	send := func(c *websocket.Conn, v any) {
		b, _ := json.Marshal(v)
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// readType returns the payload of the next frame of the wanted type, skipping
	// the interleaved state broadcasts, or fails the test on timeout.
	readType := func(c *websocket.Conn, want string, d time.Duration) json.RawMessage {
		rctx, rcancel := context.WithTimeout(ctx, d)
		defer rcancel()
		for {
			_, data, err := c.Read(rctx)
			if err != nil {
				t.Fatalf("read %q: %v", want, err)
			}
			var m struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(data, &m) != nil || m.Type != want {
				continue
			}
			return m.Payload
		}
	}
	idOf := func(c *websocket.Conn) string {
		var jp struct {
			ParticipantID string `json:"participantId"`
		}
		_ = json.Unmarshal(readType(c, "joined", 2*time.Second), &jp)
		return jp.ParticipantID
	}

	a := dial()
	defer a.Close(websocket.StatusNormalClosure, "")
	send(a, map[string]any{"type": "join", "payload": map[string]any{"roomId": "r", "name": "A", "create": true}})
	aid := idOf(a)

	b := dial()
	defer b.Close(websocket.StatusNormalClosure, "")
	send(b, map[string]any{"type": "join", "payload": map[string]any{"roomId": "r", "name": "B"}})
	bid := idOf(b)

	const spam = 5
	for i := 0; i < spam; i++ {
		send(a, map[string]any{"type": "nudge", "payload": map[string]any{"targetId": bid}})
	}

	for i := 0; i < spam; i++ {
		var ev struct {
			TargetID string `json:"targetId"`
			FromID   string `json:"fromId"`
		}
		_ = json.Unmarshal(readType(b, "nudge", 2*time.Second), &ev)
		if ev.TargetID != bid || ev.FromID != aid {
			t.Fatalf("nudge %d: got target=%q from=%q, want target=%q from=%q", i, ev.TargetID, ev.FromID, bid, aid)
		}
	}
}
