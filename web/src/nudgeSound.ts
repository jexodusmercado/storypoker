// A short, MSN-style "buzz" synthesized with the Web Audio API — no asset to
// bundle, works offline. Browsers gate audio until the user has interacted with
// the page, so call unlockAudio() from a real gesture (click/keydown) once to
// arm the context; playNudge() then works when a nudge arrives without the
// recipient having clicked anything in that moment.

type WebkitWindow = typeof window & {
  webkitAudioContext?: typeof AudioContext
}

let ctx: AudioContext | null = null

function getCtx(): AudioContext | null {
  if (ctx) return ctx
  const Ctor =
    window.AudioContext ?? (window as WebkitWindow).webkitAudioContext
  if (!Ctor) return null
  try {
    ctx = new Ctor()
  } catch {
    return null
  }
  return ctx
}

/** Resume the audio context from a user gesture so later playback isn't blocked. */
export function unlockAudio() {
  const c = getCtx()
  if (c && c.state === 'suspended') void c.resume()
}

/** Play a two-pulse buzz. No-op if audio is unavailable or still locked. */
export function playNudge() {
  const c = getCtx()
  if (!c) return
  if (c.state === 'suspended') void c.resume()

  const start = c.currentTime
  // Two short low-frequency pulses, ~the cadence of a phone buzz.
  for (let i = 0; i < 2; i++) {
    const t0 = start + i * 0.18
    const osc = c.createOscillator()
    const gain = c.createGain()
    osc.type = 'square'
    osc.frequency.setValueAtTime(150, t0)
    osc.frequency.linearRampToValueAtTime(110, t0 + 0.12)
    // Quick attack, short decay — keeps it a buzz, not a beep.
    gain.gain.setValueAtTime(0.0001, t0)
    gain.gain.exponentialRampToValueAtTime(0.18, t0 + 0.01)
    gain.gain.exponentialRampToValueAtTime(0.0001, t0 + 0.14)
    osc.connect(gain)
    gain.connect(c.destination)
    osc.start(t0)
    osc.stop(t0 + 0.15)
  }
}
