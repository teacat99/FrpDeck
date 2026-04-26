<script setup lang="ts">
defineProps<{
  title?: string
  description?: string
  /** Short emoji or glyph rendered inside the circular illustration frame. */
  icon?: string
}>()
</script>

<template>
  <div class="flex flex-col items-center justify-center py-12 px-4 gap-2 text-muted-foreground">
    <div class="mb-1.5 size-[72px] rounded-2xl flex items-center justify-center text-3xl bg-muted text-primary">
      {{ icon ?? '✨' }}
    </div>
    <div v-if="title" class="text-base font-semibold text-foreground">
      {{ title }}
    </div>
    <div v-if="description" class="max-w-xs text-center text-sm leading-relaxed">
      {{ description }}
    </div>
    <div v-if="$slots.action" class="mt-3">
      <slot name="action" />
    </div>
  </div>
</template>
