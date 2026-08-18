import { defineConfig } from "vite";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const axeSource = readFileSync(require.resolve("axe-core/axe.min.js"), "utf8");
const axeSplit = Math.ceil(axeSource.length / 2);

function splitAxeCore() {
  const modules = new Map([
    ["virtual:argus-axe-part-1", axeSource.slice(0, axeSplit)],
    ["virtual:argus-axe-part-2", axeSource.slice(axeSplit)],
  ]);
  return {
    name: "argus-split-axe-core",
    resolveId(id: string) {
      return modules.has(id) ? `\0${id}` : undefined;
    },
    load(id: string) {
      const source = modules.get(id.replace(/^\0/, ""));
      return source === undefined ? undefined : `export default ${JSON.stringify(source)};`;
    },
  };
}

export default defineConfig({
  plugins: [splitAxeCore()],
  server: { port: 4176, strictPort: true },
  preview: { port: 4176, strictPort: true },
  build: { manifest: true },
});
