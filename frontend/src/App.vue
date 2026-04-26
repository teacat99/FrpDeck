<script setup lang="ts">
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
</script>

<template>
  <TooltipProvider>
    <router-view />
    <Toaster />
  </TooltipProvider>
</template>
