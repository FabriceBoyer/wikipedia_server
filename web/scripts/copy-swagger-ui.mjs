// Copies the handful of swagger-ui-dist files our public/swagger/index.html
// needs into public/swagger/, so `vite build` picks them up as static
// assets. Runs offline against the local node_modules copy - no CDN, no
// network access needed at build or run time.
import { copyFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, "..", "node_modules", "swagger-ui-dist");
const dest = join(here, "..", "public", "swagger");

mkdirSync(dest, { recursive: true });

for (const file of [
  "swagger-ui-bundle.js",
  "swagger-ui-standalone-preset.js",
  "swagger-ui.css",
  "favicon-32x32.png",
  "favicon-16x16.png",
]) {
  copyFileSync(join(src, file), join(dest, file));
}
