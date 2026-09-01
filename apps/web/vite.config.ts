import path from "node:path"
import react from "@vitejs/plugin-react"
import { defineConfig, loadEnv } from "vite"

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "")
  const apiTarget = env.VITE_API_TARGET || "http://localhost:8080"

  return {
    plugins: [react()],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    server: {
      port: 5190,
      strictPort: true,
      proxy: {
        "/api": apiTarget,
        "/healthz": apiTarget,
      },
    },
    build: {
      rolldownOptions: {
        output: {
          codeSplitting: {
            groups: [
              {
                name: "prosemirror",
                test: /node_modules[\\/]prosemirror-/,
                priority: 30,
              },
              {
                name: "tiptap",
                test: /node_modules[\\/]@tiptap/,
                priority: 20,
              },
            ],
          },
        },
      },
    },
  }
})
