import { readFile, writeFile } from "node:fs/promises";

const [path] = process.argv.slice(2);
if (!path) {
  throw new Error("usage: minify-json.mjs <path>");
}

const value = JSON.parse(await readFile(path, "utf8"));
await writeFile(path, `${JSON.stringify(value)}\n`, "utf8");
