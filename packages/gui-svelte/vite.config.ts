import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import path from "path";

export default defineConfig({
  plugins: [svelte()] as unknown as import("vite").PluginOption[],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5174,
    proxy: {
      "/api": {
        target: "http://localhost:8090",
        changeOrigin: true,
      },
      "/ws": {
        target: "ws://localhost:8090",
        ws: true,
        changeOrigin: true,
      },
    },
  },
});
