import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  output: 'static',
  integrations: [react()],
  vite: {
    plugins: [tailwindcss()],
    build: {
      rollupOptions: {
        output: {
          // P9: Separate vendor chunks so app changes don't invalidate framework cache
          manualChunks(id) {
            if (id.includes('node_modules/react')) return 'react-vendor';
            if (id.includes('node_modules/@codemirror') || id.includes('node_modules/codemirror')) return 'editor-vendor';
            if (id.includes('node_modules/@xterm')) return 'terminal-vendor';
          },
        },
      },
    },
  },
});
