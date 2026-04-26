import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'
import path from 'node:path'

// FrpDeck frontend build config.
//   - dist is emitted to ../web/dist so Go embed picks it up.
//   - /api/* during dev is proxied to the Go backend on :8080.
//   - Tailwind CSS v4 via the @tailwindcss/vite plugin drives all styling.
//     shadcn-vue primitives are copied into src/components/ui and compiled
//     alongside the rest of the source, so there is no external UI kit to
//     lazy-load.
export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['favicon.svg', 'icons/*'],
      manifest: {
        name: 'FrpDeck',
        short_name: 'FrpDeck',
        description: 'Temporary firewall port opener',
        theme_color: '#f6f8fb',
        background_color: '#f6f8fb',
        display: 'standalone',
        start_url: '/',
        icons: [
          { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
          { src: '/icons/icon-maskable.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' }
        ]
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
        runtimeCaching: [
          {
            urlPattern: ({ url }) => url.pathname.startsWith('/api/'),
            handler: 'NetworkFirst',
            options: {
              cacheName: 'frpdeck-api',
              networkTimeoutSeconds: 5,
              expiration: { maxEntries: 50, maxAgeSeconds: 60 * 5 }
            }
          }
        ]
      }
    })
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src')
    }
  },
  build: {
    outDir: path.resolve(__dirname, '../web/dist'),
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 800,
    rollupOptions: {
      output: {
        // Split vendor chunks so the home-page TTI is not gated on libraries
        // only used by the settings page.
        manualChunks: {
          'vendor-vue': ['vue', 'vue-router', 'pinia', 'vue-i18n'],
          'vendor-ui': ['radix-vue', 'lucide-vue-next'],
          'vendor-misc': ['axios', 'dayjs']
        }
      }
    }
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true }
    }
  }
})
