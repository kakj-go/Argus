import fs from "node:fs";
import path from "node:path";
import ts from "typescript";

const apps = ["enterprise", "platform"];
const locales = ["Zh", "En"];

function listFiles(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const file = path.join(directory, entry.name);
    return entry.isDirectory() ? listFiles(file) : [file];
  });
}

function unwrap(node) {
  while (
    ts.isAsExpression(node) ||
    ts.isSatisfiesExpression(node) ||
    ts.isParenthesizedExpression(node)
  ) {
    node = node.expression;
  }
  return node;
}

function propertyName(node) {
  if (
    ts.isIdentifier(node) ||
    ts.isStringLiteral(node) ||
    ts.isNumericLiteral(node)
  ) {
    return node.text;
  }
  return null;
}

function flattenResource(rawNode, prefix, keys) {
  const node = unwrap(rawNode);
  if (!ts.isObjectLiteralExpression(node)) {
    if (prefix) keys.add(prefix);
    return;
  }
  for (const property of node.properties) {
    if (!ts.isPropertyAssignment(property)) continue;
    const name = propertyName(property.name);
    if (name === null) continue;
    const key = prefix ? `${prefix}.${name}` : name;
    const value = unwrap(property.initializer);
    if (ts.isObjectLiteralExpression(value)) {
      flattenResource(value, key, keys);
    } else {
      keys.add(key);
    }
  }
}

function resourceExports(i18nDirectory) {
  const result = { Zh: new Map(), En: new Map() };
  const files = listFiles(i18nDirectory).filter(
    (file) =>
      file.endsWith(".ts") &&
      !file.endsWith("index.ts") &&
      !file.endsWith(".test.ts"),
  );
  for (const file of files) {
    const source = ts.createSourceFile(
      file,
      fs.readFileSync(file, "utf8"),
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.TS,
    );
    for (const statement of source.statements) {
      if (!ts.isVariableStatement(statement)) continue;
      for (const declaration of statement.declarationList.declarations) {
        if (!ts.isIdentifier(declaration.name) || !declaration.initializer)
          continue;
        const match = declaration.name.text.match(/(Zh|En)$/);
        if (!match) continue;
        const keys = new Set();
        flattenResource(declaration.initializer, "", keys);
        result[match[1]].set(declaration.name.text, keys);
      }
    }
  }
  return result;
}

function registeredModules(indexFile) {
  const result = { Zh: new Set(), En: new Set() };
  const source = ts.createSourceFile(
    indexFile,
    fs.readFileSync(indexFile, "utf8"),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS,
  );
  for (const statement of source.statements) {
    if (!ts.isVariableStatement(statement)) continue;
    for (const declaration of statement.declarationList.declarations) {
      if (!ts.isIdentifier(declaration.name) || !declaration.initializer)
        continue;
      const match = declaration.name.text.match(/^modules(Zh|En)$/);
      const initializer = unwrap(declaration.initializer);
      if (!match || !ts.isArrayLiteralExpression(initializer)) continue;
      for (const element of initializer.elements) {
        if (ts.isIdentifier(element)) result[match[1]].add(element.text);
      }
    }
  }
  return result;
}

function translationReferences(sourceRoot) {
  const references = [];
  const files = listFiles(sourceRoot).filter(
    (file) =>
      /\.(ts|tsx)$/.test(file) &&
      !/\.test\.tsx?$/.test(file) &&
      !file.includes(`${path.sep}i18n${path.sep}`),
  );
  const addExpression = (rawExpression, file, source) => {
    const expression = unwrap(rawExpression);
    if (
      ts.isStringLiteral(expression) ||
      ts.isNoSubstitutionTemplateLiteral(expression)
    ) {
      const position = source.getLineAndCharacterOfPosition(
        expression.getStart(source),
      );
      references.push({
        type: "key",
        value: expression.text,
        file,
        line: position.line + 1,
      });
      return;
    }
    if (ts.isTemplateExpression(expression) && expression.head.text) {
      const position = source.getLineAndCharacterOfPosition(
        expression.getStart(source),
      );
      references.push({
        type: "prefix",
        value: expression.head.text,
        file,
        line: position.line + 1,
      });
      return;
    }
    if (ts.isConditionalExpression(expression)) {
      addExpression(expression.whenTrue, file, source);
      addExpression(expression.whenFalse, file, source);
    }
  };
  for (const file of files) {
    const source = ts.createSourceFile(
      file,
      fs.readFileSync(file, "utf8"),
      ts.ScriptTarget.Latest,
      true,
      file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );
    const visit = (node) => {
      if (ts.isCallExpression(node) && node.arguments.length > 0) {
        const callee = node.expression;
        const translationCall =
          (ts.isIdentifier(callee) && callee.text === "t") ||
          (ts.isPropertyAccessExpression(callee) && callee.name.text === "t");
        if (translationCall) addExpression(node.arguments[0], file, source);
      }
      ts.forEachChild(node, visit);
    };
    visit(source);
  }
  return [
    ...new Map(
      references.map((reference) => [
        `${reference.type}:${reference.value}:${reference.file}:${reference.line}`,
        reference,
      ]),
    ).values(),
  ];
}

const errors = [];
for (const app of apps) {
  const sourceRoot = path.join("web", "apps", app, "src");
  const i18nDirectory = path.join(sourceRoot, "i18n");
  const exportsByLocale = resourceExports(i18nDirectory);
  const registeredByLocale = registeredModules(
    path.join(i18nDirectory, "index.ts"),
  );
  const keysByLocale = { Zh: new Set(), En: new Set() };

  for (const locale of locales) {
    for (const [name, keys] of exportsByLocale[locale]) {
      if (!registeredByLocale[locale].has(name)) {
        errors.push(`${app}: ${name} is not registered in i18n/index.ts`);
      }
      for (const key of keys) keysByLocale[locale].add(key);
    }
  }

  for (const reference of translationReferences(sourceRoot)) {
    for (const locale of locales) {
      const keys = keysByLocale[locale];
      const exists =
        reference.type === "key"
          ? keys.has(reference.value)
          : [...keys].some((key) => key.startsWith(reference.value));
      if (!exists) {
        errors.push(
          `${reference.file}:${reference.line} missing ${locale} ${reference.type} ${reference.value}`,
        );
      }
    }
  }
}

if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log("i18n registration and source key coverage passed");
}
