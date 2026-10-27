import { defineConfig } from "mygo-cli";
import pkg from "./package.json" with { type: "json" };

export default defineConfig({
  name: "OpenSynapse",
  identifier: "dev.opensynapse.app",
  // The release workflow checks that the tag is v + this version.
  version: pkg.version,
  // `mygo dev` runs devCommand and loads devUrl; `mygo build` runs
  // buildCommand and embeds frontendDist into the app.
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
  // Releases are signed with the secret key (MYGO_UPDATER_PRIVATE_KEY in CI);
  // installed and portable apps accept only updates signed with it.
  updates: {
    publicKey: "Q0R/YXaA3M/5FjJeBZ515qxt8be96wjlybASDEP4KjI=",
    github: "JohnsonRan/OpenSynapse",
  },
});
