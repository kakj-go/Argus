import { readFile, stat } from "node:fs/promises";
import path from "node:path";

const root = process.cwd();
const kib = 1024;
const apps = {
  platform: 1024 * kib,
  enterprise: 1024 * kib,
  "card-runtime": 650 * kib,
};
const maxChunk = 500 * kib;
const failures = [];

for (const [app, initialBudget] of Object.entries(apps)) {
  const dist = path.join(root, "web/apps", app, "dist");
  const html = await readFile(path.join(dist, "index.html"), "utf8");
  const assetPaths = [...html.matchAll(/(?:src|href)="(\/assets\/[^\"]+\.js)"/g)]
    .map((match) => match[1])
    .filter(Boolean);
  let initialBytes = 0;
  for (const asset of new Set(assetPaths)) {
    initialBytes += (await stat(path.join(dist, asset.slice(1)))).size;
  }
  if (initialBytes > initialBudget) {
    failures.push(`${app} initial JS ${initialBytes} > ${initialBudget}`);
  }
  const manifest = JSON.parse(await readFile(path.join(dist, ".vite/manifest.json"), "utf8"));
  for (const entry of Object.values(manifest)) {
    if (!entry.file?.endsWith(".js")) continue;
    const bytes = (await stat(path.join(dist, entry.file))).size;
    if (bytes > maxChunk) failures.push(`${app}/${entry.file} ${bytes} > ${maxChunk}`);
  }
}

if (failures.length) {
  throw new Error(`Bundle budget exceeded:\n${failures.join("\n")}`);
}
console.log("Bundle budgets passed");
