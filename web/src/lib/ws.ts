export interface Frame<T = unknown> {
  v: number
  type: string
  ts: number
  payload: T
  ref?: string
}

export type ConnectionState = 'connecting' | 'open' | 'reconnecting' | 'closed'

type EventHandler = (payload: unknown, frame: Frame) => void

const MIN_BACKOFF_MS = 1000
const MAX_BACKOFF_MS = 30_000

/**
 * WsClient implements the hivemind realtime protocol (SPEC.md §5): connect,
 * hello, resume with per-channel cursors, heartbeat pong, and reconnect with
 * exponential backoff + jitter. Messages are never sent over the socket —
 * only server-authored events are dispatched here.
 */
export class WsClient {
  private socket: WebSocket | null = null
  private handlers = new Map<string, Set<EventHandler>>()
  private stateHandlers = new Set<(state: ConnectionState) => void>()
  private state: ConnectionState = 'closed'
  private backoff = MIN_BACKOFF_MS
  private closedByUser = false
  private cursors: Record<string, number> = {}

  connect(cursors: Record<string, number> = {}) {
    this.cursors = cursors
    this.closedByUser = false
    this.open()
  }

  private open() {
    this.setState(this.socket ? 'reconnecting' : 'connecting')
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${proto}://${window.location.host}/api/v1/ws`)
    this.socket = socket

    socket.onopen = () => {
      this.backoff = MIN_BACKOFF_MS
      this.setState('open')
    }

    socket.onmessage = (evt) => {
      let frame: Frame
      try {
        frame = JSON.parse(evt.data)
      } catch {
        return
      }
      if (frame.type === 'hello') {
        this.send('resume', { cursors: this.cursors })
      } else if (frame.type === 'ping') {
        this.send('pong', {})
      }
      this.dispatch(frame)
    }

    socket.onclose = () => {
      this.socket = null
      if (this.closedByUser) {
        this.setState('closed')
        return
      }
      this.setState('reconnecting')
      const delay = this.backoff + Math.random() * this.backoff * 0.2
      this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS)
      setTimeout(() => {
        if (!this.closedByUser) this.open()
      }, delay)
    }

    socket.onerror = () => socket.close()
  }

  send(type: string, payload: unknown, ref?: string) {
    if (this.socket?.readyState !== WebSocket.OPEN) return
    const frame: Frame = { v: 1, type, ts: Date.now(), payload, ref }
    this.socket.send(JSON.stringify(frame))
  }

  on(type: string, handler: EventHandler) {
    if (!this.handlers.has(type)) this.handlers.set(type, new Set())
    this.handlers.get(type)!.add(handler)
    return () => this.handlers.get(type)?.delete(handler)
  }

  onStateChange(handler: (state: ConnectionState) => void) {
    this.stateHandlers.add(handler)
    handler(this.state)
    return () => this.stateHandlers.delete(handler)
  }

  close() {
    this.closedByUser = true
    this.socket?.close()
  }

  private dispatch(frame: Frame) {
    for (const handler of this.handlers.get(frame.type) ?? []) {
      handler(frame.payload, frame)
    }
    for (const handler of this.handlers.get('*') ?? []) {
      handler(frame.payload, frame)
    }
  }

  private setState(state: ConnectionState) {
    this.state = state
    for (const handler of this.stateHandlers) handler(state)
  }
}

export const wsClient = new WsClient()
