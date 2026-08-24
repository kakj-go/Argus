import { readdir, readFile } from "node:fs/promises";
import { extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const fieldControls = new Set([
  "Input",
  "Textarea",
  "Select",
  "Switch",
  "OptionChecklist",
  "input",
  "textarea",
  "select",
]);
const formTags = new Set(["FormDrawer", "form"]);
const writeMethodPattern =
  /^(add|apply|approve|assign|cancel|change|complete|create|delete|disable|enable|enroll|execute|install|invite|regenerate|reject|remove|reset|restore|revoke|rotate|run|save|set|stepUp|submit|uninstall|update|verify)/;

async function collect(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...(await collect(path)));
    else if ([".ts", ".tsx"].includes(extname(entry.name))) files.push(path);
  }
  return files;
}

function tagName(node) {
  return ts.isIdentifier(node) ? node.text : "";
}

function unwrap(node) {
  while (
    ts.isAsExpression(node) ||
    ts.isSatisfiesExpression(node) ||
    ts.isParenthesizedExpression(node) ||
    ts.isNonNullExpression(node)
  ) {
    node = node.expression;
  }
  return node;
}

function attribute(opening, name) {
  return opening.attributes.properties.find(
    (property) => ts.isJsxAttribute(property) && property.name.text === name,
  );
}

function attributeExpression(opening, name) {
  const initializer = attribute(opening, name)?.initializer;
  return initializer && ts.isJsxExpression(initializer)
    ? initializer.expression
    : undefined;
}

function staticString(attributeNode) {
  if (!attributeNode?.initializer) return undefined;
  if (ts.isStringLiteral(attributeNode.initializer)) {
    return attributeNode.initializer.text;
  }
  const expression = attributeNode.initializer.expression;
  return expression && ts.isStringLiteral(expression)
    ? expression.text
    : undefined;
}

function hasTruthyAttribute(opening, name) {
  const value = attribute(opening, name);
  if (!value) return false;
  if (!value.initializer) return true;
  if (!ts.isJsxExpression(value.initializer)) return false;
  return value.initializer.expression?.kind === ts.SyntaxKind.TrueKeyword;
}

function isHandleSubmitCall(rawExpression) {
  const expression = unwrap(rawExpression);
  if (!ts.isCallExpression(expression)) return false;
  const callee = unwrap(expression.expression);
  return (
    (ts.isIdentifier(callee) && callee.text === "handleSubmit") ||
    (ts.isPropertyAccessExpression(callee) &&
      callee.name.text === "handleSubmit")
  );
}

function isEditableField(node) {
  return (
    ts.isJsxElement(node) &&
    tagName(node.openingElement.tagName) === "Field" &&
    staticString(attribute(node.openingElement, "requirement")) !== "none"
  );
}

function containsEditableField(node) {
  let found = false;
  function visit(child) {
    if (found) return;
    if (child !== node && isEditableField(child)) {
      found = true;
      return;
    }
    ts.forEachChild(child, visit);
  }
  visit(node);
  return found;
}

function branchWithin(node, conditional) {
  if (
    node.pos >= conditional.whenTrue.pos &&
    node.end <= conditional.whenTrue.end
  ) {
    return "true";
  }
  if (
    node.pos >= conditional.whenFalse.pos &&
    node.end <= conditional.whenFalse.end
  ) {
    return "false";
  }
  return "condition";
}

function sameConditionalBranch(left, right) {
  const leftBranches = new Map();
  for (let node = left.parent; node; node = node.parent) {
    if (ts.isConditionalExpression(node)) {
      leftBranches.set(node, branchWithin(left, node));
    }
  }
  for (let node = right.parent; node; node = node.parent) {
    if (!ts.isConditionalExpression(node) || !leftBranches.has(node)) continue;
    if (leftBranches.get(node) !== branchWithin(right, node)) return false;
  }
  return true;
}

function hasNearbyEditableField(button) {
  let jsxDepth = 0;
  for (let ancestor = button.parent; ancestor; ancestor = ancestor.parent) {
    if (!ts.isJsxElement(ancestor)) continue;
    jsxDepth += 1;
    if (jsxDepth > 2) return false;
    if (formTags.has(tagName(ancestor.openingElement.tagName))) return false;
    let found = false;
    function visit(node) {
      if (found) return;
      if (node !== ancestor && ts.isJsxElement(node)) {
        if (formTags.has(tagName(node.openingElement.tagName))) return;
        if (isEditableField(node) && sameConditionalBranch(button, node)) {
          found = true;
          return;
        }
      }
      ts.forEachChild(node, visit);
    }
    visit(ancestor);
    if (found) return true;
  }
  return false;
}

function rootIdentifier(expression) {
  let current = unwrap(expression);
  while (ts.isPropertyAccessExpression(current)) current = current.expression;
  return ts.isIdentifier(current) ? current.text : undefined;
}

function containsDirectWrite(node) {
  let found = false;
  function visit(child) {
    if (found) return;
    if (ts.isCallExpression(child)) {
      const callee = unwrap(child.expression);
      if (
        ts.isPropertyAccessExpression(callee) &&
        (callee.name.text === "mutate" || callee.name.text === "mutateAsync")
      ) {
        found = true;
        return;
      }
      if (
        ts.isPropertyAccessExpression(callee) &&
        rootIdentifier(callee) === "api" &&
        writeMethodPattern.test(callee.name.text)
      ) {
        found = true;
        return;
      }
    }
    ts.forEachChild(child, visit);
  }
  visit(node);
  return found;
}

function containsUnsafeHandlerCall(expression, unsafeWriteBindings) {
  let found = false;
  function visit(node) {
    if (found) return;
    if (isHandleSubmitCall(node)) {
      found = true;
      return;
    }
    if (ts.isCallExpression(node)) {
      const callee = unwrap(node.expression);
      if (
        ts.isPropertyAccessExpression(callee) &&
        (callee.name.text === "mutate" || callee.name.text === "mutateAsync")
      ) {
        found = true;
        return;
      }
      if (ts.isIdentifier(callee) && unsafeWriteBindings.has(callee.text)) {
        found = true;
        return;
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(expression);
  return found;
}

export function checkFormSemanticsSource(source, displayFile = "fixture.tsx") {
  const sourceFile = ts.createSourceFile(
    displayFile,
    source,
    ts.ScriptTarget.Latest,
    true,
    displayFile.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const failures = [];
  const validatedSubmitBindings = new Set();
  const unsafeWriteBindings = new Set();

  function collectBindings(node) {
    if (
      ts.isVariableDeclaration(node) &&
      ts.isIdentifier(node.name) &&
      node.initializer
    ) {
      if (isHandleSubmitCall(node.initializer)) {
        validatedSubmitBindings.add(node.name.text);
      } else if (
        (ts.isArrowFunction(node.initializer) ||
          ts.isFunctionExpression(node.initializer)) &&
        containsDirectWrite(node.initializer.body)
      ) {
        unsafeWriteBindings.add(node.name.text);
      }
    }
    if (ts.isFunctionDeclaration(node) && node.name && node.body) {
      if (containsDirectWrite(node.body))
        unsafeWriteBindings.add(node.name.text);
    }
    ts.forEachChild(node, collectBindings);
  }
  collectBindings(sourceFile);

  function fail(node, message) {
    const { line, character } = sourceFile.getLineAndCharacterOfPosition(
      node.getStart(sourceFile),
    );
    failures.push(`${displayFile}:${line + 1}:${character + 1} ${message}`);
  }

  function inspectField(node) {
    const opening = node.openingElement;
    const requirement = attribute(opening, "requirement");
    if (!requirement)
      fail(opening, "Field must declare requirement explicitly");
    const label = attribute(opening, "label");
    if (label?.getText(sourceFile).includes("*")) {
      fail(
        label,
        "Field label must not contain a hand-written required marker",
      );
    }

    const controls = [];
    function collectControls(child) {
      if (
        child !== node &&
        (ts.isJsxElement(child) || ts.isJsxSelfClosingElement(child))
      ) {
        const childOpening = ts.isJsxElement(child)
          ? child.openingElement
          : child;
        const name = tagName(childOpening.tagName);
        if (name === "Field") return;
        if (fieldControls.has(name)) controls.push(childOpening);
      }
      ts.forEachChild(child, collectControls);
    }
    collectControls(node);

    if (
      controls.length > 1 &&
      staticString(attribute(opening, "controlMode")) !== "group"
    ) {
      fail(
        opening,
        'Field with multiple controls must use controlMode="group"',
      );
    }
    if (staticString(requirement) === "none") {
      for (const control of controls) {
        if (
          !hasTruthyAttribute(control, "readOnly") &&
          !hasTruthyAttribute(control, "disabled")
        ) {
          fail(control, 'Editable controls cannot use requirement="none"');
        }
      }
    }
  }

  function visit(node) {
    if (
      ts.isJsxElement(node) &&
      tagName(node.openingElement.tagName) === "Field"
    ) {
      inspectField(node);
    }
    if (ts.isJsxElement(node)) {
      const opening = node.openingElement;
      const name = tagName(opening.tagName);
      if (formTags.has(name) && containsEditableField(node)) {
        const onSubmit = attribute(opening, "onSubmit");
        const expression = attributeExpression(opening, "onSubmit");
        const unwrapped = expression && unwrap(expression);
        const validated =
          expression &&
          (isHandleSubmitCall(expression) ||
            (ts.isIdentifier(unwrapped) &&
              validatedSubmitBindings.has(unwrapped.text)));
        const isDemoOnlyDrawer =
          displayFile.endsWith("/pages/demo-page.tsx") && name === "FormDrawer";
        if (!onSubmit && !isDemoOnlyDrawer) {
          fail(opening, "Editable form must declare an onSubmit handler");
        } else if (onSubmit && !validated && !isDemoOnlyDrawer) {
          fail(onSubmit, "Form submission must pass through handleSubmit/Zod");
        }
      }
      if (name === "Button") {
        const onClick = attribute(opening, "onClick");
        const expression = attributeExpression(opening, "onClick");
        if (
          onClick &&
          expression &&
          hasNearbyEditableField(node) &&
          containsUnsafeHandlerCall(expression, unsafeWriteBindings)
        ) {
          fail(
            onClick,
            "Editable form actions must submit through a validated form, not Button onClick",
          );
        }
      }
    }
    if (
      ts.isPropertyAssignment(node) &&
      ((ts.isIdentifier(node.name) && node.name.text === "label") ||
        (ts.isStringLiteral(node.name) && node.name.text === "label")) &&
      ts.isStringLiteralLike(node.initializer) &&
      node.initializer.text.includes("*")
    ) {
      fail(
        node.initializer,
        "Translation labels must not contain a hand-written *",
      );
    }
    ts.forEachChild(node, visit);
  }
  visit(sourceFile);
  return failures;
}

async function main() {
  const root = process.cwd();
  const sourceRoot = join(root, "web", "apps");
  const failures = [];
  for (const file of await collect(sourceRoot)) {
    const source = await readFile(file, "utf8");
    const displayFile = relative(root, file).replaceAll("\\", "/");
    failures.push(...checkFormSemanticsSource(source, displayFile));
  }
  if (failures.length > 0) {
    throw new Error(`Form semantics checks failed:\n${failures.join("\n")}`);
  }
  console.log("Form semantics checks passed");
}

if (resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url)) {
  await main();
}
