import { defineConfig } from 'vite';

export default defineConfig({
  // WebLLM ships large WASM/WebGPU shaders — keep them out of inlining.
  build: {
    target: 'es2022',
    sourcemap: true,
  },
  optimizeDeps: {
    // @mlc-ai/web-llm pulls in heavy shaders; let Vite serve them as-is.
    exclude: ['@mlc-ai/web-llm'],
  },
  server: {
    headers: {
      // Required for WebLLM shared-array-buffer / cross-origin isolation in
      // some browsers — harmless to set unconditionally.
      'Cross-Origin-Opener-Policy': 'same-origin',
      'Cross-Origin-Embedder-Policy': 'require-corp',
    },
  },
});
