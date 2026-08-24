import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import ts from "typescript";

const appRoots = [
  "web/apps/enterprise/src",
  "web/apps/platform/src",
];
const styleRoots = [...appRoots, "web/packages/ui/src"];
const cssFiles = files(styleRoots, ["*.css"]);
const sourceFiles = files(appRoots, ["*.ts", "*.tsx"]);
const failures = [];
const allowedClass = /^(?:argus-|is-|active$|dark$|light$)/;
const governedProperties = /^(?:font|font-size|gap|row-gap|column-gap|padding(?:-[a-z]+)?|margin(?:-[a-z]+)?|border-radius)$/;

function files(roots, globs) {
  const args = ["--files", ...roots];
  for (const glob of globs) args.push("-g", glob);
  const output = execFileSync("rg", args, { encoding: "utf8" }).trim();
  return output ? output.split("\n") : [];
}

function lineOf(source, offset) {
  return source.slice(0, offset).split("\n").length;
}

for (const file of cssFiles) {
  const source = readFileSync(file, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
  if (source.includes("argus-var(")) {
    failures.push(`${file}: malformed CSS custom property reference`);
  }
  for (const match of source.matchAll(/\.([A-Za-z_][A-Za-z0-9_-]*)/g)) {
    if (file.includes("/apps/") && !allowedClass.test(match[1])) {
      failures.push(`${file}:${lineOf(source, match.index)} class .${match[1]} must use .argus-*`);
    }
  }
  for (const match of source.matchAll(/#[0-9a-fA-F]{3,8}|(?:rgb|hsl)a?\(|(?<![-\w])(?:white|black)(?![-\w])/g)) {
    failures.push(`${file}:${lineOf(source, match.index)} hard-coded color ${match[0]}`);
  }
  for (const match of source.matchAll(/([a-z-]+)\s*:\s*([^;{}]+)/g)) {
    if (!governedProperties.test(match[1])) continue;
    if (/-?(?:\d+\.?\d*|\.\d+)(?:px|rem|em)/.test(match[2])) {
      failures.push(`${file}:${lineOf(source, match.index)} ${match[1]} must use a design token`);
    }
    if (match[1] === "border-radius" && /\d+%/.test(match[2])) {
      failures.push(`${file}:${lineOf(source, match.index)} border-radius must use a design token`);
    }
  }
}

function checkLiteral(file, source, node) {
  const text = node.text ?? "";
  for (const token of text.split(/\s+/).filter(Boolean)) {
    if (/^[A-Za-z_][A-Za-z0-9_-]*$/.test(token) && !allowedClass.test(token)) {
      failures.push(`${file}:${lineOf(source, node.getStart())} class ${token} must use argus-*`);
    }
  }
}

for (const file of sourceFiles) {
  const source = readFileSync(file, "utf8");
  const sourceFile = ts.createSourceFile(
    file,
    source,
    ts.ScriptTarget.Latest,
    true,
    file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  function inspectClassName(node) {
    if (
      ts.isStringLiteral(node) ||
      ts.isNoSubstitutionTemplateLiteral(node)
    ) {
      checkLiteral(file, source, node);
      return;
    }
    if (ts.isJsxExpression(node) && node.expression) {
      const expression = node.expression;
      if (
        ts.isStringLiteral(expression) ||
        ts.isNoSubstitutionTemplateLiteral(expression)
      ) {
        checkLiteral(file, source, expression);
      } else if (ts.isTemplateExpression(expression)) {
        checkLiteral(file, source, expression.head);
        for (const span of expression.templateSpans) {
          checkLiteral(file, source, span.literal);
        }
      }
    }
  }
  function visit(node) {
    if (
      ts.isJsxAttribute(node) &&
      node.name.getText(sourceFile) === "className" &&
      node.initializer
    ) {
      inspectClassName(node.initializer);
    }
    ts.forEachChild(node, visit);
  }
  visit(sourceFile);
}

if (failures.length > 0) {
  throw new Error(`Web style checks failed:\n${failures.join("\n")}`);
}

console.log("Web style checks passed");
