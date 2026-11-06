<template>
  <!-- 励联 LiLink 品牌标识：官方徽标图（与 Windows GUI 同源 appicon.png） -->
  <img
    :src="logoUrl"
    :width="size"
    :height="size"
    class="brand-logo-img"
    :class="{ 'on-dark': onDark }"
    alt="励联 LiLink"
  />
</template>

<script setup>
import { computed } from 'vue'
import logoSrc from '../assets/logo.png'

const props = defineProps({
  size: { type: Number, default: 40 },
  // 在深色背景上使用时加白色圆角衬底，保证徽标对比度（参照 GUI 登录页做法）
  onDark: { type: Boolean, default: false },
})

const logoUrl = computed(() => logoSrc)
</script>

<style scoped>
.brand-logo-img {
  display: block;
  border-radius: 10px;
  object-fit: cover;
}

.brand-logo-img.on-dark {
  background: #ffffff;
  padding: 3px;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.18);
}
</style>
