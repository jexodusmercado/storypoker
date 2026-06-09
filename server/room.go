package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var DefaultDeck = []Card{"0", "1", "2", "3", "5", "8", "13", "21", "?", "☕"}

var (
	ErrParticipantNotFound = errors.New("participant not found")
	ErrCardNotInDeck       = errors.New("card not in deck")
	ErrVotingClosed        = errors.New("voting closed after reveal")
	ErrSpectatorCannotVote = errors.New("spectators cannot vote")
	ErrRoomFull            = errors.New("room is full")
)

type Participant struct {
	ID        string
	Name      string
	Vote      *Card
	Connected bool
	Spectator bool
	JoinedAt  time.Time
	LastSeen  time.Time
}

type Room struct {
	ID           string
	Deck         []Card
	Revealed     bool
	RevealAt     int64 // unix ms; 0 unless a countdown is running
	AutoReveal   bool
	Story        string
	History      []HistoryEntry
	Participants map[string]*Participant
	mu           sync.Mutex

	// lastActivity is read by the hub's sweeper goroutine without holding mu,
	// so we keep it as an atomic and update on every state-mutating method.
	lastActivity atomic.Int64 // unix nanos
}

const (
	maxStoryLen            = 200
	maxHistoryLen          = 100
	maxParticipantsPerRoom = 50
)

func NewRoom(id string, deck []Card) *Room {
	if len(deck) == 0 {
		deck = DefaultDeck
	}
	r := &Room{
		ID:           id,
		Deck:         deck,
		Participants: make(map[string]*Participant),
	}
	r.lastActivity.Store(time.Now().UnixNano())
	return r
}

// touchActivity records the current time as the room's last meaningful state
// change. Called by every mutating method below. Read by the hub's sweeper
// goroutine via LastActivityAt — atomic because that reader doesn't take r.mu.
func (r *Room) touchActivity() {
	r.lastActivity.Store(time.Now().UnixNano())
}

// LastActivityAt returns the timestamp of the most recent state mutation.
// Safe to call without r.mu.
func (r *Room) LastActivityAt() time.Time {
	return time.Unix(0, r.lastActivity.Load())
}

const (
	maxDeckSize = 32
	maxCardLen  = 16
)

func SanitizeDeck(deck []Card) []Card {
	if len(deck) == 0 || len(deck) > maxDeckSize {
		return nil
	}
	out := make([]Card, 0, len(deck))
	seen := make(map[Card]struct{}, len(deck))
	for _, c := range deck {
		c = Card(strings.TrimSpace(string(c)))
		if c == "" || len(c) > maxCardLen {
			return nil
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *Room) AddParticipant(name string, spectator bool) (*Participant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.Participants) >= maxParticipantsPerRoom {
		return nil, ErrRoomFull
	}
	now := time.Now()
	p := &Participant{
		ID:        newParticipantID(),
		Name:      name,
		Connected: true,
		Spectator: spectator,
		JoinedAt:  now,
		LastSeen:  now,
	}
	r.Participants[p.ID] = p
	r.touchActivity()
	return p, nil
}

func (r *Room) RemoveParticipant(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Participants, id)
}

func (r *Room) SetConnected(id string, connected bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.Participants[id]
	if !ok {
		return
	}
	p.Connected = connected
	if connected {
		p.LastSeen = time.Now()
		r.touchActivity()
	}
}

// Reattach marks an existing participant as connected and returns true. Returns
// false if no such participant exists — e.g. their grace window already expired
// and the reaper removed them. Used by the rejoin path so the "is this
// participant still here?" check and the "claim it" mutation happen under a
// single r.mu acquisition (the caller holds hub.mu across the whole rejoin, so
// a concurrent grace expiry cannot slip a removal between check and claim).
func (r *Room) Reattach(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.Participants[id]
	if !ok {
		return false
	}
	p.Connected = true
	p.LastSeen = time.Now()
	r.touchActivity()
	return true
}

func (r *Room) IsEmpty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Participants) == 0
}

func (r *Room) SetVote(participantID string, card Card) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Revealed || r.RevealAt > 0 {
		return ErrVotingClosed
	}
	p, ok := r.Participants[participantID]
	if !ok {
		return ErrParticipantNotFound
	}
	if p.Spectator {
		return ErrSpectatorCannotVote
	}
	if !r.deckContainsLocked(card) {
		return ErrCardNotInDeck
	}
	c := card
	p.Vote = &c
	p.LastSeen = time.Now()
	r.touchActivity()

	return nil
}

// ShouldAutoReveal reports whether the room state currently meets the
// auto-reveal trigger (all non-spectator voters have voted, auto-reveal is on,
// and nothing is already revealed or counting down). Caller is responsible for
// scheduling the actual reveal countdown.
func (r *Room) ShouldAutoReveal() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.AutoReveal && !r.Revealed && r.RevealAt == 0 && r.allVotersVotedLocked()
}

func (r *Room) SetAutoReveal(enabled bool) (shouldRevealNow bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.AutoReveal = enabled
	r.touchActivity()
	return enabled && !r.Revealed && r.RevealAt == 0 && r.allVotersVotedLocked()
}

// StartReveal marks the room as counting down. Returns false if a reveal is
// already in progress, already done, or no one has voted yet (nothing to reveal).
func (r *Room) StartReveal(at int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Revealed || r.RevealAt > 0 {
		return false
	}
	if !r.anyVoteLocked() {
		return false
	}
	r.RevealAt = at
	r.touchActivity()
	return true
}

// CompleteReveal flips Revealed to true and clears RevealAt. Returns false
// if the countdown was cancelled (RevealAt already zero) so the caller can skip
// the broadcast.
func (r *Room) CompleteReveal() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.RevealAt == 0 {
		return false
	}
	r.Revealed = true
	r.RevealAt = 0
	r.touchActivity()
	return true
}

// CancelReveal clears RevealAt without flipping Revealed. Safe to call when no
// countdown is in progress.
func (r *Room) CancelReveal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RevealAt = 0
}

func (r *Room) anyVoteLocked() bool {
	for _, p := range r.Participants {
		if p.Vote != nil {
			return true
		}
	}
	return false
}

func (r *Room) allVotersVotedLocked() bool {
	any := false
	for _, p := range r.Participants {
		if p.Spectator {
			continue
		}
		any = true
		if p.Vote == nil {
			return false
		}
	}
	return any
}

func (r *Room) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Revealed {
		entry := HistoryEntry{
			Story: r.Story,
			At:    time.Now().UnixMilli(),
			Votes: make([]HistoryVote, 0, len(r.Participants)),
		}
		for _, p := range r.Participants {
			if p.Vote != nil {
				entry.Votes = append(entry.Votes, HistoryVote{Name: p.Name, Vote: *p.Vote})
			}
		}
		sort.Slice(entry.Votes, func(i, j int) bool {
			return entry.Votes[i].Name < entry.Votes[j].Name
		})
		r.History = append(r.History, entry)
		if len(r.History) > maxHistoryLen {
			r.History = r.History[len(r.History)-maxHistoryLen:]
		}
	}
	r.Revealed = false
	r.RevealAt = 0
	r.Story = ""
	for _, p := range r.Participants {
		p.Vote = nil
	}
	r.touchActivity()
}

func (r *Room) Revote() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Revealed = false
	r.RevealAt = 0
	for _, p := range r.Participants {
		p.Vote = nil
	}
	r.touchActivity()
}

func (r *Room) SetStory(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(s) > maxStoryLen {
		s = s[:maxStoryLen]
	}
	r.Story = s
	r.touchActivity()
}

// SnapshotFor returns a state payload tailored for a specific viewer.
// Pre-reveal, the viewer sees their OWN vote (so the UI can show "you picked
// 5"), while every other participant's vote is stripped to nil. Post-reveal,
// all votes are populated. Empty viewerID = no personalization (legacy path).
func (r *Room) SnapshotFor(viewerID string) StatePayload {
	r.mu.Lock()
	defer r.mu.Unlock()
	parts := make([]ParticipantPublic, 0, len(r.Participants))
	for _, p := range r.Participants {
		pub := ParticipantPublic{
			ID:        p.ID,
			Name:      p.Name,
			HasVoted:  p.Vote != nil,
			Connected: p.Connected,
			Spectator: p.Spectator,
			Vote:      nil,
		}
		if p.Vote != nil && (r.Revealed || p.ID == viewerID) {
			v := *p.Vote
			pub.Vote = &v
		}
		parts = append(parts, pub)
	}
	sort.Slice(parts, func(i, j int) bool {
		a, b := r.Participants[parts[i].ID], r.Participants[parts[j].ID]
		if !a.JoinedAt.Equal(b.JoinedAt) {
			return a.JoinedAt.Before(b.JoinedAt)
		}
		return parts[i].ID < parts[j].ID
	})
	history := make([]HistoryEntry, len(r.History))
	copy(history, r.History)

	return StatePayload{
		RoomID:       r.ID,
		Revealed:     r.Revealed,
		RevealAt:     r.RevealAt,
		AutoReveal:   r.AutoReveal,
		Story:        r.Story,
		Deck:         r.Deck,
		Participants: parts,
		History:      history,
	}
}

func (r *Room) deckContainsLocked(c Card) bool {
	for _, d := range r.Deck {
		if d == c {
			return true
		}
	}
	return false
}

func newParticipantID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
