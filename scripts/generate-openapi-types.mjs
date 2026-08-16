import { readFile, writeFile } from "node:fs/promises";
import openapiTS, { UNKNOWN, astToString } from "openapi-typescript";

const [input, output] = process.argv.slice(2);
if (!input || !output) {
  throw new Error("usage: generate-openapi-types.mjs <input> <output>");
}

const source = await readFile(input);
const ast = await openapiTS(source, {
  transform(schema) {
    if (schema["x-argus-typescript-type"] === "unknown") {
      return UNKNOWN;
    }
    return undefined;
  },
});

await writeFile(output, astToString(ast), "utf8");
