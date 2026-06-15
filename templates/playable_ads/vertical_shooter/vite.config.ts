import { defineConfig } from "vite";

export default defineConfig({
  base: "./",
  build: {
    emptyOutDir: true,
    target: "es2019",
    assetsInlineLimit: 1024 * 1024,
  },
});
