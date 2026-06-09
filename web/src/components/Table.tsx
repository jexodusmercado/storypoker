import { memo, useMemo, type ReactNode } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import type { ParticipantPublic } from '../protocol'
import { ConsensusBurst } from './ConsensusBurst'

interface OvalTableProps {
  voters: ParticipantPublic[]
  selfId: string | null
  revealed: boolean
  outliers: Set<string>
  centerContent: ReactNode
}

function detectConsensusKey(
  voters: ParticipantPublic[],
  revealed: boolean,
): string | null {
  if (!revealed) return null
  const votes = voters.map((v) => v.vote).filter((v): v is string => v != null)
  if (votes.length < 2) return null
  const unique = new Set(votes)
  if (unique.size !== 1) return null
  return `${votes.length}:${votes[0]}`
}

export function OvalTable({
  voters,
  selfId,
  revealed,
  outliers,
  centerContent,
}: OvalTableProps) {
  const consensusKey = useMemo(
    () => detectConsensusKey(voters, revealed),
    [voters, revealed],
  )
  const ordered = useMemo(
    () =>
      [...voters].sort((a, b) => {
        if (a.id === selfId) return -1
        if (b.id === selfId) return 1
        return a.id.localeCompare(b.id)
      }),
    [voters, selfId],
  )

  return (
    <div className="relative w-full max-w-3xl mx-auto aspect-[4/3] md:aspect-[5/3] mt-4 mb-8 md:my-4">
      {/* Rim — driftwood band, drops a soft shadow onto the canvas */}
      <div className="absolute inset-0 bg-driftwood rounded-[50%] shadow-lg shadow-ink/10" />

      {/* Felt — inset from rim, radial gradient for depth (lighter center, deeper rim).
          Subtle brightness breathing on ~6s cycle gives the felt a "living" feel.
          Reduced-motion: MotionConfig collapses this to no motion. */}
      <motion.div
        className="absolute inset-[10px] md:inset-[14px] rounded-[50%] shadow-inner shadow-sage-strong/20"
        style={{
          background:
            'radial-gradient(ellipse at 50% 50%, var(--color-sage-soft) 0%, var(--color-sage-medium) 100%)',
        }}
        animate={{
          filter: ['brightness(0.985)', 'brightness(1.02)', 'brightness(0.985)'],
        }}
        transition={{
          duration: 6,
          repeat: Infinity,
          ease: 'easeInOut',
        }}
      />

      <div className="absolute inset-0 flex items-center justify-center px-6 pointer-events-none">
        <div className="pointer-events-auto">{centerContent}</div>
      </div>

      {consensusKey && <ConsensusBurst key={consensusKey} />}

      <AnimatePresence>
        {ordered.map((p, i) => {
          // index 0 is self at the bottom of the oval; the rest fan
          // counter-clockwise around the rim
          const angle = (i / ordered.length) * 2 * Math.PI + Math.PI / 2
          // Radius pulled in (was 44) so cards sit on the felt with clearance
          // from the rim; especially important on mobile where the container
          // is shorter and the card column overflows tight aspect ratios.
          const x = 50 + 36 * Math.cos(angle)
          const y = 50 + 36 * Math.sin(angle)
          // Centering via motion's `x`/`y` style props (percentages of self
          // size) so motion combines the translate with its animated scale
          // into one transform string. A plain `transform: translate(-50%,
          // -50%)` in style would get clobbered the moment motion animates
          // scale, pinning the card by its top-left corner instead.
          return (
            <motion.div
              key={p.id}
              className="absolute"
              style={{
                left: `${x}%`,
                top: `${y}%`,
                x: '-50%',
                y: '-50%',
              }}
              initial={{ opacity: 0, scale: 0.5 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.5 }}
              transition={{
                delay: i * 0.05,
                duration: 0.35,
                ease: [0.34, 1.36, 0.64, 1],
              }}
            >
              <ParticipantCard
                p={p}
                isSelf={p.id === selfId}
                revealed={revealed}
                isOutlier={outliers.has(p.id)}
                flipDelay={i * 0.08}
              />
            </motion.div>
          )
        })}
      </AnimatePresence>
    </div>
  )
}

interface CardProps {
  p: ParticipantPublic
  isSelf: boolean
  revealed: boolean
  isOutlier: boolean
  flipDelay: number
}

function ParticipantCardImpl({
  p,
  isSelf,
  revealed,
  isOutlier,
  flipDelay,
}: CardProps) {
  // Self always sees their own face. Others see card-back until reveal.
  const selfFaceUp = isSelf && p.vote != null
  const hasFlippableCard = !isSelf && p.hasVoted

  return (
    <div
      className={`flex flex-col items-center gap-1.5 ${
        !p.connected ? 'opacity-50' : ''
      }`}
    >
      <div data-self-card={isSelf ? 'true' : undefined}>
        {selfFaceUp ? (
          <SelfFaceUpCard vote={p.vote!} isOutlier={isOutlier} />
        ) : hasFlippableCard ? (
          <FlippableCard
            vote={p.vote}
            revealed={revealed}
            isOutlier={isOutlier}
            flipDelay={flipDelay}
          />
        ) : (
          <EmptyCard />
        )}
      </div>
      <div className="flex items-center gap-1 max-w-[6rem]">
        <span
          aria-hidden="true"
          className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
            p.connected ? 'bg-sage-strong' : 'bg-terracotta'
          }`}
        />
        {!p.connected && <span className="sr-only">reconnecting</span>}
        <span className="text-xs text-ink truncate">{p.name}</span>
        {isSelf && <span className="text-[10px] text-ink-soft">(you)</span>}
      </div>
    </div>
  )
}

// Each room broadcast parses fresh participant objects, so a plain memo
// (reference-equal props) would never hit. Compare the fields the card actually
// renders: when one person votes, the other cards are field-equal and skip
// re-rendering (and re-running their motion subtrees).
export const ParticipantCard = memo(
  ParticipantCardImpl,
  (a, b) =>
    a.p.id === b.p.id &&
    a.p.name === b.p.name &&
    a.p.hasVoted === b.p.hasVoted &&
    a.p.connected === b.p.connected &&
    a.p.vote === b.p.vote &&
    a.isSelf === b.isSelf &&
    a.revealed === b.revealed &&
    a.isOutlier === b.isOutlier &&
    a.flipDelay === b.flipDelay,
)

const CARD_SHAPE = 'w-12 h-16 md:w-14 md:h-20 rounded-md'

function SelfFaceUpCard({
  vote,
  isOutlier,
}: {
  vote: string
  isOutlier: boolean
}) {
  return (
    <motion.div
      key={`self-vote-${vote}`}
      initial={{ scale: 0.85, boxShadow: '0 0 0 0px rgba(110, 142, 118, 0)' }}
      animate={{
        scale: 1,
        boxShadow: [
          '0 0 0 0px rgba(110, 142, 118, 0.5)',
          '0 0 0 12px rgba(110, 142, 118, 0)',
        ],
      }}
      transition={{
        scale: { type: 'spring', stiffness: 400, damping: 16 },
        boxShadow: { duration: 0.6, ease: 'easeOut' },
      }}
      className={`${CARD_SHAPE} bg-surface border-2 flex items-center justify-center text-ink font-bold text-xl md:text-2xl tabular-nums shadow-md shadow-ink/20 ${
        isOutlier
          ? 'border-terracotta ring-2 ring-terracotta/40'
          : 'border-sage ring-2 ring-sage/40'
      }`}
    >
      {vote}
    </motion.div>
  )
}

function FlippableCard({
  vote,
  revealed,
  isOutlier,
  flipDelay,
}: {
  vote: string | null
  revealed: boolean
  isOutlier: boolean
  flipDelay: number
}) {
  return (
    <div className={`${CARD_SHAPE} relative`} style={{ perspective: 800 }}>
      <motion.div
        className="absolute inset-0"
        style={{ transformStyle: 'preserve-3d' }}
        animate={{ rotateY: revealed ? 180 : 0 }}
        transition={{
          duration: 0.65,
          delay: revealed ? flipDelay : 0,
          ease: [0.45, 0, 0.55, 1],
        }}
      >
        {/* Front face (face-down, sage gradient) */}
        <div
          className={`absolute inset-0 ${CARD_SHAPE} bg-gradient-to-br from-sage to-sage-strong border-2 border-sage-strong shadow-md shadow-ink/25`}
          style={{ backfaceVisibility: 'hidden' }}
        />
        {/* Back face (face-up, value) */}
        <div
          className={`absolute inset-0 ${CARD_SHAPE} bg-surface border-2 flex items-center justify-center text-ink font-bold text-xl md:text-2xl tabular-nums shadow-md shadow-ink/20 ${
            isOutlier
              ? 'border-terracotta ring-2 ring-terracotta/40'
              : 'border-divider-strong'
          }`}
          style={{
            backfaceVisibility: 'hidden',
            transform: 'rotateY(180deg)',
          }}
        >
          {vote ?? '—'}
        </div>
      </motion.div>
    </div>
  )
}

function EmptyCard() {
  return (
    <div
      className={`${CARD_SHAPE} border-2 border-dashed border-divider-strong bg-surface/40 shadow-sm shadow-ink/10`}
      aria-label="not yet voted"
    />
  )
}

export const SpectatorsStrip = memo(function SpectatorsStrip({
  spectators,
}: {
  spectators: ParticipantPublic[]
}) {
  if (spectators.length === 0) return null
  return (
    <aside className="flex flex-wrap items-center justify-center gap-2 text-xs text-ink-muted">
      <span className="uppercase tracking-wide">Viewers:</span>
      {spectators.map((p) => (
        <span
          key={p.id}
          className={`bg-surface border border-divider rounded px-2 py-1 text-ink ${
            !p.connected ? 'opacity-50' : ''
          }`}
        >
          {p.name}
        </span>
      ))}
    </aside>
  )
})
