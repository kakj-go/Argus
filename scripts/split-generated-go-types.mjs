import { access, readFile, unlink, writeFile } from "node:fs/promises";

const [input] = process.argv.slice(2);
if (!input) throw new Error("usage: split-generated-go-types.mjs <types.gen.go>");

const source = await readFile(input, "utf8");
const extraPath = input.replace(/types\.gen\.go$/, "types_extra.gen.go");
try {
  await access(extraPath);
  await unlink(extraPath);
} catch {}

if (source.split("\n").length <= 2000) process.exit(0);

const importEnd = source.indexOf(")\n\n");
if (importEnd < 0) throw new Error(`imports not found in ${input}`);
const prefix = source.slice(0, importEnd + 3);
const body = source.slice(importEnd + 3);
const declarations = [...body.matchAll(/^\/\/ .*\n(?=(?:const|type|func|var)\b)/gm)];
const split = declarations.find((match) => match.index > body.length / 2)?.index;
if (split == null) throw new Error(`unable to split generated types ${input}`);

const withUsedImports = (part) => {
  const match = prefix.match(/import \(([\s\S]*?)\n\)\n\n/);
  if (!match) return prefix + part.trimEnd() + "\n";
  const imports = match[1].split("\n").filter((line) => {
    const quoted = line.match(/\"([^\"]+)\"/);
    if (!quoted) return true;
    const pkg = quoted[1].split("/").at(-1);
    return part.includes(`${pkg}.`);
  }).join("\n");
  return prefix.replace(match[1], imports) + part.trimEnd() + "\n";
};
await writeFile(input, withUsedImports(body.slice(0, split)), "utf8");
await writeFile(extraPath, withUsedImports(body.slice(split)), "utf8");
