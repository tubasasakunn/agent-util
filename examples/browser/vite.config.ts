import { defineConfig } from 'vite';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  build: {
    target: 'es2022',
    sourcemap: true,
  },
  resolve: {
    // demo の node_modules にある WebLLM を SDK の optional peer dep として解決させる。
    // これがないと Vite は SDK package の peerDependenciesMeta.optional を見て空スタブを返す。
    alias: {
      '@mlc-ai/web-llm': resolve(here, 'node_modules/@mlc-ai/web-llm/lib/index.js'),
    },
  },
  optimizeDeps: {
    include: ['@mlc-ai/web-llm'],
  },
  server: {
    headers: {
      'Cross-Origin-Opener-Policy': 'same-origin',
      'Cross-Origin-Embedder-Policy': 'require-corp',
    },
  },
});
