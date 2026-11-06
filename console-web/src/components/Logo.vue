<template>
  <!-- 励友 P2P 品牌标识：蓝色渐变盾牌 + 金色四角星 -->
  <svg :width="size" :height="size" viewBox="0 0 64 64" fill="none" xmlns="http://www.w3.org/2000/svg">
    <defs>
      <linearGradient :id="gradId" x1="8" y1="6" x2="56" y2="58" gradientUnits="userSpaceOnUse">
        <stop stop-color="#2AA4E9" />
        <stop offset="1" stop-color="#0F51A8" />
      </linearGradient>
    </defs>
    <path
      d="M32 3 L57 12.5 V30 C57 46.5 46.6 57.6 32 61 C17.4 57.6 7 46.5 7 30 V12.5 Z"
      :fill="`url(#${gradId})`"
    />
    <path
      d="M32 8 L52.5 15.8 V30 C52.5 43.4 44.3 52.6 32 56 C19.7 52.6 11.5 43.4 11.5 30 V15.8 Z"
      fill="#FFFFFF"
      fill-opacity="0.12"
    />
    <path
      d="M32 15 Q34.2 28.2 48 30.5 Q34.2 32.8 32 46 Q29.8 32.8 16 30.5 Q29.8 28.2 32 15 Z"
      fill="#F5A623"
    />
  </svg>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  size: { type: Number, default: 40 },
})

// 多实例渲染时保证渐变 id 不重复
const gradId = computed(() => `ly-logo-grad-${Math.random().toString(36).slice(2, 10)}`)
</script>
