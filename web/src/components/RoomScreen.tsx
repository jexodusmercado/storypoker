import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { motion } from 'motion/react'
import { useRoom, type ConnectionStatus } from '../useRoom'
import type { Card, HistoryEntry, ParticipantPublic } from '../protocol'
import { OvalTable, SpectatorsStrip } from './Table'
import { FlightCard, type FlightSpec } from './FlightCard'
import { computeOutliers } from '../voteAnalysis'
import { playNudge, unlockAudio } from '../nudgeSound'
import { readNudgeMuted, writeNudgeMuted } from '../storage'

interface Props {
  roomId: string
  name: string
  deck: string[]
  spectator: boolean
  create: boolean
  onLeave: () => void
}

const statusLabel: Record<ConnectionStatus, string> = {
  connecting: 'connecting…',
  joining: 'joining…',
  joined: 'connected',
  reconnecting: 'reconnecting…',
  error: 'connection error',
}

export function RoomScreen({
  roomId,
  name,
  deck,
  spectator,
  create,
  onLeave,
}: Props) {
  const {
    status,
    state,
    participantId,
    error,
    vote,
    reveal,
    reset,
    revote,
    setStory,
    setAutoReveal,
    setSpectator,
    nudge,
    nudgeEvent,
  } = useRoom(roomId, name, deck, spectator, create)

  const me = state?.participants.find((p) => p.id === participantId) ?? null
  const myVote = me?.vote ?? null

  const [flight, setFlight] = useState<FlightSpec | null>(null)
  const flightIdRef = useRef(0)

  // Offset between the server's clock and ours (serverNow - localNow at receipt).
  // The reveal countdown is driven by the server's absolute revealAt; applying
  // this offset keeps it accurate even when the client's clock is skewed.
  const clockOffsetRef = useRef(0)
  useEffect(() => {
    if (state) clockOffsetRef.current = state.serverNow - Date.now()
  }, [state])

  // Nudge: `shakingId` is whose card shakes for everyone; `selfShake` shakes my
  // whole screen when I'm the target. `muted` silences the buzz (persisted).
  const [shakingId, setShakingId] = useState<string | null>(null)
  const [selfShake, setSelfShake] = useState(false)
  const [muted, setMuted] = useState(readNudgeMuted)

  // Read inside the nudge effect without making it a dependency (so toggling
  // mute or a re-render can't replay the last nudge).
  const mutedRef = useRef(muted)
  const myIdRef = useRef(participantId)
  useEffect(() => {
    mutedRef.current = muted
  }, [muted])
  useEffect(() => {
    myIdRef.current = participantId
  }, [participantId])

  // Browsers block audio until a gesture; arm the context on the first one.
  useEffect(() => {
    const arm = () => unlockAudio()
    window.addEventListener('pointerdown', arm, { once: true })
    window.addEventListener('keydown', arm, { once: true })
    return () => {
      window.removeEventListener('pointerdown', arm)
      window.removeEventListener('keydown', arm)
    }
  }, [])

  useEffect(() => {
    if (!nudgeEvent) return
    const { targetId } = nudgeEvent
    setShakingId(targetId)
    const timers = [window.setTimeout(() => setShakingId(null), 600)]
    if (targetId === myIdRef.current) {
      setSelfShake(true)
      timers.push(window.setTimeout(() => setSelfShake(false), 600))
      if (!mutedRef.current) playNudge()
    }
    return () => timers.forEach((t) => window.clearTimeout(t))
  }, [nudgeEvent])

  const toggleMuted = (next: boolean) => {
    setMuted(next)
    writeNudgeMuted(next)
  }

  const handleNudge = useCallback(
    (targetId: string) => {
      unlockAudio() // this click also arms audio for the nudger
      nudge(targetId)
    },
    [nudge],
  )

  const handleVote = (card: Card, sourceEl: HTMLElement) => {
    const target = document.querySelector<HTMLElement>('[data-self-card]')
    if (target) {
      const from = sourceEl.getBoundingClientRect()
      const to = target.getBoundingClientRect()
      flightIdRef.current += 1
      setFlight({
        id: flightIdRef.current,
        card,
        from: { x: from.left, y: from.top, width: from.width, height: from.height },
        to: { x: to.left, y: to.top, width: to.width, height: to.height },
      })
    }
    vote(card)
  }

  const isJoined = status === 'joined'
  const showReconnectBanner = !isJoined && state !== null

  // Derived view state recomputed only when the room state actually changes,
  // not on every render (e.g. the flight animation's setState). Stable
  // identities also let memoized children skip reconciliation.
  const participants = state?.participants
  const voters = useMemo(
    () => participants?.filter((p) => !p.spectator) ?? [],
    [participants],
  )
  const spectators = useMemo(
    () => participants?.filter((p) => p.spectator) ?? [],
    [participants],
  )
  const votedCount = useMemo(
    () => voters.filter((p) => p.hasVoted).length,
    [voters],
  )
  const outliers = useMemo(
    () =>
      state?.revealed && participants
        ? computeOutliers(participants)
        : new Set<string>(),
    [state?.revealed, participants],
  )

  useEffect(() => {
    const prev = document.title
    if (state) {
      const prefix = `(${votedCount}/${voters.length}) `
      const storyPart = state.story ? `${state.story} — ` : ''
      document.title = `${prefix}${storyPart}Story Poker`
    }
    return () => {
      document.title = prev
    }
  }, [state, votedCount, voters.length])

  const centerContent = state
    ? state.revealed
      ? (
          <CenterSpread
            participants={state.participants}
            onRevote={revote}
            canAct={isJoined}
          />
        )
      : state.revealAt > 0
        ? (
            <CenterCountdown
              revealAt={state.revealAt}
              clockOffset={clockOffsetRef.current}
            />
          )
        : (
            <CenterIdle
              votedCount={votedCount}
              totalVoters={voters.length}
              onReveal={reveal}
              canAct={isJoined}
            />
          )
    : null

  return (
    <div
      className={`min-h-full flex flex-col p-4 sm:p-6 gap-4 sm:gap-6 max-w-4xl mx-auto w-full ${
        selfShake ? 'animate-nudge' : ''
      }`}
    >
      {flight && (
        <FlightCard
          key={flight.id}
          spec={flight}
          onComplete={() => setFlight(null)}
        />
      )}
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-xl font-medium flex items-center gap-2 flex-wrap text-ink">
          <span>Room</span>
          <code className="text-sage-strong bg-sage-soft px-2 py-1 rounded font-mono tracking-wide">
            {roomId}
          </code>
          <CopyLinkButton roomId={roomId} />
        </h1>
        <div className="flex items-center gap-3 text-sm">
          <span
            className={`flex items-center gap-1.5 ${
              isJoined ? 'text-sage-strong' : 'text-terracotta'
            }`}
            role="status"
            aria-live="polite"
          >
            <span
              aria-hidden="true"
              className={`w-2 h-2 rounded-full ${
                isJoined
                  ? 'bg-sage-strong'
                  : 'bg-terracotta animate-pulse'
              }`}
            />
            {statusLabel[status]}
          </span>
          <button
            onClick={onLeave}
            className="text-ink-muted hover:text-ink rounded focus-visible:outline-2 focus-visible:outline-sage"
          >
            Leave
          </button>
        </div>
      </header>

      {showReconnectBanner && (
        <div
          role="status"
          aria-live="polite"
          className="bg-surface-muted border border-divider text-ink-muted rounded p-3 text-sm"
        >
          Connection lost — reconnecting…
        </div>
      )}

      {error && state && (
        <div
          role="alert"
          aria-live="assertive"
          className="bg-terracotta-soft border border-terracotta/40 text-terracotta rounded p-3 text-sm"
        >
          {error}
        </div>
      )}

      {status === 'error' && !state ? (
        <div className="bg-terracotta-soft border border-terracotta/40 text-terracotta rounded p-4 space-y-3">
          <p className="font-medium">Couldn't join this room.</p>
          {error && <p className="text-sm">{error}</p>}
          <button
            type="button"
            onClick={onLeave}
            className="bg-surface hover:bg-surface-muted border border-divider text-ink rounded px-3 py-1.5 text-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage"
          >
            ← Back to start
          </button>
        </div>
      ) : state ? (
        <>
          <StoryInput
            value={state.story}
            disabled={!isJoined}
            onCommit={setStory}
          />

          {voters.length > 0 ? (
            <OvalTable
              voters={voters}
              selfId={participantId}
              revealed={state.revealed}
              outliers={outliers}
              centerContent={centerContent}
              shakingId={shakingId}
              onNudge={handleNudge}
            />
          ) : (
            <div className="text-ink-muted text-center py-8 bg-surface-muted border border-dashed border-divider rounded-lg">
              No voters yet — share the room link to invite teammates.
            </div>
          )}

          <SpectatorsStrip
            spectators={spectators}
            selfId={participantId}
            shakingId={shakingId}
            onNudge={handleNudge}
          />

          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex flex-col gap-2">
              <label className="flex items-center gap-2 text-sm text-ink-muted select-none">
                <input
                  type="checkbox"
                  checked={state.autoReveal}
                  onChange={(e) => setAutoReveal(e.target.checked)}
                  disabled={!isJoined}
                  className="accent-sage-strong"
                />
                Auto-reveal when everyone has voted
              </label>
              <label className="flex items-center gap-2 text-sm text-ink-muted select-none">
                <input
                  type="checkbox"
                  checked={me?.spectator ?? false}
                  onChange={(e) => setSpectator(e.target.checked)}
                  disabled={!isJoined || !me}
                  className="accent-sage-strong"
                />
                Spectate (don't vote)
              </label>
              <label className="flex items-center gap-2 text-sm text-ink-muted select-none">
                <input
                  type="checkbox"
                  checked={!muted}
                  onChange={(e) => toggleMuted(!e.target.checked)}
                  className="accent-sage-strong"
                />
                Nudge sound
              </label>
            </div>
            <button
              type="button"
              onClick={reset}
              disabled={!isJoined}
              className="bg-surface hover:bg-surface-muted border border-divider text-ink disabled:opacity-50 rounded px-3 py-1.5 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage"
              title="Save to history and start a new story"
            >
              Reset round
            </button>
          </div>

          {me?.spectator ? (
            <div className="flex flex-col items-center gap-2 text-sm text-ink-muted bg-surface-muted border border-divider rounded-lg px-4 py-3 text-center">
              <span>You're a spectator — you can watch but not vote.</span>
              <button
                type="button"
                onClick={() => setSpectator(false)}
                disabled={!isJoined}
                className="bg-sage-strong hover:opacity-90 disabled:opacity-50 text-white rounded px-3 py-1.5 text-sm font-medium transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage"
              >
                Start voting
              </button>
            </div>
          ) : (
            <Deck
              deck={state.deck}
              myVote={myVote}
              disabled={!isJoined || state.revealed || state.revealAt > 0}
              onPick={handleVote}
            />
          )}

          {state.history.length > 0 && <History entries={state.history} />}
        </>
      ) : (
        <div className="text-ink-muted">Joining…</div>
      )}
    </div>
  )
}

function StoryInput({
  value,
  disabled,
  onCommit,
}: {
  value: string
  disabled: boolean
  onCommit: (s: string) => void
}) {
  const [local, setLocal] = useState(value)
  const focusedRef = useRef(false)

  useEffect(() => {
    if (!focusedRef.current) setLocal(value)
  }, [value])

  const commit = () => {
    if (local !== value) onCommit(local)
  }

  return (
    <label className="block">
      <span className="text-xs uppercase tracking-wide text-ink-muted">
        Voting on
      </span>
      <input
        type="text"
        value={local}
        disabled={disabled}
        maxLength={200}
        placeholder="e.g. STORY-123 — Add user avatars"
        onChange={(e) => setLocal(e.target.value)}
        onFocus={() => {
          focusedRef.current = true
        }}
        onBlur={() => {
          focusedRef.current = false
          commit()
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            ;(e.target as HTMLInputElement).blur()
          }
        }}
        className="mt-1 w-full bg-surface border border-divider text-ink placeholder:text-ink-soft focus:border-sage focus:ring-2 focus:ring-sage/30 rounded px-3 py-2 text-lg outline-none transition-colors disabled:opacity-60"
      />
    </label>
  )
}

function CenterCountdown({
  revealAt,
  clockOffset,
}: {
  revealAt: number
  clockOffset: number
}) {
  const remaining = useCountdown(revealAt, clockOffset)
  const display = remaining > 0 ? String(remaining) : 'Reveal!'
  return (
    <div className="text-center flex flex-col items-center gap-1">
      <motion.div
        key={display}
        initial={{ scale: 0.4, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        exit={{ scale: 1.4, opacity: 0 }}
        transition={{ type: 'spring', stiffness: 350, damping: 18 }}
        className="text-5xl md:text-6xl font-bold text-sage-strong tabular-nums drop-shadow-[0_2px_6px_rgba(74,107,82,0.3)]"
      >
        {display}
      </motion.div>
      <div className="text-xs text-ink-muted uppercase tracking-wider">
        Revealing
      </div>
    </div>
  )
}

function useCountdown(revealAt: number, clockOffset = 0): number {
  // revealAt is on the server clock; serverTime ≈ Date.now() + clockOffset.
  const [remaining, setRemaining] = useState(() =>
    Math.max(0, Math.ceil((revealAt - (Date.now() + clockOffset)) / 1000)),
  )
  useEffect(() => {
    const tick = () => {
      const ms = Math.max(0, revealAt - (Date.now() + clockOffset))
      setRemaining(Math.ceil(ms / 1000))
    }
    tick()
    const id = window.setInterval(tick, 100)
    return () => window.clearInterval(id)
  }, [revealAt, clockOffset])
  return remaining
}

function CenterIdle({
  votedCount,
  totalVoters,
  onReveal,
  canAct,
}: {
  votedCount: number
  totalVoters: number
  onReveal: () => void
  canAct: boolean
}) {
  const allIn = totalVoters > 0 && votedCount === totalVoters
  const noneIn = votedCount === 0
  const partial = !noneIn && !allIn

  const label =
    totalVoters === 0
      ? ''
      : noneIn
        ? 'Ready when you are'
        : partial
          ? `${votedCount} of ${totalVoters} in…`
          : 'All in — ready to reveal'

  return (
    <div className="text-center flex flex-col items-center gap-2 max-w-[16rem]">
      {label && (
        <div
          className={`text-sm tabular-nums ${
            allIn
              ? 'text-sage-strong font-medium'
              : noneIn
                ? 'text-ink-muted italic'
                : 'text-ink-muted'
          }`}
        >
          {label}
        </div>
      )}
      <button
        type="button"
        onClick={onReveal}
        disabled={!canAct || votedCount === 0}
        className="bg-sage-strong hover:opacity-90 disabled:bg-divider disabled:text-ink-soft text-white rounded px-5 py-2 font-semibold transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage shadow-md shadow-sage-strong/30"
      >
        Reveal
      </button>
    </div>
  )
}

function CenterSpread({
  participants,
  onRevote,
  canAct,
}: {
  participants: ParticipantPublic[]
  onRevote: () => void
  canAct: boolean
}) {
  const votes = participants
    .map((p) => p.vote)
    .filter((v): v is string => v != null)
  if (votes.length === 0) return null

  const counts = new Map<string, number>()
  for (const v of votes) counts.set(v, (counts.get(v) ?? 0) + 1)

  const sorted = Array.from(counts.entries()).sort(([a], [b]) => {
    const na = Number(a)
    const nb = Number(b)
    const aNum = !Number.isNaN(na)
    const bNum = !Number.isNaN(nb)
    if (aNum && bNum) return na - nb
    if (aNum) return -1
    if (bNum) return 1
    return a.localeCompare(b)
  })

  const nums = votes.map(Number).filter((n) => !Number.isNaN(n))
  const consensus = counts.size === 1
  const minN = nums.length ? Math.min(...nums) : null
  const maxN = nums.length ? Math.max(...nums) : null
  const hasSpread = minN !== null && maxN !== null && minN !== maxN
  const mean = nums.length
    ? (nums.reduce((a, b) => a + b, 0) / nums.length).toFixed(1)
    : null

  return (
    <div className="text-center flex flex-col items-center gap-2 max-w-[22rem]">
      <div className="flex flex-wrap gap-1.5 justify-center">
        {sorted.map(([v, c]) => {
          const n = Number(v)
          const isOutlier =
            hasSpread && !Number.isNaN(n) && (n === minN || n === maxN)
          return (
            <span
              key={v}
              className={`text-xs px-2 py-1 rounded border ${
                isOutlier
                  ? 'bg-terracotta-soft border-terracotta/40'
                  : 'bg-surface border-divider'
              }`}
            >
              <span className="font-bold text-sage-strong tabular-nums">
                {v}
              </span>
              <span className="text-ink-muted ml-1">×{c}</span>
            </span>
          )
        })}
      </div>
      <div className="text-xs text-ink-muted">
        {consensus ? (
          <span className="text-sage-strong font-medium">Consensus</span>
        ) : nums.length >= 2 ? (
          <>
            <span className="text-ink">
              {Math.min(...nums)}–{Math.max(...nums)}
            </span>{' '}
            <span className="text-ink-soft">·</span>{' '}
            <span>mean </span>
            <span className="text-ink">{mean}</span>
          </>
        ) : null}
      </div>
      <button
        type="button"
        onClick={onRevote}
        disabled={!canAct}
        className="bg-sage-strong hover:opacity-90 disabled:bg-divider disabled:text-ink-soft text-white rounded px-4 py-1.5 text-sm font-medium transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage shadow-md shadow-sage-strong/30"
      >
        Re-vote
      </button>
    </div>
  )
}

function CopyLinkButton({ roomId }: { roomId: string }) {
  const [copied, setCopied] = useState(false)

  const handleClick = async () => {
    const url = new URL(window.location.href)
    url.searchParams.set('room', roomId)
    try {
      await navigator.clipboard.writeText(url.toString())
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard API unavailable (e.g. http:// in some browsers); silently no-op.
    }
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      aria-label="Copy room link to clipboard"
      className="text-xs px-2 py-1 rounded border border-divider text-ink-muted hover:bg-surface-muted hover:text-ink transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage"
    >
      {copied ? 'Copied!' : 'Copy link'}
    </button>
  )
}

function Deck({
  deck,
  myVote,
  disabled,
  onPick,
}: {
  deck: Card[]
  myVote: Card | null
  disabled: boolean
  onPick: (c: Card, source: HTMLElement) => void
}) {
  const hasPicked = myVote !== null
  return (
    <div className="flex flex-wrap gap-3 justify-center">
      {deck.map((card) => {
        const picked = card === myVote
        const dimmed = hasPicked && !picked
        return (
          <motion.button
            key={card}
            type="button"
            disabled={disabled}
            onClick={(e) => onPick(card, e.currentTarget)}
            aria-label={`Vote ${card}${picked ? ' (selected)' : ''}`}
            aria-pressed={picked}
            animate={{
              y: picked ? -14 : 0,
              opacity: dimmed ? 0.45 : 1,
            }}
            whileHover={
              !disabled
                ? { y: picked ? -16 : -6, opacity: 1 }
                : undefined
            }
            whileTap={
              !disabled
                ? { scale: 0.94, y: picked ? -10 : -3 }
                : undefined
            }
            transition={{ type: 'spring', stiffness: 420, damping: 18 }}
            className={`w-14 h-20 lg:w-16 lg:h-24 rounded-lg text-xl lg:text-2xl font-bold border-2 transition-colors ${
              picked
                ? 'bg-sage-strong border-sage shadow-xl shadow-sage-strong/50 ring-4 ring-sage-strong/20 text-white'
                : 'bg-surface border-divider hover:border-sage/60 hover:bg-surface-muted text-ink'
            } disabled:opacity-40 disabled:cursor-not-allowed focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage`}
          >
            {card}
          </motion.button>
        )
      })}
    </div>
  )
}

function History({ entries }: { entries: HistoryEntry[] }) {
  const reversed = [...entries].reverse()
  return (
    <details className="bg-surface border border-divider rounded-lg overflow-hidden">
      <summary className="cursor-pointer px-4 py-2 text-sm text-ink-muted hover:bg-surface-muted hover:text-ink select-none transition-colors">
        Session history ({entries.length})
      </summary>
      <ul className="divide-y divide-divider">
        {reversed.map((e, i) => (
          <li key={`${e.at}-${i}`} className="px-4 py-3">
            <div className="flex items-start justify-between gap-3">
              <div className="text-sm text-ink truncate">
                {e.story || (
                  <span className="text-ink-soft italic">no title</span>
                )}
              </div>
              <div className="flex flex-wrap gap-1.5 flex-shrink-0">
                {summarizeVotes(e.votes).map(([v, c]) => (
                  <span
                    key={v}
                    className="text-xs bg-surface-muted border border-divider rounded px-2 py-0.5 tabular-nums"
                  >
                    <span className="text-sage-strong font-bold">{v}</span>
                    <span className="text-ink-soft"> ×{c}</span>
                  </span>
                ))}
              </div>
            </div>
          </li>
        ))}
      </ul>
    </details>
  )
}

function summarizeVotes(
  votes: { vote: string }[],
): Array<[string, number]> {
  const counts = new Map<string, number>()
  for (const { vote } of votes) {
    counts.set(vote, (counts.get(vote) ?? 0) + 1)
  }
  return Array.from(counts.entries()).sort(([a], [b]) => {
    const na = Number(a)
    const nb = Number(b)
    const aN = !Number.isNaN(na)
    const bN = !Number.isNaN(nb)
    if (aN && bN) return na - nb
    if (aN) return -1
    if (bN) return 1
    return a.localeCompare(b)
  })
}
