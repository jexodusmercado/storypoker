import { useEffect, useRef } from 'react'

// Drives the CSS "nudge" shake imperatively so repeated nudges (spam) restart
// the animation instead of being swallowed because the class is already applied
// — re-adding an in-flight animation class is a no-op, so we remove it, force a
// reflow, then re-add. Pass the latest nudge sequence number targeting this
// element (0 = not currently a target). Attach the returned ref to a wrapper
// whose className you DON'T otherwise change, so React can't clobber the
// imperatively-toggled class mid-animation.
export function useNudgeShake<T extends HTMLElement>(seq: number) {
  const ref = useRef<T>(null)
  useEffect(() => {
    if (!seq) return
    const el = ref.current
    if (!el) return
    el.classList.remove('animate-nudge')
    void el.offsetWidth // force reflow so the animation restarts from frame 0
    el.classList.add('animate-nudge')
    const clear = () => el.classList.remove('animate-nudge')
    el.addEventListener('animationend', clear, { once: true })
    return () => el.removeEventListener('animationend', clear)
  }, [seq])
  return ref
}
