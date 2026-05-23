package main

import "encoding/json"

type Card string

const (
	MsgJoin          = "join"
	MsgVote          = "vote"
	MsgReveal        = "reveal"
	MsgReset         = "reset"
	MsgRevote        = "revote"
	MsgSetStory      = "setStory"
	MsgSetAutoReveal = "setAutoReveal"

	MsgState  = "state"
	MsgJoined = "joined"
	MsgError  = "error"
)

type Inbound struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Outbound struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

type JoinPayload struct {
	RoomID    string `json:"roomId"`
	Name      string `json:"name"`
	RejoinID  string `json:"rejoinId,omitempty"`
	Deck      []Card `json:"deck,omitempty"`
	Spectator bool   `json:"spectator,omitempty"`
	Create    bool   `json:"create,omitempty"`
}

type VotePayload struct {
	Card Card `json:"card"`
}

type SetStoryPayload struct {
	Story string `json:"story"`
}

type SetAutoRevealPayload struct {
	Enabled bool `json:"enabled"`
}

type StatePayload struct {
	RoomID       string              `json:"roomId"`
	Revealed     bool                `json:"revealed"`
	RevealAt     int64               `json:"revealAt"` // unix ms; 0 unless countdown in progress
	AutoReveal   bool                `json:"autoReveal"`
	Story        string              `json:"story"`
	Deck         []Card              `json:"deck"`
	Participants []ParticipantPublic `json:"participants"`
	History      []HistoryEntry      `json:"history"`
}

type HistoryEntry struct {
	Story string        `json:"story"`
	Votes []HistoryVote `json:"votes"`
	At    int64         `json:"at"`
}

type HistoryVote struct {
	Name string `json:"name"`
	Vote Card   `json:"vote"`
}

type ParticipantPublic struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	HasVoted  bool   `json:"hasVoted"`
	Connected bool   `json:"connected"`
	Spectator bool   `json:"spectator"`
	Vote      *Card  `json:"vote"`
}

type JoinedPayload struct {
	ParticipantID string `json:"participantId"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}
