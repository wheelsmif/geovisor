// Round-trip driver: extract a fixture, or apply compiled TIR against it.
//
//   node client/test/apply-roundtrip.mjs extract <fixture.html>
//   node client/test/apply-roundtrip.mjs execute <fixture.html> <tir.json>
//
// Execute mode builds the DOM with jsdom, points the Node global `document` at
// it, and runs client/src/apply-runtime.ts so locators resolve in the same
// realm as the extractor. Both modes write a single JSON document to stdout.

import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

import { JSDOM, VirtualConsole } from "jsdom";

import {
  isFrame,
  loadEsbuildModule,
  messageOf,
  repositoryRoot,
  upgradeDeclarativeShadows,
  visitTree,
} from "./helpers.mjs";

const FIXTURE_URL = "https://roundtrip.example/";
const STRING_INPUT = "roundtrip-fill";

const [mode, fixturePath, tirPath] = process.argv.slice(2);
if (mode !== "extract" && mode !== "execute") {
  throw new Error("usage: apply-roundtrip.mjs <extract|execute> <fixture.html> [tir.json]");
}
if (!fixturePath) {
  throw new Error("a fixture path is required");
}
if (mode === "execute" && !tirPath) {
  throw new Error("execute mode requires the compiled TIR path");
}

const html = await readFile(resolve(repositoryRoot, fixturePath), "utf8");

function buildDOM() {
  // jsdom reports unimplemented navigation and form submission as jsdomError.
  // The driver cancels both (see the capturing click listener), so the channel
  // is silenced rather than allowed to pollute stdout.
  const virtualConsole = new VirtualConsole();
  virtualConsole.on("jsdomError", () => {});
  const dom = new JSDOM(html, {
    runScripts: "outside-only",
    url: FIXTURE_URL,
    virtualConsole,
  });
  upgradeDeclarativeShadows(dom.window.document);
  populateSrcdocFrames(dom.window.document);
  return dom;
}

function frameContentDocument(frame) {
  try {
    return frame.contentDocument;
  } catch {
    return null;
  }
}

// jsdom creates a contentDocument for an iframe but never parses `srcdoc`, so a
// fixture's declared frame content is written in explicitly. Real browsers do
// this themselves. Frames nested inside srcdoc content are handled by repeating
// until no unpopulated frame remains, bounded so a malformed fixture cannot spin.
function populateSrcdocFrames(document, depth = 4) {
  if (depth <= 0) return;
  let populated = false;
  visitTree(document, (element) => {
    if (element.localName !== "iframe" || !element.hasAttribute("srcdoc")) return;
    const inner = frameContentDocument(element);
    if (!inner || inner.body?.hasChildNodes()) return;
    inner.body.innerHTML = element.getAttribute("srcdoc");
    populated = true;
    populateSrcdocFrames(inner, depth - 1);
  });
  if (populated) populateSrcdocFrames(document, depth - 1);
}

// Element indexes span frame documents, because a frame locator is only correct
// if it reaches an element *inside the right frame*. Frames are traversed in the
// order the runtime numbers them so an index is stable and meaningful.
function collectElements(document) {
  const elements = [];
  const visitRoot = (root) => {
    visitTree(root, (element) => {
      elements.push(element);
      if (!isFrame(element)) return;
      const inner = frameContentDocument(element);
      if (inner?.documentElement) visitRoot(inner);
    });
  };
  visitRoot(document);
  return elements;
}

// An event raised inside a frame does not propagate to the parent document, so
// every frame document needs its own listeners for the driver to see what a
// frame-scoped action resolved to.
function collectDocuments(root) {
  const documents = [root];
  visitTree(root, (element) => {
    if (!isFrame(element)) return;
    const inner = frameContentDocument(element);
    if (inner?.documentElement) documents.push(...collectDocuments(inner));
  });
  return documents;
}

if (mode === "extract") {
  const bundle = await readFile(
    resolve(repositoryRoot, "internal", "payload", "extractor.js"),
    "utf8",
  );
  const dom = buildDOM();
  const batches = await extractFrameTree(dom.window.document, bundle);
  process.stdout.write(`${JSON.stringify({ batches })}\n`);
} else {
  process.stdout.write(`${JSON.stringify(await executeTIR())}\n`);
}

function collectFrames(root) {
  const frames = [];
  visitTree(root, (element) => {
    if (isFrame(element)) frames.push(element);
  });
  return frames;
}

function stampFramePath(batch, indexes) {
  const refs = indexes.map((index) => ({ index }));
  const nodes = indexes.map((index) => ({ semantic: { role: "iframe", nth: index } }));
  for (const interaction of batch.interactions ?? []) {
    interaction.framePath = refs;
    for (const locator of interaction.locators ?? []) {
      locator.framePath = nodes;
    }
  }
  for (const warning of batch.warnings ?? []) {
    if (!warning.framePath || warning.framePath.length === 0) {
      warning.framePath = refs;
    }
  }
}

// Frame-local extraction plus the same path stamping the browser source applies
// in augmentBatch / frameTraversalNodes: role iframe, addressed by ordinal.
async function extractFrameTree(document, bundle) {
  const batches = [];
  const extractWindow = async (win) => {
    if (typeof win.__GEOVISOR_EXTRACT__ !== "function") {
      win.eval(bundle);
    }
    return win.__GEOVISOR_EXTRACT__({ safeExplore: false });
  };
  const walk = async (doc, indexes) => {
    if (!doc?.defaultView) return;
    const batch = await extractWindow(doc.defaultView);
    stampFramePath(batch, indexes);
    batches.push(batch);
    const frames = collectFrames(doc);
    for (let index = 0; index < frames.length; index += 1) {
      const inner = frameContentDocument(frames[index]);
      if (inner?.documentElement) {
        await walk(inner, [...indexes, index]);
      }
    }
  };
  await walk(document, []);
  return batches;
}

function toolDefinition(tool) {
  const properties = {};
  const required = [];
  for (const parameter of tool.parameters ?? []) {
    const property = { type: parameter.type };
    if (parameter.description) property.description = parameter.description;
    if (Array.isArray(parameter.enum) && parameter.enum.length > 0) {
      property.enum = parameter.enum;
    }
    properties[parameter.name] = property;
    if (parameter.required) required.push(parameter.name);
  }
  return {
    name: tool.id,
    description: tool.description ?? "",
    inputSchema: { type: "object", properties, required },
    actions: (tool.actionBindings ?? []).map((action) => ({
      action: action.action,
      inputParameter: action.inputParameter,
      locatorCandidateIds: action.locatorCandidateIds ?? [],
    })),
    locators: tool.locatorCandidates ?? [],
  };
}

async function executeTIR() {
  const dom = buildDOM();
  const document = dom.window.document;

  // Elements are identified by their index in document order. Nothing is
  // stamped onto the DOM, so the document apply resolves against is the same
  // document the extractor saw.
  const elements = collectElements(document);
  const indexOf = (element) => elements.indexOf(element);

  const clicked = [];
  const submitted = [];
  const dispatched = [];
  const inputEvents = [];
  for (const scope of collectDocuments(document)) {
    scope.addEventListener(
      "click",
      (event) => {
        const target = composedTarget(event);
        clicked.push(indexOf(target));
        if (isSubmitControl(target)) {
          submitted.push({
            index: indexOf(target),
            name: submitName(target),
          });
        }
        event.preventDefault();
      },
      true,
    );
    scope.addEventListener(
      "input",
      (event) => {
        dispatched.push(indexOf(composedTarget(event)));
        inputEvents.push(event.constructor?.name ?? "");
      },
      true,
    );
    scope.addEventListener("change", (event) => dispatched.push(indexOf(composedTarget(event))), true);
  }

  globalThis.document = document;
  globalThis.window = dom.window;

  let applyError = null;
  let executeTool;
  try {
    ({ executeTool } = await loadEsbuildModule(
      resolve(repositoryRoot, "client", "src", "apply-runtime.ts"),
      "geovisor-apply-",
    ));
  } catch (error) {
    applyError = messageOf(error);
  }

  const tools = [];
  if (!applyError) {
    const documentTIR = JSON.parse(await readFile(resolve(tirPath), "utf8"));
    for (const tool of documentTIR.tools ?? []) {
      const definition = toolDefinition(tool);
      for (const input of inputsFor(definition.inputSchema)) {
        const before = snapshot(elements);
        clicked.length = 0;
        submitted.length = 0;
        dispatched.length = 0;
        inputEvents.length = 0;
        let error = null;
        try {
          await executeTool(definition, input);
        } catch (caught) {
          error = messageOf(caught);
        }
        const changed = diff(before, snapshot(elements));
        const resolved = unique([...changed, ...clicked, ...dispatched]);
        tools.push({
          name: definition.name,
          input,
          error,
          changed,
          clicked: unique(clicked),
          submitted: submitted.map((item) => ({ ...item })),
          dispatched: unique(dispatched),
          inputEvents: [...inputEvents],
          state: resolved.map((index) => ({ index, ...describe(elements[index]) })),
        });
      }
    }
  }

  return {
    applyError,
    elements: elements.map((element, index) => ({ index, ...describe(element) })),
    tools,
  };
}

function composedTarget(event) {
  const path = typeof event.composedPath === "function" ? event.composedPath() : [];
  for (const node of path) {
    if (node && node.nodeType === 1) return node;
  }
  return event.target;
}

function isSubmitControl(element) {
  if (!element || element.nodeType !== 1) return false;
  if (element.localName === "button") {
    const type = (element.getAttribute("type") || "submit").toLowerCase();
    return type === "submit" || type === "image";
  }
  if (element.localName === "input") {
    const type = (element.getAttribute("type") || "text").toLowerCase();
    return type === "submit" || type === "image";
  }
  return false;
}

function submitName(element) {
  return (
    element.getAttribute("aria-label") ||
    element.getAttribute("value") ||
    (element.textContent || "").trim() ||
    element.localName
  );
}

function describe(element) {
  const selected =
    element.localName === "select" ? element.options[element.selectedIndex] : undefined;
  return {
    tag: element.localName,
    id: element.id || null,
    ariaLabel: element.getAttribute("aria-label"),
    name: element.getAttribute("name"),
    value: "value" in element ? String(element.value ?? "") : null,
    checked: "checked" in element ? Boolean(element.checked) : null,
    selectedIndex: element.localName === "select" ? element.selectedIndex : null,
    selectedLabel: selected ? (selected.label || selected.textContent || "").trim() : null,
    text: element.isContentEditable ? element.textContent : null,
  };
}

function snapshot(elements) {
  return elements.map((element) => {
    const state = describe(element);
    return `${state.value}\u0000${state.checked}\u0000${state.selectedIndex}\u0000${state.text}`;
  });
}

function unique(indexes) {
  return [...new Set(indexes)].filter((index) => index >= 0);
}

function diff(before, after) {
  const changed = [];
  for (let index = 0; index < before.length; index += 1) {
    if (before[index] !== after[index]) changed.push(index);
  }
  return changed;
}

function inputsFor(schema) {
  const target = schema?.properties?.target;
  if (target && Array.isArray(target.enum) && target.enum.length > 0) {
    return target.enum.map((value) => {
      const input = inputFor(schema);
      input.target = value;
      return input;
    });
  }
  return [inputFor(schema)];
}

function inputFor(schema) {
  const input = {};
  for (const [name, property] of Object.entries(schema?.properties ?? {})) {
    input[name] = valueFor(property);
  }
  return input;
}

function valueFor(property) {
  if (Array.isArray(property.enum) && property.enum.length > 0) {
    const usable = property.enum.filter((value) => value !== null);
    return usable.length > 0 ? usable[usable.length - 1] : null;
  }
  const types = Array.isArray(property.type) ? property.type : [property.type];
  if (types.includes("boolean")) return true;
  if (types.includes("integer") || types.includes("number")) return 1;
  return STRING_INPUT;
}
