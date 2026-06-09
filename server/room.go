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
	"unicode/utf8"
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
func (r *Room) Reattach(id, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.Participants[id]
	if !ok {
		return false
	}
	p.Connected = true
	// Adopt the name supplied on rejoin so a name the user changed while away
	// takes effect instead of silently keeping the stale one. The caller has
	// already validated it as non-empty and within length limits.
	if name != "" {
		p.Name = name
	}
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

// SetSpectator flips a participant between voter and spectator. Becoming a
// spectator drops any vote they were holding, since spectators never carry one
// (which also keeps them out of the auto-reveal "all voters voted" tally).
func (r *Room) SetSpectator(id string, spectator bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.Participants[id]
	if !ok {
		return ErrParticipantNotFound
	}
	p.Spectator = spectator
	if spectator {
		p.Vote = nil
	}
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
	// Cap by rune count, not bytes: slicing s[:maxStoryLen] on a multibyte
	// string (emoji, accents) can split a rune and leave a mangled trailing
	// character on the wire. maxStoryLen is a character budget.
	if utf8.RuneCountInString(s) > maxStoryLen {
		s = string([]rune(s)[:maxStoryLen])
	}
	r.Story = s
	r.touchActivity()
}

// Snapshot is a per-broadcast view of the room, computed once and then
// personalized cheaply for each viewer via For. The expensive work (allocating
// + sorting the participant list, copying history) happens once here instead of
// once per connection, which is the difference between O(conns) and
// O(conns·participants·log participants) work on the hot broadcast path.
type Snapshot struct {
	base     StatePayload    // participants have Vote populated only when revealed
	revealed bool            // when true, every viewer sees the same base unchanged
	selfVote map[string]Card // pre-reveal: viewerID -> their own vote (voters only)
	idx      map[string]int  // pre-reveal: viewerID -> index into base.Participants
}

// Snapshot builds the shared, viewer-independent view of the room. Pre-reveal,
// every participant's Vote is nil in the base; For fills in just the viewer's
// own. Post-reveal, all votes are populated and identical for everyone.
func (r *Room) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Sort on a value carried alongside each entry rather than re-looking up the
	// participant map inside the comparator (which cost 2 map hashes per
	// comparison, ~2·N·logN per snapshot).
	type entry struct {
		pub      ParticipantPublic
		joinedAt time.Time
	}
	entries := make([]entry, 0, len(r.Participants))
	for _, p := range r.Participants {
		pub := ParticipantPublic{
			ID:        p.ID,
			Name:      p.Name,
			HasVoted:  p.Vote != nil,
			Connected: p.Connected,
			Spectator: p.Spectator,
			Vote:      nil,
		}
		if p.Vote != nil && r.Revealed {
			v := *p.Vote
			pub.Vote = &v
		}
		entries = append(entries, entry{pub: pub, joinedAt: p.JoinedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].joinedAt.Equal(entries[j].joinedAt) {
			return entries[i].joinedAt.Before(entries[j].joinedAt)
		}
		return entries[i].pub.ID < entries[j].pub.ID
	})
	parts := make([]ParticipantPublic, len(entries))
	for i := range entries {
		parts[i] = entries[i].pub
	}

	var selfVote map[string]Card
	var idx map[string]int
	if !r.Revealed {
		idx = make(map[string]int, len(parts))
		for i, pub := range parts {
			idx[pub.ID] = i
		}
		selfVote = make(map[string]Card)
		for _, p := range r.Participants {
			if p.Vote != nil {
				selfVote[p.ID] = *p.Vote
			}
		}
	}

	history := make([]HistoryEntry, len(r.History))
	copy(history, r.History)

	base := StatePayload{
		RoomID:       r.ID,
		Revealed:     r.Revealed,
		RevealAt:     r.RevealAt,
		ServerNow:    time.Now().UnixMilli(),
		AutoReveal:   r.AutoReveal,
		Story:        r.Story,
		Deck:         r.Deck,
		Participants: parts,
		History:      history,
	}
	return Snapshot{base: base, revealed: r.Revealed, selfVote: selfVote, idx: idx}
}

// For returns the state payload as a specific viewer should see it. Post-reveal,
// or for a viewer who hasn't voted, the shared base is returned unchanged (safe
// for concurrent readers — it is never mutated). Pre-reveal, for a viewer who
// has voted, it returns a shallow copy with only that viewer's own Vote filled
// in, so the UI can show "you picked 5" without leaking anyone else's vote.
func (s Snapshot) For(viewerID string) StatePayload {
	if s.revealed {
		return s.base
	}
	vote, voted := s.selfVote[viewerID]
	if !voted {
		return s.base
	}
	parts := make([]ParticipantPublic, len(s.base.Participants))
	copy(parts, s.base.Participants)
	v := vote
	parts[s.idx[viewerID]].Vote = &v
	out := s.base
	out.Participants = parts
	return out
}

// SnapshotFor returns a state payload tailored for a specific viewer. Thin
// wrapper over Snapshot/For for callers (and tests) that want a single payload.
func (r *Room) SnapshotFor(viewerID string) StatePayload {
	return r.Snapshot().For(viewerID)
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
