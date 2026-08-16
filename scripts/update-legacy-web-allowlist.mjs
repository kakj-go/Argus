import { createHash } from "node:crypto";
import { readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const root = process.cwd();
const webRoot = path.join(root, "web");
const outputPath = path.join(root, "api/contracts/legacy-web-allowlist.json");
const legacyPattern = /(project_id|ProjectId|EnterpriseMembership|memberships|scopeType.*project|projectIds|listProjects|createProject|updateProject|resourceGroupIds|ownerTeamId|\btags\b|PendingAction\.params|params: Record<string, unknown>)/i;
const counts = new Map();

async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  for (const entry of entries) {
    if (entry.name === "node_modules" || entry.name === "dist") continue;
    const absolute = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      await walk(absolute);
      continue;
    }
    const relative = path.relative(root, absolute).split(path.sep).join("/");
    const contents = await readFile(absolute, "utf8");
    for (const line of contents.split("\n")) {
      if (!legacyPattern.test(line)) continue;
      const fingerprint = createHash("sha256").update(line.trim()).digest("hex");
      const key = `${relative}\0${fingerprint}`;
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
  }
}

await walk(webRoot);
const entries = [...counts.entries()]
  .map(([key, count]) => {
    const [entryPath, fingerprint] = key.split("\0");
    return { path: entryPath, fingerprint, count };
  })
  .sort((left, right) => left.path.localeCompare(right.path) || left.fingerprint.localeCompare(right.fingerprint));

await writeFile(outputPath, `${JSON.stringify({ version: "argus.legacy_web_allowlist/v1", entries }, null, 2)}\n`);
