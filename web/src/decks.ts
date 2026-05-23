export const DECK_PRESETS = {
  fibonacci: ['0', '1', '2', '3', '5', '8', '13', '21', '?', '☕'],
  modified: ['0', '½', '1', '2', '3', '5', '8', '13', '20', '40', '100', '?', '☕'],
  tshirt: ['XS', 'S', 'M', 'L', 'XL', 'XXL', '?', '☕'],
  powers: ['1', '2', '4', '8', '16', '32', '?', '☕'],
} as const

export type DeckPreset = keyof typeof DECK_PRESETS | 'custom'

export const DECK_LABELS: Record<Exclude<DeckPreset, 'custom'>, string> = {
  fibonacci: 'Fibonacci',
  modified: 'Modified Fibonacci',
  tshirt: 'T-shirt',
  powers: 'Powers of 2',
}

export function parseCustomDeck(input: string): string[] | null {
  const cards = input
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  if (cards.length === 0 || cards.length > 32) return null
  if (cards.some((c) => c.length > 16)) return null
  return Array.from(new Set(cards))
}
