import { readFile, writeFile } from "node:fs/promises";

const [path] = process.argv.slice(2);
if (!path) {
  throw new Error("usage: minify-json.mjs <path>");
}

const value = JSON.parse(await readFile(path, "utf8"));

// Bundled JSON Schema documents live below the OpenAPI document's base URI.
// Keeping their standalone $id values would rebase bundled local $refs back to
// the source document and make otherwise valid component references ambiguous.
const schemas = value?.components?.schemas;
if (schemas && typeof schemas === "object") {
  for (const schema of Object.values(schemas)) {
    if (schema && typeof schema === "object") {
      delete schema.$id;
      delete schema.$schema;
    }
  }
}
await writeFile(path, `${JSON.stringify(value)}\n`, "utf8");
