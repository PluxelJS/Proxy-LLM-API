import { defineConfig } from "vite";
export default defineConfig({
  build: { outDir: "../internal/web/dist", emptyOutDir: true },
  server: { proxy: { "/api": "http://127.0.0.1:8318" } },
});
