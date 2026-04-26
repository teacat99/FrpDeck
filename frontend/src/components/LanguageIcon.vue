<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    /** 覆盖自动从 i18n locale 推断；zh 显示"中/A"图标，en 显示"A/中"图标 */
    locale?: 'zh' | 'en'
    size?: number | string
  }>(),
  { size: 20 },
)

const { locale: i18nLocale } = useI18n()
const isZh = computed(() => {
  const v = props.locale ?? String(i18nLocale.value)
  return v.toLowerCase().startsWith('zh')
})
</script>

<template>
  <svg
    v-if="isZh"
    :width="size"
    :height="size"
    viewBox="0 0 20 20"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <path
      d="M15 8.33417L18.6675 17.5008H16.8717L15.8717 15.0008H12.4625L11.4625 17.5008H9.6675L13.3342 8.33417H15ZM14.1675 10.7383L13.1275 13.3333H15.205L14.1675 10.7367V10.7383ZM4.16667 12.5V14.1667C4.16674 14.587 4.32564 14.9918 4.61153 15.2999C4.89742 15.6081 5.28918 15.7968 5.70833 15.8283L5.83333 15.8333H8.33333V17.5H5.83333C4.94928 17.5 4.10143 17.1488 3.47631 16.5237C2.85119 15.8986 2.5 15.0507 2.5 14.1667V12.5H4.16667ZM14.1667 2.5C15.0507 2.5 15.8986 2.85119 16.5237 3.47631C17.1488 4.10143 17.5 4.94928 17.5 5.83333V7.5H15.8333V5.83333C15.8333 5.39145 15.6579 4.96765 15.3455 4.65512C15.0331 4.34258 14.6094 4.16689 14.1675 4.16667H11.6675V2.5H14.1675H14.1667Z"
      fill="currentColor"
      fill-opacity="0.55"
    />
    <path
      d="M6.66504 1.66602V3.33352H9.99837V9.16602H6.66504V11.666H4.99837V9.16602H1.66504V3.33352H4.99837V1.66602H6.66504ZM4.99837 4.99935H3.33171V7.49935H5.00004V5.00018H4.99837V4.99935ZM8.3317 4.99935H6.66504V7.49935H8.3317V5.00018V4.99935Z"
      fill="currentColor"
    />
  </svg>
  <svg
    v-else
    :width="size"
    :height="size"
    viewBox="0 0 20 20"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <path
      d="M17.4999 5.84V7.5H15.8299V5.84C15.8299 5.39 15.6599 4.97 15.3499 4.66C15.0399 4.35 14.6099 4.17 14.1699 4.17H11.6699V2.5H14.1699C15.0499 2.5 15.8999 2.85 16.5199 3.48C17.1499 4.1 17.4999 4.95 17.4999 5.84Z"
      fill="currentColor"
      fill-opacity="0.55"
    />
    <path
      d="M8.33 15.84V17.5H5.83C4.95 17.5 4.1 17.15 3.48 16.53C2.86 15.91 2.5 15.05 2.5 14.17V12.5H4.17V14.17C4.17 14.59 4.33 14.99 4.61 15.3C4.9 15.61 5.29 15.8 5.71 15.83H5.83L8.33 15.84Z"
      fill="currentColor"
      fill-opacity="0.55"
    />
    <path
      d="M7.33977 0.648438H5.66977L2.00977 9.80844H3.79977L4.79977 7.30844H8.20977L9.20977 9.80844H11.0098L7.33977 0.648438ZM5.46977 5.64844L6.50977 3.04844L7.53977 5.64844H5.46977Z"
      fill="currentColor"
    />
    <path
      d="M15 9V10.67H18.33V16.5H15V19H13.33V16.5H10V10.67H13.33V9H15ZM13.33 12.33H11.66V14.83H13.33V12.33ZM16.67 12.33H15V14.83H16.67V12.33Z"
      fill="currentColor"
      fill-opacity="0.55"
    />
  </svg>
</template>
