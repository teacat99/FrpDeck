<script setup lang="ts">
import { Toaster, type ToasterProps } from 'vue-sonner'
import { useThemeStore } from '@/stores/theme'
import { computed } from 'vue'

const props = defineProps<ToasterProps>()
const theme = useThemeStore()
const resolvedTheme = computed<ToasterProps['theme']>(() =>
  theme.isDark ? 'dark' : 'light'
)
</script>

<template>
  <Toaster
    v-bind="props"
    :theme="resolvedTheme"
    position="top-right"
    rich-colors
    close-button
    :offset="8"
    :toast-options="{
      classes: {
        toast: 'rounded-md border-border shadow-float !py-2 !min-h-0',
        closeButton: '!left-auto !-right-1 !top-1/2 !-translate-y-1/2 !translate-x-full',
      }
    }"
  />
</template>
