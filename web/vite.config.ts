import path from 'path';
import { defineConfig } from 'vite';
import { tanstackStart } from '@tanstack/react-start/plugin/vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// https://tanstack.com/start/latest/docs/framework/react/build-from-scratch
export default defineConfig({
  plugins: [
    // TanStack Start must come before React. SPA mode keeps today's client-only behaviour: auth and data load in the
    // browser, against the Go control plane.
    tanstackStart({ spa: { enabled: true } }),
    // The React Compiler runs through oxc (Rust), not Babel.
    react({ compiler: true }),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        // Keep the browser's Host so the backend's same-host WebSocket Origin check passes.
        changeOrigin: false,
        ws: true,
      },
    },
  },
});
