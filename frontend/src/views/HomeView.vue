<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Server, Network, Activity, Cpu, ArrowRight } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { listEndpoints, fetchVersion } from '@/api/endpoints'
import { listTunnels } from '@/api/tunnels'
import type { Endpoint, Tunnel, VersionInfo } from '@/api/types'

const { t } = useI18n()
const router = useRouter()

const endpoints = ref<Endpoint[]>([])
const tunnels = ref<Tunnel[]>([])
const version = ref<VersionInfo | null>(null)
const loading = ref(false)

const activeCount = computed(() => tunnels.value.filter(x => x.status === 'active').length)
const expiringCount = computed(() => tunnels.value.filter(x => !!x.expire_at && x.status === 'active').length)

async function reload() {
  loading.value = true
  try {
    const [eps, tns, ver] = await Promise.all([
      listEndpoints(),
      listTunnels(),
      fetchVersion(),
    ])
    endpoints.value = eps
    tunnels.value = tns
    version.value = ver
  } finally {
    loading.value = false
  }
}

onMounted(reload)
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex items-end justify-between flex-wrap gap-3">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('home.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('home.subtitle') }}</p>
      </div>
      <Badge v-if="version" variant="secondary" class="font-mono">
        {{ version.driver }} · frp {{ version.frp_version }}
      </Badge>
    </div>

    <div class="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-4">
      <Card>
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium">{{ t('home.cards.endpoints') }}</CardTitle>
          <Server class="size-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ endpoints.length }}</div>
          <p class="text-xs text-muted-foreground">{{ t('home.cards.endpoints_hint') }}</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium">{{ t('home.cards.tunnels') }}</CardTitle>
          <Network class="size-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ tunnels.length }}</div>
          <p class="text-xs text-muted-foreground">{{ t('home.cards.tunnels_hint') }}</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium">{{ t('home.cards.active') }}</CardTitle>
          <Activity class="size-4 text-emerald-500" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ activeCount }}</div>
          <p class="text-xs text-muted-foreground">{{ t('home.cards.active_hint') }}</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium">{{ t('home.cards.expiring') }}</CardTitle>
          <Cpu class="size-4 text-amber-500" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ expiringCount }}</div>
          <p class="text-xs text-muted-foreground">{{ t('home.cards.expiring_hint') }}</p>
        </CardContent>
      </Card>
    </div>

    <Card>
      <CardHeader>
        <CardTitle>{{ t('home.next_steps.title') }}</CardTitle>
        <CardDescription>{{ t('home.next_steps.subtitle') }}</CardDescription>
      </CardHeader>
      <CardContent class="flex flex-wrap gap-2">
        <Button variant="outline" @click="router.push({ name: 'endpoints' })">
          <Server class="size-4" />
          <span>{{ t('menu.endpoints') }}</span>
          <ArrowRight class="size-4" />
        </Button>
        <Button variant="outline" @click="router.push({ name: 'tunnels' })">
          <Network class="size-4" />
          <span>{{ t('menu.tunnels') }}</span>
          <ArrowRight class="size-4" />
        </Button>
      </CardContent>
    </Card>
  </div>
</template>
