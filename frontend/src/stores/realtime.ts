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
    }
    // EventLog is intentionally not stashed in the store; the future
    // log panel will subscribe to its own topic and stream lines into
    // a bounded ring buffer.
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

  const isConnected = computed(() => connection.value === 'connected')

  return {
    connection,
    isConnected,
    helloPayload,
    endpoints,
    tunnels,
    ensureConnected,
    disconnect,
    subscribeTunnels,
    subscribeEndpoints,
    subscribeLogs,
    endpointState,
    tunnelState
  }
})
