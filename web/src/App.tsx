import { useEffect, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { EntryScreen } from './components/EntryScreen'
import { NamePromptModal } from './components/NamePromptModal'
import { RoomScreen } from './components/RoomScreen'
import { readStoredName, writeStoredName } from './storage'

interface Session {
  roomId: string
  name: string
  deck: string[]
  spectator: boolean
  create: boolean
}

function readRoomFromUrl(): string {
  if (typeof window === 'undefined') return ''
  const url = new URL(window.location.href)
  return url.searchParams.get('room')?.trim() ?? ''
}

function syncRoomToUrl(roomId: string | null) {
  if (typeof window === 'undefined') return
  const url = new URL(window.location.href)
  if (roomId) {
    url.searchParams.set('room', roomId)
  } else {
    url.searchParams.delete('room')
  }
  window.history.replaceState({}, '', url.toString())
}

export default function App() {
  const [session, setSession] = useState<Session | null>(() => {
    const room = readRoomFromUrl()
    const name = readStoredName().trim()
    if (room && name) {
      return {
        roomId: room,
        name,
        deck: [],
        spectator: false,
        create: false,
      }
    }
    return null
  })
  const [urlRoom, setUrlRoom] = useState(() => readRoomFromUrl())

  useEffect(() => {
    syncRoomToUrl(session?.roomId ?? null)
  }, [session])

  const goToEntry = () => {
    syncRoomToUrl(null)
    setUrlRoom('')
    setSession(null)
  }

  const fadeTransition = { duration: 0.25, ease: 'easeInOut' as const }

  return (
    <AnimatePresence mode="wait">
      {session ? (
        <motion.div
          key="room"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={fadeTransition}
        >
          <RoomScreen
            roomId={session.roomId}
            name={session.name}
            deck={session.deck}
            spectator={session.spectator}
            create={session.create}
            onLeave={goToEntry}
          />
        </motion.div>
      ) : urlRoom ? (
        <motion.div
          key="modal"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={fadeTransition}
        >
          <NamePromptModal
            roomId={urlRoom}
            onSubmit={(name) => {
              writeStoredName(name)
              setSession({
                roomId: urlRoom,
                name,
                deck: [],
                spectator: false,
                create: false,
              })
            }}
            onCancel={goToEntry}
          />
        </motion.div>
      ) : (
        <motion.div
          key="entry"
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -8 }}
          transition={fadeTransition}
        >
          <EntryScreen
            onJoin={(roomId, name, deck, spectator, create) =>
              setSession({ roomId, name, deck, spectator, create })
            }
          />
        </motion.div>
      )}
    </AnimatePresence>
  )
}
