import { defineConfig } from "vitest/config";
import path from "path";

export default defineConfig({
  resolve: {
    alias: {
      "@agent-teams/shared": path.resolve(__dirname, "../shared/index.ts"),
    },
  },
  test: {
    environment: "jsdom",
  },
});
