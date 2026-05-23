export type Card = string

export interface ParticipantPublic {
  id: string
  name: string
  hasVoted: boolean
  connected: boolean
  spectator: boolean
  vote: Card | null
}

export interface StatePayload {
  roomId: string
  revealed: boolean
  revealAt: number
  autoReveal: boolean
  story: string
  deck: Card[]
  participants: ParticipantPublic[]
  history: HistoryEntry[]
}

export interface HistoryEntry {
  story: string
  votes: HistoryVote[]
  at: number
}

export interface HistoryVote {
  name: string
  vote: Card
}

export interface JoinedPayload {
  participantId: string
}

export interface ErrorPayload {
  message: string
}

export type InboundFromServer =
  | { type: 'state'; payload: StatePayload }
  | { type: 'joined'; payload: JoinedPayload }
  | { type: 'error'; payload: ErrorPayload }

export type OutboundToServer =
  | {
      type: 'join'
      payload: {
        roomId: string
        name: string
        rejoinId?: string
        deck?: Card[]
        spectator?: boolean
        create?: boolean
      }
    }
  | { type: 'vote'; payload: { card: Card } }
  | { type: 'reveal' }
  | { type: 'reset' }
  | { type: 'revote' }
  | { type: 'setStory'; payload: { story: string } }
  | { type: 'setAutoReveal'; payload: { enabled: boolean } }
