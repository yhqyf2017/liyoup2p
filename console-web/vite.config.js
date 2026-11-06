import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  base: './',
  plugins: [vue()],
  build: {
    outDir: 'dist',
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8900',
        changeOrigin: true,
      },
      '/ws': {
        target: 'http://127.0.0.1:8900',
        ws: true,
        changeOrigin: true,
      },
    },
  },
})
