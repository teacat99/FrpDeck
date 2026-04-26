<script setup lang="ts">
import { computed, watch, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'

// PluginConfigEditor renders a typed form for known frpc plugin types
// (P5-C polish). For unknown plugins it falls back to a free-form
// textarea so operators can still copy-paste raw TOML fragments.
//
// The component does not import a TOML parser to keep the bundle slim.
// Instead, we accept the limited shape used by frp client plugin
// options (`key = "value"`, `key = true`, `key = 123` lines) and
// round-trip them through a small in-house serializer. Unknown lines
// are surfaced verbatim in a "raw extras" textarea so we never silently
// drop content the backend will need.

interface PluginField {
  key: string
  type: 'string' | 'boolean' | 'number' | 'password'
  labelKey: string
  hintKey?: string
  placeholder?: string
}

interface PluginSchema {
  type: string
  fields: PluginField[]
}

// Schema registry. Key names mirror frp's JSON tags so the import
// pipeline (which ships TOML produced from the typed structs) lines up
// without translation. Adding a new plugin just needs a row here.
const SCHEMAS: PluginSchema[] = [
  {
    type: 'static_file',
    fields: [
      { key: 'localPath', type: 'string', labelKey: 'plugin.field.localPath', placeholder: '/srv/static' },
      { key: 'stripPrefix', type: 'string', labelKey: 'plugin.field.stripPrefix', placeholder: 'static' },
      { key: 'httpUser', type: 'string', labelKey: 'plugin.field.httpUser' },
      { key: 'httpPassword', type: 'password', labelKey: 'plugin.field.httpPassword' },
    ],
  },
  {
    type: 'unix_domain_socket',
    fields: [
      { key: 'unixPath', type: 'string', labelKey: 'plugin.field.unixPath', placeholder: '/var/run/app.sock' },
    ],
  },
  {
    type: 'http_proxy',
    fields: [
      { key: 'httpUser', type: 'string', labelKey: 'plugin.field.httpUser' },
      { key: 'httpPassword', type: 'password', labelKey: 'plugin.field.httpPassword' },
    ],
  },
  {
    type: 'socks5',
    fields: [
      { key: 'username', type: 'string', labelKey: 'plugin.field.username' },
      { key: 'password', type: 'password', labelKey: 'plugin.field.password' },
    ],
  },
  {
    type: 'https2http',
    fields: [
      { key: 'localAddr', type: 'string', labelKey: 'plugin.field.localAddr', placeholder: '127.0.0.1:8080' },
      { key: 'hostHeaderRewrite', type: 'string', labelKey: 'plugin.field.hostHeaderRewrite' },
      { key: 'crtPath', type: 'string', labelKey: 'plugin.field.crtPath', placeholder: '/etc/ssl/cert.pem' },
      { key: 'keyPath', type: 'string', labelKey: 'plugin.field.keyPath', placeholder: '/etc/ssl/key.pem' },
    ],
  },
  {
    type: 'https2https',
    fields: [
      { key: 'localAddr', type: 'string', labelKey: 'plugin.field.localAddr', placeholder: '127.0.0.1:8443' },
      { key: 'hostHeaderRewrite', type: 'string', labelKey: 'plugin.field.hostHeaderRewrite' },
      { key: 'crtPath', type: 'string', labelKey: 'plugin.field.crtPath' },
      { key: 'keyPath', type: 'string', labelKey: 'plugin.field.keyPath' },
    ],
  },
  {
    type: 'http2https',
    fields: [
      { key: 'localAddr', type: 'string', labelKey: 'plugin.field.localAddr', placeholder: '127.0.0.1:443' },
      { key: 'hostHeaderRewrite', type: 'string', labelKey: 'plugin.field.hostHeaderRewrite' },
    ],
  },
  {
    type: 'tls2raw',
    fields: [
      { key: 'localAddr', type: 'string', labelKey: 'plugin.field.localAddr' },
      { key: 'crtPath', type: 'string', labelKey: 'plugin.field.crtPath' },
      { key: 'keyPath', type: 'string', labelKey: 'plugin.field.keyPath' },
    ],
  },
]

interface Props {
  plugin: string
  modelValue: string
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
}>()

const { t } = useI18n()

const schema = computed<PluginSchema | null>(() => {
  if (!props.plugin) return null
  return SCHEMAS.find(s => s.type === props.plugin) ?? null
})

interface ParsedConfig {
  fields: Record<string, string | boolean | number>
  extras: string[]
}

// Parse a TOML-ish flat block "key = value" with optional inline
// comments. Strings live in double quotes; booleans/numbers ride bare.
// Unrecognised lines flow into `extras` so we can put them back when
// re-serialising — the editor never destroys data it does not own.
function parseConfig(raw: string): ParsedConfig {
  const out: ParsedConfig = { fields: {}, extras: [] }
  if (!raw) return out
  const lines = raw.split(/\r?\n/)
  for (const rawLine of lines) {
    const line = rawLine.trim()
    if (!line || line.startsWith('#')) {
      if (line) out.extras.push(rawLine)
      continue
    }
    const m = /^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+?)\s*$/.exec(line)
    if (!m) {
      out.extras.push(rawLine)
      continue
    }
    const key = m[1]
    let valueRaw = m[2].trim()
    // Strip trailing inline comment (only when not inside quotes).
    if (!valueRaw.startsWith('"') && !valueRaw.startsWith("'")) {
      const hashAt = valueRaw.indexOf('#')
      if (hashAt > -1) valueRaw = valueRaw.slice(0, hashAt).trim()
    }
    let value: string | boolean | number = valueRaw
    if (valueRaw === 'true' || valueRaw === 'false') {
      value = valueRaw === 'true'
    } else if (/^-?\d+(\.\d+)?$/.test(valueRaw)) {
      value = Number(valueRaw)
    } else if (/^"(.*)"$/s.test(valueRaw)) {
      value = valueRaw.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, '\\')
    } else if (/^'(.*)'$/s.test(valueRaw)) {
      value = valueRaw.slice(1, -1)
    }
    out.fields[key] = value
  }
  return out
}

function escapeToml(s: string): string {
  return s.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
}

function serialiseConfig(parsed: ParsedConfig): string {
  const lines: string[] = []
  for (const [k, v] of Object.entries(parsed.fields)) {
    if (v === '' || v === null || v === undefined) continue
    if (typeof v === 'string') {
      lines.push(`${k} = "${escapeToml(v)}"`)
    } else {
      lines.push(`${k} = ${String(v)}`)
    }
  }
  for (const e of parsed.extras) {
    if (e.trim().length) lines.push(e)
  }
  return lines.join('\n')
}

const parsed = ref<ParsedConfig>(parseConfig(props.modelValue))

// Sync inbound prop changes (e.g. operator pasted a fresh fragment) but
// only when the new payload is structurally different from what we
// already have, otherwise keystroke-driven re-emits would clobber
// transient state like trailing commas.
watch(
  () => props.modelValue,
  (next) => {
    const reSer = serialiseConfig(parsed.value)
    if (next !== reSer) {
      parsed.value = parseConfig(next)
    }
  },
)

function valueOf(field: PluginField): string | boolean {
  const raw = parsed.value.fields[field.key]
  if (raw === undefined || raw === null) return field.type === 'boolean' ? false : ''
  if (field.type === 'boolean') return Boolean(raw)
  return String(raw)
}

function onFieldChange(field: PluginField, value: string | boolean | number) {
  if (field.type === 'string' || field.type === 'password') {
    if (value === '' || value === null || value === undefined) {
      delete parsed.value.fields[field.key]
    } else {
      parsed.value.fields[field.key] = String(value)
    }
  } else if (field.type === 'boolean') {
    if (!value) {
      delete parsed.value.fields[field.key]
    } else {
      parsed.value.fields[field.key] = true
    }
  } else if (field.type === 'number') {
    if (value === '' || Number.isNaN(Number(value))) {
      delete parsed.value.fields[field.key]
    } else {
      parsed.value.fields[field.key] = Number(value)
    }
  }
  emit('update:modelValue', serialiseConfig(parsed.value))
}

const hasExtras = computed(() => parsed.value.extras.length > 0)

const showRaw = ref(false)

function onRawInput(value: string) {
  parsed.value = parseConfig(value)
  emit('update:modelValue', value)
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <div v-if="!plugin" class="text-xs text-muted-foreground">
      {{ t('plugin.empty_hint') }}
    </div>

    <div v-else-if="schema" class="flex flex-col gap-3">
      <div class="grid grid-cols-2 gap-3">
        <div
          v-for="f in schema.fields"
          :key="f.key"
          class="flex flex-col gap-1.5"
          :class="{ 'col-span-2': f.type === 'string' && (f.placeholder?.includes('/') || f.key.toLowerCase().includes('addr')) }"
        >
          <Label class="text-xs">{{ t(f.labelKey) }} <span class="text-muted-foreground">({{ f.key }})</span></Label>
          <Switch
            v-if="f.type === 'boolean'"
            :checked="!!valueOf(f)"
            @update:checked="(v: boolean) => onFieldChange(f, v)"
          />
          <Input
            v-else
            :type="f.type === 'password' ? 'password' : f.type === 'number' ? 'number' : 'text'"
            :model-value="valueOf(f) as string"
            :placeholder="f.placeholder"
            @update:model-value="(v: any) => onFieldChange(f, v)"
          />
          <span v-if="f.hintKey" class="text-[11px] text-muted-foreground">{{ t(f.hintKey) }}</span>
        </div>
      </div>

      <div v-if="hasExtras" class="rounded-md border border-amber-500/50 bg-amber-500/5 p-2 text-xs text-amber-600 dark:text-amber-400">
        {{ t('plugin.extras_warning') }}
      </div>

      <div class="flex items-center gap-2">
        <Button type="button" variant="ghost" size="sm" @click="showRaw = !showRaw">
          {{ showRaw ? t('plugin.hide_raw') : t('plugin.show_raw') }}
        </Button>
      </div>

      <textarea
        v-if="showRaw"
        :value="modelValue"
        rows="4"
        class="w-full font-mono text-xs leading-relaxed rounded-md border bg-muted/30 p-2"
        :placeholder="t('plugin.raw_placeholder')"
        @input="(e: Event) => onRawInput((e.target as HTMLTextAreaElement).value)"
      />
    </div>

    <div v-else class="flex flex-col gap-1.5">
      <textarea
        :value="modelValue"
        rows="4"
        class="w-full font-mono text-xs leading-relaxed rounded-md border bg-background p-2"
        :placeholder="t('plugin.raw_placeholder')"
        @input="(e: Event) => emit('update:modelValue', (e.target as HTMLTextAreaElement).value)"
      />
      <span class="text-[11px] text-muted-foreground">{{ t('plugin.unknown_hint', { plugin }) }}</span>
    </div>
  </div>
</template>
