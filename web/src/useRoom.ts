import { useCallback, useEffect, useRef, useState } from 'react'
import type {
  Card,
  InboundFromServer,
  OutboundToServer,
  StatePayload,
} from './protocol'

export type ConnectionStatus =
  | 'connecting'
  | 'joining'
  | 'joined'
  | 'reconnecting'
  | 'error'

export interface UseRoomResult {
  status: ConnectionStatus
  state: StatePayload | null
  participantId: string | null
  error: string | null
  vote: (card: Card) => void
  reveal: () => void
  reset: () => void
  revote: () => void
  setStory: (story: string) => void
  setAutoReveal: (enabled: boolean) => void
}

const BACKOFFS_MS = [250, 500, 1000, 2000, 4000, 8000]

function backoffFor(attempt: number): number {
  return BACKOFFS_MS[Math.min(attempt, BACKOFFS_MS.length - 1)]
}

function resolveWsUrl(): string {
  const configured = (import.meta.env as { VITE_WS_URL?: string }).VITE_WS_URL
  if (configured && configured.trim()) return configured.trim()
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/ws`
}

function rejoinStorageKey(roomId: string): string {
  return `storypoker.rejoinId.${roomId}`
}

function readStoredRejoinId(roomId: string): string | null {
  try {
    return window.sessionStorage.getItem(rejoinStorageKey(roomId))
  } catch {
    return null
  }
}

function writeStoredRejoinId(roomId: string, id: string | null) {
  try {
    const key = rejoinStorageKey(roomId)
    if (id) window.sessionStorage.setItem(key, id)
    else window.sessionStorage.removeItem(key)
  } catch {
    /* sessionStorage unavailable */
  }
}

export function useRoom(
  roomId: string,
  name: string,
  deck: string[],
  spectator: boolean,
  create: boolean,
): UseRoomResult {
  const [status, setStatus] = useState<ConnectionStatus>('connecting')
  const [state, setState] = useState<StatePayload | null>(null)
  const [participantId, setParticipantId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const wsRef = useRef<WebSocket | null>(null)
  const rejoinIdRef = useRef<string | null>(null)
  const attemptRef = useRef(0)
  const reconnectTimerRef = useRef<number | null>(null)
  const unmountedRef = useRef(false)
  const everJoinedRef = useRef(false)
  const giveUpRef = useRef(false)

  useEffect(() => {
    unmountedRef.current = false
    everJoinedRef.current = false
    giveUpRef.current = false
    rejoinIdRef.current = readStoredRejoinId(roomId)

    // `cancelled` is per-effect-invocation. StrictMode's dev double-mount runs
    // setup → cleanup → setup synchronously; the first invocation's cleanup
    // flips its own `cancelled` to true, so its queued microtask aborts
    // before a stray WebSocket is opened.
    let cancelled = false

    const connect = () => {
      if (cancelled || unmountedRef.current) return
      reconnectTimerRef.current = null

      const ws = new WebSocket(resolveWsUrl())
      wsRef.current = ws
      setStatus(rejoinIdRef.current ? 'reconnecting' : 'connecting')

      ws.onopen = () => {
        setStatus('joining')
        const join: OutboundToServer = {
          type: 'join',
          payload: {
            roomId,
            name,
            ...(rejoinIdRef.current ? { rejoinId: rejoinIdRef.current } : {}),
            ...(deck.length > 0 ? { deck } : {}),
            ...(spectator ? { spectator: true } : {}),
            ...(create ? { create: true } : {}),
          },
        }
        ws.send(JSON.stringify(join))
      }

      ws.onmessage = (ev) => {
        let msg: InboundFromServer
        try {
          msg = JSON.parse(ev.data) as InboundFromServer
        } catch {
          return
        }
        switch (msg.type) {
          case 'joined':
            rejoinIdRef.current = msg.payload.participantId
            writeStoredRejoinId(roomId, msg.payload.participantId)
            setParticipantId(msg.payload.participantId)
            attemptRef.current = 0
            everJoinedRef.current = true
            setStatus('joined')
            setError(null)
            break
          case 'state':
            setState(msg.payload)
            break
          case 'error':
            setError(msg.payload.message)
            if (!everJoinedRef.current) {
              giveUpRef.current = true
              setStatus('error')
              writeStoredRejoinId(roomId, null)
              ws.close()
            }
            break
        }
      }

      ws.onclose = () => {
        wsRef.current = null
        if (unmountedRef.current || giveUpRef.current || cancelled) return
        const delay = backoffFor(attemptRef.current++)
        setStatus('reconnecting')
        reconnectTimerRef.current = window.setTimeout(connect, delay)
      }

      ws.onerror = () => {
        // onclose follows; reconnect there.
      }
    }

    // Defer to next microtask so a StrictMode cleanup that fires synchronously
    // after setup can flip `cancelled` before any WebSocket is created.
    queueMicrotask(connect)

    return () => {
      cancelled = true
      unmountedRef.current = true
      if (reconnectTimerRef.current != null) {
        clearTimeout(reconnectTimerRef.current)
        reconnectTimerRef.current = null
      }
      wsRef.current?.close()
      wsRef.current = null
      // Intentionally not clearing sessionStorage here: tab close clears it
      // automatically, and we want refresh / StrictMode-remount to preserve
      // the participantId so the rejoin path works.
    }
    // deck is captured at mount; it doesn't change for the lifetime of this session.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [roomId, name])

  const send = useCallback((msg: OutboundToServer) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg))
    }
  }, [])

  const vote = useCallback(
    (card: Card) => send({ type: 'vote', payload: { card } }),
    [send],
  )
  const reveal = useCallback(() => send({ type: 'reveal' }), [send])
  const reset = useCallback(() => send({ type: 'reset' }), [send])
  const revote = useCallback(() => send({ type: 'revote' }), [send])
  const setStory = useCallback(
    (story: string) => send({ type: 'setStory', payload: { story } }),
    [send],
  )
  const setAutoReveal = useCallback(
    (enabled: boolean) =>
      send({ type: 'setAutoReveal', payload: { enabled } }),
    [send],
  )

  return {
    status,
    state,
    participantId,
    error,
    vote,
    reveal,
    reset,
    revote,
    setStory,
    setAutoReveal,
  }
}
