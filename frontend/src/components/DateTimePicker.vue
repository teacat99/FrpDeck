<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import dayjs, { type Dayjs } from 'dayjs'
import { Calendar as CalendarIcon, ChevronLeft, ChevronRight, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

// Custom lightweight DateTimePicker built on shadcn Popover + dayjs. We
// deliberately avoid radix-vue Calendar + @internationalized/date to keep
// the bundle small and the template fully style-controllable. The wire
// format mirrors <input type="datetime-local">'s `YYYY-MM-DDTHH:mm` so
// the component is a drop-in replacement for existing v-model bindings.

interface Props {
  modelValue?: string | null
  placeholder?: string
  disabled?: boolean
  class?: string
  /** Dayjs parse format - tolerate historical ISO plus native datetime-local. */
  parseFormats?: string[]
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: '',
  placeholder: '',
  disabled: false
})

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
}>()

const { locale } = useI18n()

const WIRE_FORMAT = 'YYYY-MM-DDTHH:mm'
const DISPLAY_FORMAT = 'YYYY-MM-DD HH:mm'

function parseValue(v?: string | null): Dayjs | null {
  if (!v) return null
  const d = dayjs(v)
  return d.isValid() ? d : null
}

const open = ref(false)
const selected = computed(() => parseValue(props.modelValue))
const viewMonth = ref<Dayjs>(selected.value ?? dayjs().startOf('day'))

// Keep the calendar pinned to the committed value whenever it changes
// externally (form reset, preset click), otherwise the user would see a
// stale month when reopening.
watch(
  () => props.modelValue,
  (v) => {
    const d = parseValue(v)
    if (d) viewMonth.value = d
  }
)

const hour = ref<number>(selected.value ? selected.value.hour() : 0)
const minute = ref<number>(selected.value ? selected.value.minute() : 0)

watch(open, (v) => {
  if (v) {
    // Sync time inputs when reopening so stale edits don't persist after
    // the user discarded the previous session with Clear.
    hour.value = selected.value ? selected.value.hour() : 0
    minute.value = selected.value ? selected.value.minute() : 0
    viewMonth.value = selected.value ?? dayjs().startOf('day')
  }
})

const displayText = computed(() => {
  const d = selected.value
  return d ? d.format(DISPLAY_FORMAT) : ''
})

// Zero-based Sun..Sat labels; we derive from dayjs so the locale switches
// automatically. (dayjs weekdays default to English unless locales are
// loaded, so we keep it minimal and localise via i18n lookup instead.)
const weekdayLabels = computed(() => {
  if (locale.value === 'zh-CN') return ['日', '一', '二', '三', '四', '五', '六']
  return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
})

interface DayCell {
  date: Dayjs
  inCurrentMonth: boolean
  isToday: boolean
  isSelected: boolean
  disabled: boolean
}

const grid = computed<DayCell[]>(() => {
  const first = viewMonth.value.startOf('month')
  const gridStart = first.subtract(first.day(), 'day')
  const today = dayjs().startOf('day')
  const selDay = selected.value ? selected.value.startOf('day') : null
  const cells: DayCell[] = []
  for (let i = 0; i < 42; i++) {
    const d = gridStart.add(i, 'day')
    cells.push({
      date: d,
      inCurrentMonth: d.month() === viewMonth.value.month(),
      isToday: d.isSame(today, 'day'),
      isSelected: selDay ? d.isSame(selDay, 'day') : false,
      disabled: false
    })
  }
  return cells
})

const monthLabel = computed(() => {
  if (locale.value === 'zh-CN') {
    return `${viewMonth.value.year()} 年 ${viewMonth.value.month() + 1} 月`
  }
  return viewMonth.value.format('MMMM YYYY')
})

function prevMonth() {
  viewMonth.value = viewMonth.value.subtract(1, 'month')
}
function nextMonth() {
  viewMonth.value = viewMonth.value.add(1, 'month')
}

function clampTime() {
  if (hour.value < 0) hour.value = 0
  if (hour.value > 23) hour.value = 23
  if (minute.value < 0) minute.value = 0
  if (minute.value > 59) minute.value = 59
}

function pickDay(cell: DayCell) {
  clampTime()
  const d = cell.date.hour(hour.value).minute(minute.value).second(0)
  emit('update:modelValue', d.format(WIRE_FORMAT))
  open.value = false
}

function setToday() {
  const now = dayjs()
  hour.value = now.hour()
  minute.value = now.minute()
  emit('update:modelValue', now.format(WIRE_FORMAT))
  open.value = false
}

function clearValue() {
  emit('update:modelValue', '')
  open.value = false
}

// When the user edits the hour/minute inputs directly, commit the change
// so the committed value stays in sync with what's visible. We only emit
// when there's already a selected date - a lone time edit with no date
// would produce an ambiguous value.
function onTimeCommit() {
  clampTime()
  if (!selected.value) return
  const d = selected.value.hour(hour.value).minute(minute.value).second(0)
  emit('update:modelValue', d.format(WIRE_FORMAT))
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        type="button"
        :disabled="disabled"
        :class="cn(
          'flex h-9 w-full items-center gap-2 rounded-md border border-input bg-transparent px-3 text-sm shadow-sm transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
          !displayText && 'text-muted-foreground',
          props.class
        )"
      >
        <CalendarIcon class="size-4 shrink-0 opacity-60" />
        <span class="flex-1 text-left truncate">
          {{ displayText || placeholder || (locale === 'zh-CN' ? '选择日期时间' : 'Pick date & time') }}
        </span>
        <span
          v-if="displayText"
          class="inline-flex size-5 items-center justify-center rounded-sm opacity-60 hover:opacity-100 hover:bg-muted"
          role="button"
          :aria-label="locale === 'zh-CN' ? '清空' : 'Clear'"
          @click.stop="clearValue"
        >
          <X class="size-3" />
        </span>
      </button>
    </PopoverTrigger>

    <PopoverContent :align="'start'" class="w-auto p-3">
      <div class="flex flex-col gap-3">
        <!-- Month nav -->
        <div class="flex items-center justify-between">
          <Button variant="ghost" size="icon" class="size-7" type="button" @click="prevMonth">
            <ChevronLeft class="size-4" />
          </Button>
          <span class="text-sm font-medium tabular-nums">{{ monthLabel }}</span>
          <Button variant="ghost" size="icon" class="size-7" type="button" @click="nextMonth">
            <ChevronRight class="size-4" />
          </Button>
        </div>

        <!-- Weekday labels -->
        <div class="grid grid-cols-7 gap-1 text-center">
          <span
            v-for="w in weekdayLabels"
            :key="w"
            class="text-[11px] text-muted-foreground font-medium"
          >{{ w }}</span>
        </div>

        <!-- Days grid -->
        <div class="grid grid-cols-7 gap-1">
          <button
            v-for="(cell, idx) in grid"
            :key="idx"
            type="button"
            :class="cn(
              'size-8 text-sm rounded-md inline-flex items-center justify-center transition-colors',
              !cell.inCurrentMonth && 'text-muted-foreground/50',
              cell.isSelected && 'bg-primary text-primary-foreground hover:bg-primary',
              !cell.isSelected && cell.isToday && 'border border-primary text-primary',
              !cell.isSelected && !cell.isToday && 'hover:bg-accent'
            )"
            @click="pickDay(cell)"
          >{{ cell.date.date() }}</button>
        </div>

        <!-- Time + actions -->
        <div class="flex items-center justify-between pt-2 border-t border-border">
          <div class="flex items-center gap-1 text-sm">
            <input
              v-model.number="hour"
              type="number"
              min="0"
              max="23"
              class="h-8 w-12 rounded-md border border-input bg-transparent px-2 text-center tabular-nums text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              @blur="onTimeCommit"
              @keydown.enter="onTimeCommit"
            />
            <span class="text-muted-foreground">:</span>
            <input
              v-model.number="minute"
              type="number"
              min="0"
              max="59"
              class="h-8 w-12 rounded-md border border-input bg-transparent px-2 text-center tabular-nums text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              @blur="onTimeCommit"
              @keydown.enter="onTimeCommit"
            />
          </div>
          <div class="flex items-center gap-1">
            <Button variant="ghost" size="sm" type="button" @click="clearValue">
              {{ locale === 'zh-CN' ? '清空' : 'Clear' }}
            </Button>
            <Button size="sm" type="button" @click="setToday">
              {{ locale === 'zh-CN' ? '此刻' : 'Now' }}
            </Button>
          </div>
        </div>
      </div>
    </PopoverContent>
  </Popover>
</template>
