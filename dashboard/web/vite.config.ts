import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

// Dev port contract:
//   Backend (Track A server): http://localhost:8787
//   Vite dev server:          http://localhost:5173
// The /api proxy forwards all /api/* requests from the frontend to the backend.
// In production this co-location assumption doesn't apply; serve via a reverse proxy.
const BACKEND_PORT = 8787;

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@agent-teams/shared": path.resolve(__dirname, "../shared/index.ts"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: `http://localhost:${BACKEND_PORT}`,
        changeOrigin: true,
      },
    },
  },
});
