import { motion } from 'motion/react'

export interface FlightSpec {
  /** Unique id so React can re-mount on each flight even with same card value. */
  id: number
  card: string
  from: { x: number; y: number; width: number; height: number }
  to: { x: number; y: number; width: number; height: number }
}

interface Props {
  spec: FlightSpec
  onComplete: () => void
}

/**
 * A ghost card that flies from the clicked deck button to the self table card.
 * `position: fixed` + transform translate; layout-property-free for compositor.
 * Reduced-motion: MotionConfig collapses transition to 0 and fires onComplete
 * immediately — the ghost briefly flashes at the destination then unmounts.
 */
export function FlightCard({ spec, onComplete }: Props) {
  const dx = spec.to.x + spec.to.width / 2 - (spec.from.x + spec.from.width / 2)
  const dy =
    spec.to.y + spec.to.height / 2 - (spec.from.y + spec.from.height / 2)
  const scale = spec.to.width / spec.from.width

  return (
    <motion.div
      className="fixed pointer-events-none rounded-lg bg-sage-strong border-2 border-sage flex items-center justify-center text-white font-bold text-xl md:text-2xl tabular-nums shadow-lg shadow-sage-strong/50"
      style={{
        left: spec.from.x,
        top: spec.from.y,
        width: spec.from.width,
        height: spec.from.height,
        zIndex: 40,
      }}
      initial={{ x: 0, y: 0, scale: 1, opacity: 1 }}
      animate={{ x: dx, y: dy, scale, opacity: 0 }}
      transition={{ duration: 0.4, ease: [0.4, 0, 0.2, 1] }}
      onAnimationComplete={onComplete}
      aria-hidden="true"
    >
      {spec.card}
    </motion.div>
  )
}
