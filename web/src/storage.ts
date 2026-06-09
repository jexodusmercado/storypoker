const NAME_STORAGE_KEY = 'storypoker.name'

export function readStoredName(): string {
  try {
    return window.localStorage.getItem(NAME_STORAGE_KEY) ?? ''
  } catch {
    return ''
  }
}

export function writeStoredName(name: string) {
  try {
    window.localStorage.setItem(NAME_STORAGE_KEY, name)
  } catch {
    /* localStorage unavailable */
  }
}

const NUDGE_MUTED_KEY = 'storypoker.nudgeMuted'

export function readNudgeMuted(): boolean {
  try {
    return window.localStorage.getItem(NUDGE_MUTED_KEY) === '1'
  } catch {
    return false
  }
}

export function writeNudgeMuted(muted: boolean) {
  try {
    if (muted) window.localStorage.setItem(NUDGE_MUTED_KEY, '1')
    else window.localStorage.removeItem(NUDGE_MUTED_KEY)
  } catch {
    /* localStorage unavailable */
  }
}
