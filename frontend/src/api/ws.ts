// Tiny browser-side wrapper around the global /api/ws WebSocket. The
// realtime store (stores/realtime.ts) owns a singleton instance and
// drives reconnect; views just `subscribe()` / `unsubscribe()` to
// declare which channels they care about.
//
// Reconnect strategy: exponential backoff capped at 30 s. We do not
// give up — the user is presumed to be inside the FrpDeck UI and would
// rather wait for the network to come back than refresh the page.

import type { RealtimeEvent } from './types'

// Wire envelope shared with internal/api/ws.go. `op` flows browser →
// server (control), `event` flows server → browser (notifications).
export interface WsMessage {
  op?: 'subscribe' | 'unsubscribe' | 'ping'
  topics?: string[]
  event?: 'hello' | 'ack' | 'pong' | 'endpoint_state' | 'tunnel_state' | 'log'
  data?: unknown
  err?: string
}

export type ConnectionState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'closed'

// Browsers can't set Authorization on a WS handshake, so we forward the
// JWT through the second slot of the Sec-WebSocket-Protocol negotiation
// list — see internal/api/ws.go authWebSocket. The first slot
// ("jwt") is the protocol identifier the server agrees to.
function buildUrl(): string {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/api/ws`
}

export interface WsClientHandlers {
  onState?: (state: ConnectionState) => void
  onEvent?: (e: RealtimeEvent) => void
  onHello?: (info: Record<string, unknown>) => void
  // Lets the store re-issue subscribe frames after a reconnect. Called
  // immediately after the server `hello` arrives.
  onReady?: () => void
}

// WsClient owns the underlying WebSocket and exposes a tiny imperative
// surface. Pinia store wraps it with reactive state.
export class WsClient {
  private ws: WebSocket | null = null
  private url: string
  private getToken: () => string
  private handlers: WsClientHandlers
  private retry = 0
  private retryTimer: number | null = null
  private explicitlyClosed = false

  constructor(getToken: () => string, handlers: WsClientHandlers) {
    this.url = buildUrl()
    this.getToken = getToken
    this.handlers = handlers
  }

  connect(): void {
    this.explicitlyClosed = false
    const token = this.getToken()
    if (!token) {
      this.setState('idle')
      return
    }
    this.setState(this.retry === 0 ? 'connecting' : 'reconnecting')
    try {
      this.ws = new WebSocket(this.url, ['jwt', token])
    } catch {
      this.scheduleReconnect()
      return
    }
    this.ws.onopen = () => {
      // The server still sends a `hello` event before we treat the
      // connection as live; staying in "connecting" until then surfaces
      // auth failures as an immediate close instead of a flicker.
    }
    this.ws.onmessage = (ev) => {
      let msg: WsMessage
      try {
        msg = JSON.parse(ev.data as string)
      } catch {
        return
      }
      if (msg.event === 'hello') {
        this.retry = 0
        this.setState('connected')
        if (msg.data && typeof msg.data === 'object') {
          this.handlers.onHello?.(msg.data as Record<string, unknown>)
        }
        this.handlers.onReady?.()
        return
      }
      if (msg.event === 'endpoint_state' || msg.event === 'tunnel_state' || msg.event === 'log') {
        if (msg.data && typeof msg.data === 'object') {
          this.handlers.onEvent?.(msg.data as RealtimeEvent)
        }
      }
    }
    this.ws.onclose = () => {
      this.ws = null
      if (this.explicitlyClosed) {
        this.setState('closed')
        return
      }
      this.scheduleReconnect()
    }
    this.ws.onerror = () => {
      // onclose runs right after, so let it drive the reconnect path.
    }
  }

  disconnect(): void {
    this.explicitlyClosed = true
    if (this.retryTimer !== null) {
      window.clearTimeout(this.retryTimer)
      this.retryTimer = null
    }
    this.ws?.close(1000, 'client closing')
    this.ws = null
    this.setState('closed')
  }

  send(msg: WsMessage): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN) return false
    this.ws.send(JSON.stringify(msg))
    return true
  }

  subscribe(topics: string[]): void {
    if (topics.length === 0) return
    this.send({ op: 'subscribe', topics })
  }

  unsubscribe(topics: string[]): void {
    if (topics.length === 0) return
    this.send({ op: 'unsubscribe', topics })
  }

  // Internal helper: schedule the next reconnect with exponential
  // backoff and a small jitter so multiple tabs don't synchronise their
  // reconnect storms onto the server.
  private scheduleReconnect(): void {
    this.setState('reconnecting')
    const base = Math.min(30_000, 1000 * Math.pow(2, this.retry))
    const jitter = Math.floor(Math.random() * 500)
    this.retry += 1
    this.retryTimer = window.setTimeout(() => {
      this.retryTimer = null
      this.connect()
    }, base + jitter)
  }

  private setState(state: ConnectionState): void {
    this.handlers.onState?.(state)
  }
}
