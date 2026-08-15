import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  server: { port: 4174, strictPort: true },
  preview: { port: 4174, strictPort: true },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) return;
          if (id.includes("react")) return "vendor-react";
          if (id.includes("@tanstack")) return "vendor-tanstack";
          if (id.includes("@radix-ui")) return "vendor-radix";
          if (id.includes("lucide-react")) return "vendor-icons";
          return "vendor";
        },
      },
    },
  },
});
