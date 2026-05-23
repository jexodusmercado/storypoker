import { useMemo, useState, type FormEvent } from 'react'
import {
  DECK_LABELS,
  DECK_PRESETS,
  parseCustomDeck,
  type DeckPreset,
} from '../decks'
import { readStoredName, writeStoredName } from '../storage'

type Mode = 'create' | 'join'

interface Props {
  onJoin: (
    roomId: string,
    name: string,
    deck: string[],
    spectator: boolean,
    create: boolean,
  ) => void
}

const DECK_STORAGE_KEY = 'storypoker.deck'

const ROOM_CODE_ALPHABET = 'abcdefghijkmnpqrstuvwxyz23456789'

function generateRoomCode(): string {
  const buf = new Uint8Array(6)
  crypto.getRandomValues(buf)
  let out = ''
  for (const b of buf) {
    out += ROOM_CODE_ALPHABET[b % ROOM_CODE_ALPHABET.length]
  }
  return out
}

function readStoredDeckPreset(): DeckPreset {
  try {
    const stored = window.localStorage.getItem(DECK_STORAGE_KEY) ?? ''
    if (stored === 'custom' || stored in DECK_PRESETS) {
      return stored as DeckPreset
    }
  } catch {
    /* localStorage unavailable */
  }
  return 'fibonacci'
}

function writeStoredDeckPreset(preset: DeckPreset) {
  try {
    window.localStorage.setItem(DECK_STORAGE_KEY, preset)
  } catch {
    /* localStorage unavailable */
  }
}

const fieldLabel = 'text-sm text-ink-muted'
const fieldInput =
  'mt-1 w-full bg-canvas border border-divider rounded px-3 py-2 outline-none focus:ring-2 focus:ring-sage focus:border-sage transition-colors'

export function EntryScreen({ onJoin }: Props) {
  const [mode, setMode] = useState<Mode>('create')
  const [name, setName] = useState(readStoredName)
  const [roomCode, setRoomCode] = useState('')
  const [deckPreset, setDeckPreset] = useState<DeckPreset>(readStoredDeckPreset)
  const [customDeck, setCustomDeck] = useState('1, 2, 3, 5, 8, ?')
  const [spectator, setSpectator] = useState(false)

  const resolvedDeck = useMemo<string[] | null>(() => {
    if (deckPreset === 'custom') return parseCustomDeck(customDeck)
    return [...DECK_PRESETS[deckPreset]]
  }, [deckPreset, customDeck])

  const canSubmit =
    name.trim().length > 0 &&
    (mode === 'create'
      ? resolvedDeck !== null
      : roomCode.trim().length > 0)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!canSubmit) return
    const n = name.trim()
    writeStoredName(n)
    if (mode === 'create') {
      if (!resolvedDeck) return
      writeStoredDeckPreset(deckPreset)
      onJoin(generateRoomCode(), n, resolvedDeck, spectator, true)
    } else {
      onJoin(roomCode.trim(), n, [], spectator, false)
    }
  }

  return (
    <div className="min-h-full flex items-center justify-center p-4 sm:p-6">
      <form
        onSubmit={submit}
        className="w-full max-w-sm space-y-5 bg-surface border border-divider p-6 rounded-xl shadow-sm shadow-ink/5"
      >
        <h1 className="text-2xl font-semibold text-center text-ink">
          Story Poker
        </h1>

        <label className="block">
          <span className={fieldLabel}>Your name</span>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            className={fieldInput}
          />
        </label>

        {mode === 'join' && (
          <label className="block">
            <span className={fieldLabel}>Room code</span>
            <input
              type="text"
              value={roomCode}
              onChange={(e) => setRoomCode(e.target.value.toLowerCase())}
              placeholder="e.g. ab3kx9"
              className={`${fieldInput} font-mono tracking-wide`}
            />
          </label>
        )}

        {mode === 'create' && (
          <label className="block">
            <span className={fieldLabel}>Deck</span>
            <select
              value={deckPreset}
              onChange={(e) => setDeckPreset(e.target.value as DeckPreset)}
              className={fieldInput}
            >
              {(
                Object.keys(DECK_LABELS) as Array<keyof typeof DECK_LABELS>
              ).map((k) => (
                <option key={k} value={k}>
                  {DECK_LABELS[k]}
                </option>
              ))}
              <option value="custom">Custom</option>
            </select>
            {deckPreset === 'custom' && (
              <input
                type="text"
                value={customDeck}
                onChange={(e) => setCustomDeck(e.target.value)}
                placeholder="comma-separated, e.g. 1, 2, 3, 5, 8, ?"
                className={`${fieldInput} mt-2 text-sm`}
              />
            )}
            <p className="mt-2 text-xs text-ink-soft">
              {resolvedDeck
                ? `Preview: ${resolvedDeck.join(', ')}`
                : 'Invalid deck — comma-separated, 1–32 cards, max 16 chars each.'}
            </p>
          </label>
        )}

        <label className="flex items-center gap-2 text-sm text-ink-muted">
          <input
            type="checkbox"
            checked={spectator}
            onChange={(e) => setSpectator(e.target.checked)}
            className="accent-sage-strong"
          />
          Join as spectator (don't vote)
        </label>

        <button
          type="submit"
          disabled={!canSubmit}
          className="w-full bg-sage-strong hover:opacity-90 disabled:bg-divider disabled:text-ink-soft text-white rounded py-2 font-medium transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage"
        >
          {mode === 'create' ? 'Create room' : 'Join room'}
        </button>

        <p className="text-xs text-ink-soft text-center">
          {mode === 'create' ? (
            <>
              Have a code?{' '}
              <button
                type="button"
                onClick={() => setMode('join')}
                className="text-sage-strong hover:text-sage underline-offset-2 hover:underline"
              >
                Join existing →
              </button>
            </>
          ) : (
            <>
              No code?{' '}
              <button
                type="button"
                onClick={() => {
                  setMode('create')
                  setRoomCode('')
                }}
                className="text-sage-strong hover:text-sage underline-offset-2 hover:underline"
              >
                Create a new room →
              </button>
            </>
          )}
        </p>
      </form>
    </div>
  )
}
