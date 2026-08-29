import { access, readFile, unlink, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

const [input] = process.argv.slice(2);
if (!input) throw new Error("usage: split-generated-openapi-types.mjs <types.ts>");

const source = await readFile(input, "utf8");
const extraName = basename(input, ".ts") + "_operations.ts";
const extraPath = join(dirname(input), extraName);
try {
  await access(extraPath);
  await unlink(extraPath);
} catch {}

if (source.split("\n").length <= 2000) process.exit(0);

const marker = "export interface operations {";
const split = source.indexOf(marker);
if (split < 0) throw new Error(`unable to split generated TypeScript ${input}`);

const importLine = `import type { components } from "./${basename(input, ".ts")}.js";\n\n`;
const operationsBody = source.slice(split);
const mainBody = source.slice(0, split);
const localImport = `import type { operations } from "./${extraName.replace(/\.ts$/, ".js")}";\nexport type { operations };\n\n`;
const imports = (source.match(/^import[\s\S]*?\n\n/) || [""])[0];
const mainImports = imports.split("\n").filter((line) => !line.includes("operations")).join("\n");
await writeFile(input, localImport + mainImports + mainBody.slice(imports.length).trimEnd() + "\n", "utf8");
await writeFile(extraPath, importLine + operationsBody.trimEnd() + "\n", "utf8");
