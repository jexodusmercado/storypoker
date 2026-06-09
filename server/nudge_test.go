package main

import (
	"testing"
	"time"
)

// TestNudgeCooldownAndTargeting checks the two pieces of logic Hub.Nudge adds
// on top of a plain room broadcast: it only buzzes targets that are actually
// connected, and it enforces a per-target cooldown so a nudge can't be spammed.
// (fakeConn.Send no-ops on a nil ws, so we assert via the cooldown bookkeeping
// rather than the wire.)
func TestNudgeCooldownAndTargeting(t *testing.T) {
	hub := NewHub(time.Hour)
	hub.GetOrCreateRoom("r", DefaultDeck)

	a := fakeConn()
	aid, _, err := hub.JoinOrRejoin(a, "r", "", "Alice", false)
	if err != nil {
		t.Fatalf("join a: %v", err)
	}
	b := fakeConn()
	bid, _, err := hub.JoinOrRejoin(b, "r", "", "Bob", false)
	if err != nil {
		t.Fatalf("join b: %v", err)
	}

	lastNudge := func(id string) (time.Time, bool) {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		ts, ok := hub.rooms["r"].lastNudge[id]
		return ts, ok
	}

	// Alice nudges Bob → records a cooldown stamp for Bob.
	hub.Nudge("r", aid, bid)
	first, ok := lastNudge(bid)
	if !ok {
		t.Fatal("nudge did not record a cooldown stamp for the target")
	}

	// A second nudge within the cooldown window is dropped (stamp unchanged).
	hub.Nudge("r", aid, bid)
	if second, _ := lastNudge(bid); !second.Equal(first) {
		t.Fatal("nudge within the cooldown window should have been dropped")
	}

	// Nudging someone who isn't in the room is a no-op.
	hub.Nudge("r", aid, "ghost")
	if _, ok := lastNudge("ghost"); ok {
		t.Fatal("nudging an absent target should record nothing")
	}

	// When the target leaves, their cooldown bookkeeping is cleaned up.
	hub.Detach(b)
	hub.expireGrace("r", bid)
	if _, ok := lastNudge(bid); ok {
		t.Fatal("cooldown entry should be deleted when the participant is removed")
	}
}
