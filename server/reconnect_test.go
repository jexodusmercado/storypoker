package main

import (
	"sync"
	"testing"
	"time"
)

// fakeConn builds a *Conn without a real websocket. Conn.Send no-ops when ws is
// nil, so broadcasts triggered by hub bookkeeping don't panic; these tests
// exercise the registry/grace logic, not the wire.
func fakeConn() *Conn {
	return &Conn{}
}

func participant(snap StatePayload, id string) (ParticipantPublic, bool) {
	for _, p := range snap.Participants {
		if p.ID == id {
			return p, true
		}
	}
	return ParticipantPublic{}, false
}

func roomHas(r *Room, id string) bool {
	_, ok := participant(r.SnapshotFor(""), id)
	return ok
}

// TestReattachKeepsIdentityAndVote: a reconnect within the grace window must
// reuse the same participant (no duplicate), keep its vote, cancel the grace
// timer, and survive a stale grace expiry firing afterward.
func TestReattachKeepsIdentityAndVote(t *testing.T) {
	hub := NewHub(time.Hour) // long TTL: the real timer won't fire mid-test
	hub.GetOrCreateRoom("r", DefaultDeck)
	room := hub.Room("r")

	c1 := fakeConn()
	pid, _, err := hub.JoinOrRejoin(c1, "r", "", "Alice", false)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := room.SetVote(pid, "5"); err != nil {
		t.Fatalf("vote: %v", err)
	}

	hub.Detach(c1) // wifi blip: arms the grace timer, marks disconnected

	c2 := fakeConn()
	rp, kicked, err := hub.JoinOrRejoin(c2, "r", pid, "Alice", false)
	if err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	if rp != pid {
		t.Fatalf("rejoin created a new id %q; want reuse of %q (duplicate bug)", rp, pid)
	}
	if kicked != nil {
		t.Fatalf("rejoin reported a kicked conn; the old conn was already detached")
	}

	snap := room.SnapshotFor(pid)
	if len(snap.Participants) != 1 {
		t.Fatalf("expected exactly 1 participant, got %d (duplicate)", len(snap.Participants))
	}
	p, ok := participant(snap, pid)
	if !ok || !p.Connected || !p.HasVoted {
		t.Fatalf("after rejoin want present+connected+voted, got %+v ok=%v", p, ok)
	}

	// A stale grace timer for pid firing now must be a no-op: the reattach
	// cancelled the pending entry.
	hub.expireGrace("r", pid)
	if room == nil || !roomHas(room, pid) {
		t.Fatalf("reattached participant was removed by a stale grace expiry (missing-member bug)")
	}
}

// TestGraceExpiryRemovesAndGCs: when the grace window truly lapses with no
// reconnect, the participant is removed and the now-empty room is GC'd.
func TestGraceExpiryRemovesAndGCs(t *testing.T) {
	hub := NewHub(time.Hour)
	hub.GetOrCreateRoom("r", DefaultDeck)

	c1 := fakeConn()
	pid, _, err := hub.JoinOrRejoin(c1, "r", "", "Alice", false)
	if err != nil {
		t.Fatalf("join: %v", err)
	}

	hub.Detach(c1)
	hub.expireGrace("r", pid) // simulate the timer firing

	if hub.Room("r") != nil {
		t.Fatalf("empty room should be GC'd after the last participant's grace expires")
	}
}

// TestExpireGraceSkipsWhenConnActive directly exercises the defensive guard:
// even if a pending entry coexists with an active conn, expireGrace must not
// remove the participant. (Detach/JoinOrRejoin keep pending and conn mutually
// exclusive in normal flow; this guard backstops any future refactor that
// breaks that invariant.)
func TestExpireGraceSkipsWhenConnActive(t *testing.T) {
	hub := NewHub(time.Hour)
	hub.GetOrCreateRoom("r", DefaultDeck)

	c1 := fakeConn()
	pid, _, err := hub.JoinOrRejoin(c1, "r", "", "Alice", false)
	if err != nil {
		t.Fatalf("join: %v", err)
	}

	// Manually plant a stale pending timer while c1 is still the active conn.
	hub.mu.Lock()
	e := hub.rooms["r"]
	e.pending[pid] = time.AfterFunc(time.Hour, func() {})
	hub.mu.Unlock()

	hub.expireGrace("r", pid)

	if !roomHas(hub.Room("r"), pid) {
		t.Fatalf("expireGrace removed a participant that still had an active conn")
	}
}

// TestRejoinRaceUnderDetector hammers the detach/reattach interleaving with the
// race detector on. It won't deterministically reproduce the old ordering bug,
// but it guards against data races and deadlocks in the hub.mu/room.mu nesting
// the fix introduces, and asserts the no-ghost / no-duplicate invariant holds
// across thousands of concurrent runs.
func TestRejoinRaceUnderDetector(t *testing.T) {
	hub := NewHub(0) // zero TTL: grace fires immediately, maximal overlap

	for i := 0; i < 2000; i++ {
		hub.GetOrCreateRoom("r", DefaultDeck)
		c1 := fakeConn()
		pid, _, err := hub.JoinOrRejoin(c1, "r", "", "Alice", false)
		if err != nil {
			t.Fatalf("join: %v", err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); hub.Detach(c1) }()
		var rp string
		go func() {
			defer wg.Done()
			rp, _, _ = hub.JoinOrRejoin(fakeConn(), "r", pid, "Alice", false)
		}()
		wg.Wait()
		time.Sleep(time.Millisecond) // let any zero-TTL timer settle

		hub.mu.Lock()
		e, ok := hub.rooms["r"]
		if ok {
			e.room.mu.Lock()
			for cpid := range e.conns {
				if _, present := e.room.Participants[cpid]; !present {
					e.room.mu.Unlock()
					hub.mu.Unlock()
					t.Fatalf("iter %d: stranded conn %s (ghost member)", i, redactID(cpid))
				}
			}
			if rp != "" {
				if _, hasConn := e.conns[rp]; hasConn {
					if _, present := e.room.Participants[rp]; !present {
						e.room.mu.Unlock()
						hub.mu.Unlock()
						t.Fatalf("iter %d: reattached %s has conn but missing from room", i, redactID(rp))
					}
				}
			}
			e.room.mu.Unlock()
		}
		delete(hub.rooms, "r")
		hub.mu.Unlock()
	}
}
