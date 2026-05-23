package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var DefaultDeck = []Card{"0", "1", "2", "3", "5", "8", "13", "21", "?", "☕"}

var (
	ErrParticipantNotFound = errors.New("participant not found")
	ErrCardNotInDeck       = errors.New("card not in deck")
	ErrVotingClosed        = errors.New("voting closed after reveal")
	ErrSpectatorCannotVote = errors.New("spectators cannot vote")
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
	AutoReveal   bool
	Story        string
	History      []HistoryEntry
	Participants map[string]*Participant
	mu           sync.Mutex
}

const (
	maxStoryLen   = 200
	maxHistoryLen = 100
)

func NewRoom(id string, deck []Card) *Room {
	if len(deck) == 0 {
		deck = DefaultDeck
	}
	return &Room{
		ID:           id,
		Deck:         deck,
		Participants: make(map[string]*Participant),
	}
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

func (r *Room) AddParticipant(name string, spectator bool) *Participant {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	return p
}

func (r *Room) RemoveParticipant(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Participants, id)
}

func (r *Room) HasParticipant(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.Participants[id]
	return ok
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
	}
}

func (r *Room) IsEmpty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Participants) == 0
}

func (r *Room) SetVote(participantID string, card Card) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Revealed {
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

	if r.AutoReveal && r.allVotersVotedLocked() {
		r.Revealed = true
	}
	return nil
}

func (r *Room) SetAutoReveal(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.AutoReveal = enabled
	if enabled && !r.Revealed && r.allVotersVotedLocked() {
		r.Revealed = true
	}
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

func (r *Room) Reveal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Revealed = true
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
	r.Story = ""
	for _, p := range r.Participants {
		p.Vote = nil
	}
}

func (r *Room) Revote() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Revealed = false
	for _, p := range r.Participants {
		p.Vote = nil
	}
}

func (r *Room) SetStory(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(s) > maxStoryLen {
		s = s[:maxStoryLen]
	}
	r.Story = s
}

func (r *Room) Snapshot() StatePayload {
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
		if r.Revealed && p.Vote != nil {
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
