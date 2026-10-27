import { defineConfig } from "vite";

// The page lives in web/; `mygo build` embeds what this writes to dist/.
export default defineConfig({
  root: __dirname,
  build: { outDir: "../dist", emptyOutDir: true },
  server: {
    // The app loads this address during `mygo dev` (devUrl in mygo.config.ts).
    port: 5173,
    strictPort: true,
  },
});
