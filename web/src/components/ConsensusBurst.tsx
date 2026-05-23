import { useState } from 'react'
import { motion } from 'motion/react'

interface Particle {
  i: number
  x: number
  y: number
  size: number
  delay: number
  duration: number
}

function generateParticles(count = 16): Particle[] {
  return Array.from({ length: count }, (_, i) => {
    const angle = (i / count) * Math.PI * 2 + Math.random() * 0.3
    const distance = 90 + Math.random() * 70
    return {
      i,
      x: Math.cos(angle) * distance,
      y: Math.sin(angle) * distance,
      size: 5 + Math.random() * 5,
      delay: Math.random() * 0.12,
      duration: 0.7 + Math.random() * 0.35,
    }
  })
}

/**
 * Sage particles + "Consensus" text emanating from center.
 * Mount with a `key` prop that changes per consensus event so React remounts
 * and the burst plays fresh each time.
 */
export function ConsensusBurst() {
  const [particles] = useState<Particle[]>(generateParticles)

  return (
    <div
      className="absolute inset-0 pointer-events-none flex items-center justify-center"
      aria-hidden="true"
    >
      {particles.map((p) => (
        <motion.div
          key={p.i}
          className="absolute rounded-full bg-sage-strong"
          style={{ width: p.size, height: p.size }}
          initial={{ opacity: 0, x: 0, y: 0, scale: 0 }}
          animate={{
            opacity: [0, 1, 0],
            x: p.x,
            y: p.y,
            scale: [0, 1, 0.6],
          }}
          transition={{
            duration: p.duration,
            delay: p.delay,
            ease: 'easeOut',
            times: [0, 0.35, 1],
          }}
        />
      ))}
      <motion.div
        className="text-sage-strong text-2xl md:text-3xl font-semibold tracking-wide drop-shadow-[0_2px_8px_rgba(74,107,82,0.35)]"
        initial={{ opacity: 0, scale: 0.7, y: 10 }}
        animate={{
          opacity: [0, 1, 1, 0],
          scale: [0.7, 1.08, 1.04, 1],
          y: [10, 0, 0, -8],
        }}
        transition={{
          duration: 1.4,
          times: [0, 0.18, 0.78, 1],
          ease: 'easeOut',
        }}
      >
        Consensus
      </motion.div>
    </div>
  )
}
