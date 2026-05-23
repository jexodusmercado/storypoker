import type { ParticipantPublic } from './protocol'

export function computeOutliers(list: ParticipantPublic[]): Set<string> {
  const numericVotes: Array<{ id: string; n: number }> = []
  for (const p of list) {
    if (p.spectator || p.vote == null) continue
    const n = Number(p.vote)
    if (!Number.isNaN(n)) numericVotes.push({ id: p.id, n })
  }
  if (numericVotes.length < 2) return new Set()
  let min = numericVotes[0].n
  let max = numericVotes[0].n
  for (const { n } of numericVotes) {
    if (n < min) min = n
    if (n > max) max = n
  }
  if (min === max) return new Set()
  const out = new Set<string>()
  for (const { id, n } of numericVotes) {
    if (n === min || n === max) out.add(id)
  }
  return out
}
