import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    alias: {
      "@decky/api": new URL("./src/test/decky-api.ts", import.meta.url).pathname,
    },
  },
});
