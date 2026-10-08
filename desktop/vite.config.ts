import { fileURLToPath, URL } from "node:url"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

export default defineConfig({
  root: fileURLToPath(new URL("./ui", import.meta.url)),
  base: "./",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./ui", import.meta.url)) },
    dedupe: ["react", "react-dom"],
  },
  // Prebundle the complete shared React graph in one epoch. With strict CSP
  // and HMR disabled, late-discovered dependencies cannot auto-reload a tab.
  optimizeDeps: { include: ["react", "react-dom", "react-dom/client", "react/jsx-runtime", "react/jsx-dev-runtime", "radix-ui", "lucide-react", "class-variance-authority", "clsx", "tailwind-merge"] },
  clearScreen: false,
  // Tauri keeps script-src strict. Avoid React's inline refresh preamble;
  // local development uses an ordinary page reload after source changes.
  server: { host: "127.0.0.1", port: 1420, strictPort: true, hmr: false },
  build: {
    outDir: "../dist",
    emptyOutDir: true,
    target: "es2022",
  },
})
