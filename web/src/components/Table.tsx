import { type ReactNode } from 'react'
import type { ParticipantPublic } from '../protocol'

interface OvalTableProps {
  voters: ParticipantPublic[]
  selfId: string | null
  revealed: boolean
  outliers: Set<string>
  centerContent: ReactNode
}

export function OvalTable({
  voters,
  selfId,
  revealed,
  outliers,
  centerContent,
}: OvalTableProps) {
  const ordered = [...voters].sort((a, b) => {
    if (a.id === selfId) return -1
    if (b.id === selfId) return 1
    return a.id.localeCompare(b.id)
  })

  return (
    <div className="relative w-full max-w-3xl mx-auto aspect-[4/3] md:aspect-[5/3] my-4">
      {/* Rim — driftwood band, drops a soft shadow onto the canvas */}
      <div className="absolute inset-0 bg-driftwood rounded-[50%] shadow-lg shadow-ink/10" />

      {/* Felt — inset from rim, radial gradient for depth (lighter center, deeper rim) */}
      <div
        className="absolute inset-[10px] md:inset-[14px] rounded-[50%] shadow-inner shadow-sage-strong/20"
        style={{
          background:
            'radial-gradient(ellipse at 50% 50%, var(--color-sage-soft) 0%, var(--color-sage-medium) 100%)',
        }}
      />

      <div className="absolute inset-0 flex items-center justify-center px-6 pointer-events-none">
        <div className="pointer-events-auto">{centerContent}</div>
      </div>

      {ordered.map((p, i) => {
        // index 0 is self at the bottom of the oval; the rest fan
        // counter-clockwise around the rim
        const angle =
          (i / ordered.length) * 2 * Math.PI + Math.PI / 2
        const x = 50 + 44 * Math.cos(angle)
        const y = 50 + 44 * Math.sin(angle)
        return (
          <div
            key={p.id}
            className="absolute"
            style={{
              left: `${x}%`,
              top: `${y}%`,
              transform: 'translate(-50%, -50%)',
            }}
          >
            <ParticipantCard
              p={p}
              isSelf={p.id === selfId}
              revealed={revealed}
              isOutlier={outliers.has(p.id)}
            />
          </div>
        )
      })}
    </div>
  )
}

interface CardProps {
  p: ParticipantPublic
  isSelf: boolean
  revealed: boolean
  isOutlier: boolean
}

export function ParticipantCard({ p, isSelf, revealed, isOutlier }: CardProps) {
  const showsValue = (isSelf || revealed) && p.vote != null
  const isFaceDown = !isSelf && !revealed && p.hasVoted

  const cardClass = showsValue
    ? `w-12 h-16 md:w-14 md:h-20 rounded-md bg-surface border-2 flex items-center justify-center text-ink font-bold text-xl md:text-2xl tabular-nums shadow-md shadow-ink/20 transition-all ${
        isOutlier
          ? 'border-terracotta ring-2 ring-terracotta/40'
          : isSelf
            ? 'border-sage ring-2 ring-sage/40'
            : 'border-divider-strong'
      }`
    : isFaceDown
      ? 'w-12 h-16 md:w-14 md:h-20 rounded-md bg-gradient-to-br from-sage to-sage-strong border-2 border-sage-strong shadow-md shadow-ink/25 transition-all'
      : 'w-12 h-16 md:w-14 md:h-20 rounded-md border-2 border-dashed border-divider-strong bg-surface/40 shadow-sm shadow-ink/10 transition-all'

  const ariaLabel = showsValue
    ? `${p.name}: ${p.vote}`
    : isFaceDown
      ? `${p.name}: voted, hidden until reveal`
      : `${p.name}: not yet voted`

  return (
    <div
      className={`flex flex-col items-center gap-1.5 ${
        !p.connected ? 'opacity-50' : ''
      }`}
    >
      <div className={cardClass} aria-label={ariaLabel}>
        {showsValue && p.vote}
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

export function SpectatorsStrip({
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
}
