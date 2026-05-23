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
