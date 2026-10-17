import { defineConfig } from "void/config";

// Static deploy of dist/ to Cloudflare Workers Assets: `pnpm void deploy`.
export default defineConfig({
  cloudflare: { name: "kintsugi" },
});
