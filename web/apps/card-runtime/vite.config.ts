import { defineConfig } from "vite";

export default defineConfig({
  server: { port: 4176, strictPort: true },
  preview: { port: 4176, strictPort: true },
  build: { manifest: true },
});
