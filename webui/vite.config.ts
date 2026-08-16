import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Vite 构建产物直接输出到 Go embed 目录，确保 go build 时静态资源可用。
// 开发模式下通过 proxy 转发 /api 与 /v1 到本地 Go 服务。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/web/static',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8099',
      '/v1': 'http://localhost:8099',
    },
  },
})
