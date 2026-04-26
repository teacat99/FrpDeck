// useRealtimeStore is the Pinia layer between the global WebSocket and
// the views. Views call `subscribeTunnels()` / `subscribeEndpoints()`
// during onMounted, the store reference-counts each topic, and only
// the first subscriber actually issues a `subscribe` frame upstream.
// The same applies in reverse on unsubscribe.
//
// Why reference counting: a user navigating between TunnelsView and
// EndpointsView would otherwise either issue a subscribe storm
// (tearing down on every transition is wasteful) or never tear down
// (server keeps shipping events nobody renders). The counter solves
// both.

import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'

import { WsClient, type ConnectionState } from '@/api/ws'
import type {
  EndpointState,
  RealtimeEvent,
  TunnelRuntimeState
} from '@/api/types'
import i18n from '@/i18n'
import { toast } from '@/lib/toast'
import { useAuthStore } from './auth'

interface EndpointLive {
  state: EndpointState
  err: string
  at: string
}

interface TunnelLive {
  state: TunnelRuntimeState
  err: string
  at: string
}

// TunnelExpiring captures the most recent `tunnel_expiring` warning
// the backend lifecycle manager emitted for a given tunnel. The TTL of
// the row is "until ExpireAt"; we keep stale entries around because
// the lifecycle manager may reuse them on reconcile, but the UI hides
// them once the remaining countdown reaches 0.
interface TunnelExpiring {
  // Remaining seconds at the moment the event was received. The view
  // re-derives the live countdown from `expiresAtMs`, so this is just
  // a snapshot for debugging / replay.
  remainingSec: number
  // Absolute deadline in epoch ms, computed from at + remainingSec so
  // the UI does not have to re-fetch the tunnel row.
  expiresAtMs: number
  name: string
  at: string
}

export const useRealtimeStore = defineStore('realtime', () => {
  const auth = useAuthStore()

  // Connection state surfaced to UI badges.
  const connection = ref<ConnectionState>('idle')
  // Last "hello" payload, primarily for debugging.
  const helloPayload = ref<Record<string, unknown> | null>(null)

  // Per-id live state. Keyed reactively so a v-for over the table
  // re-renders on update without us touching the surrounding array.
  const endpoints = reactive<Record<number, EndpointLive>>({})
  const tunnels = reactive<Record<number, TunnelLive>>({})
  // Indexed by tunnel id so a row can show "expiring soon" chrome
  // without a separate API call. Renew + Stop clear the entry.
  const expiring = reactive<Record<number, TunnelExpiring>>({})

  // Dedup: a single ExpireAt instant should only trigger one toast +
  // one browser notification. Keyed by tunnel id, value is the epoch
  // ms of the warning we last surfaced.
  const lastNotified: Record<number, number> = {}

  // Reference counts let multiple components subscribe to the same
  // topic without us double-sending the `subscribe` op.
  const topicRefs = reactive<Record<string, number>>({})

  const client = new WsClient(
    () => auth.token,
    {
      onState: (s) => {
        connection.value = s
      },
      onHello: (info) => {
        helloPayload.value = info
      },
      onReady: () => {
        // Re-subscribe to anything we cared about before the (re)connect.
        const topics = Object.keys(topicRefs).filter((t) => topicRefs[t] > 0)
        if (topics.length > 0) {
          client.subscribe(topics)
        }
      },
      onEvent: (ev) => applyEvent(ev)
    }
  )

  function applyEvent(ev: RealtimeEvent): void {
    const at = ev.at ?? new Date().toISOString()
    if (ev.type === 'endpoint_state' && ev.endpoint_id) {
      endpoints[ev.endpoint_id] = {
        state: (ev.state as EndpointState) ?? 'disconnected',
        err: ev.err ?? '',
        at
      }
    } else if (ev.type === 'tunnel_state' && ev.tunnel_id) {
      tunnels[ev.tunnel_id] = {
        state: (ev.state as TunnelRuntimeState) ?? 'pending',
        err: ev.err ?? '',
        at
      }
    } else if (ev.type === 'tunnel_expiring' && ev.tunnel_id) {
      onTunnelExpiring(ev.tunnel_id, ev.state ?? '0', ev.msg ?? '', at)
    }
    // EventLog is intentionally not stashed in the store; the future
    // log panel will subscribe to its own topic and stream lines into
    // a bounded ring buffer.
  }

  // onTunnelExpiring stores the remaining-time snapshot, fires a toast
  // (always) and a browser notification (when the user has granted
  // Notification permission). Dedup by (tunnel id, expiresAtMs) so a
  // process-restart safety publish + the per-tunnel timer cannot
  // double-notify the operator.
  function onTunnelExpiring(tunnelID: number, remainingRaw: string, name: string, at: string): void {
    const remainingSec = Math.max(0, parseInt(remainingRaw, 10) || 0)
    if (remainingSec <= 0) return
    const baseMs = Date.parse(at) || Date.now()
    const expiresAtMs = baseMs + remainingSec * 1000

    expiring[tunnelID] = { remainingSec, expiresAtMs, name, at }

    // Coalesce repeats for the same scheduled deadline.
    const seenAt = lastNotified[tunnelID]
    if (seenAt && Math.abs(seenAt - expiresAtMs) < 1000) return
    lastNotified[tunnelID] = expiresAtMs

    const t = i18n.global.t
    const minutes = Math.max(1, Math.round(remainingSec / 60))
    const title = t('tunnel.notify.expiring_title', { name: name || `#${tunnelID}` })
    const body = t('tunnel.notify.expiring_body', { minutes })
    toast.warning({ title, description: body, duration: 8000 })

    if (typeof window !== 'undefined' && 'Notification' in window) {
      try {
        if (Notification.permission === 'granted') {
          new Notification(title, { body, tag: `frpdeck-expiring-${tunnelID}` })
        }
      } catch {
        // Some browsers throw on construction in certain contexts
        // (e.g. private mode). Toast already covered the user, so
        // failing silently is OK.
      }
    }
  }

  // dismissExpiring clears the stored warning for a tunnel. The renew
  // / stop handlers call this so the UI badge disappears immediately,
  // before the next `tunnel_state` event arrives.
  function dismissExpiring(tunnelID: number): void {
    if (expiring[tunnelID]) delete expiring[tunnelID]
    delete lastNotified[tunnelID]
  }

  // ensureConnected is idempotent: calling it on every guard run is
  // free; the WsClient only re-dials when the previous attempt closed.
  function ensureConnected(): void {
    if (!auth.token) return
    if (connection.value === 'idle' || connection.value === 'closed') {
      client.connect()
    }
  }

  function disconnect(): void {
    client.disconnect()
  }

  function addRef(topic: string): void {
    const next = (topicRefs[topic] ?? 0) + 1
    topicRefs[topic] = next
    if (next === 1 && (connection.value === 'connected')) {
      client.subscribe([topic])
    }
  }

  function removeRef(topic: string): void {
    const next = (topicRefs[topic] ?? 0) - 1
    if (next <= 0) {
      delete topicRefs[topic]
      if (connection.value === 'connected') {
        client.unsubscribe([topic])
      }
    } else {
      topicRefs[topic] = next
    }
  }

  function subscribeTunnels(): () => void {
    addRef('tunnels')
    return () => removeRef('tunnels')
  }

  function subscribeEndpoints(): () => void {
    addRef('endpoints')
    return () => removeRef('endpoints')
  }

  function subscribeLogs(scope: 'all' | { endpoint?: number; tunnel?: number }): () => void {
    let topic: string
    if (scope === 'all') topic = 'logs:all'
    else if (scope.endpoint) topic = `logs:endpoint:${scope.endpoint}`
    else if (scope.tunnel) topic = `logs:tunnel:${scope.tunnel}`
    else return () => undefined
    addRef(topic)
    return () => removeRef(topic)
  }

  // Convenience accessors keep view templates terse.
  function endpointState(id: number): EndpointState {
    return endpoints[id]?.state ?? 'disconnected'
  }

  function tunnelState(id: number): TunnelRuntimeState | null {
    return tunnels[id]?.state ?? null
  }

  function tunnelExpiringInfo(id: number): TunnelExpiring | null {
    return expiring[id] ?? null
  }

  const isConnected = computed(() => connection.value === 'connected')

  return {
    connection,
    isConnected,
    helloPayload,
    endpoints,
    tunnels,
    expiring,
    ensureConnected,
    disconnect,
    subscribeTunnels,
    subscribeEndpoints,
    subscribeLogs,
    endpointState,
    tunnelState,
    tunnelExpiringInfo,
    dismissExpiring
  }
})
