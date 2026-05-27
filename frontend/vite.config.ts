import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:1983',
        changeOrigin: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('/react/') || id.includes('/react-dom/') || id.includes('/scheduler/')) return 'react-vendor';
            if (id.includes('recharts') || id.includes('d3-') || id.includes('victory-vendor')) return 'recharts-vendor';
            if (id.includes('lucide')) return 'lucide-vendor';
            return 'vendor';
          }
        }
      }
    },
    chunkSizeWarningLimit: 800,
  }
})
