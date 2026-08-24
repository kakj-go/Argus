import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

const apps = ["enterprise", "platform", "card-runtime"];
const forbidden = [
  "argus-mock:",
  "createMockApiClient",
  "host-cache-bj-01",
  "payment-worker",
  "sre-schedule",
];
const failures = [];

for (const app of apps) {
  const output = execFileSync(
    "rg",
    ["--files", `web/apps/${app}/dist`, "-g", "*.js"],
    { encoding: "utf8" },
  ).trim();
  const files = output ? output.split("\n") : [];
  for (const file of files) {
    const source = readFileSync(file, "utf8");
    for (const marker of forbidden) {
      if (source.includes(marker)) {
        failures.push(`${app}: ${marker} found in ${file}`);
      }
    }
  }
}

if (failures.length > 0) {
  throw new Error(`Real frontend build contains mock data:\n${failures.join("\n")}`);
}

console.log("Real frontend bundles contain no mock seed markers");
