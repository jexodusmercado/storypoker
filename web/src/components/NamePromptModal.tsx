import { useState, type FormEvent } from 'react'

interface Props {
  roomId: string
  onSubmit: (name: string, spectator: boolean) => void
  onCancel: () => void
}

export function NamePromptModal({ roomId, onSubmit, onCancel }: Props) {
  const [name, setName] = useState('')
  const [spectator, setSpectator] = useState(false)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const n = name.trim()
    if (!n) return
    onSubmit(n, spectator)
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="name-modal-title"
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation()
          onCancel()
        }
      }}
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-ink/30 backdrop-blur-[1px]"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-sm space-y-5 bg-surface border border-divider p-6 rounded-xl shadow-lg shadow-ink/10"
      >
        <div>
          <h1
            id="name-modal-title"
            className="text-lg font-semibold text-ink"
          >
            Join room
          </h1>
          <p className="text-sm text-ink-muted mt-1">
            <code className="text-sage-strong bg-sage-soft px-2 py-0.5 rounded font-mono tracking-wide">
              {roomId}
            </code>
          </p>
        </div>

        <label className="block">
          <span className="text-sm text-ink-muted">Your name</span>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            className="mt-1 w-full bg-canvas border border-divider rounded px-3 py-2 outline-none focus:ring-2 focus:ring-sage focus:border-sage transition-colors"
          />
        </label>

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
          disabled={!name.trim()}
          className="w-full bg-sage-strong hover:opacity-90 disabled:bg-divider disabled:text-ink-soft text-white rounded py-2 font-medium transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sage"
        >
          Join room
        </button>

        <p className="text-xs text-ink-soft text-center">
          <button
            type="button"
            onClick={onCancel}
            className="text-sage-strong hover:text-sage underline-offset-2 hover:underline"
          >
            ← Create a new room instead
          </button>
        </p>
      </form>
    </div>
  )
}
