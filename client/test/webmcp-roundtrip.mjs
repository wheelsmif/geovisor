// Round-trip driver for the generated WebMCP runtime (GV-036).
//
// The emitted artifact is an ES module, which jsdom cannot execute. Node can,
// so `execute` mode builds the DOM with jsdom, points the Node global
// `document` at it, and imports the emitted module for real. The module reads
// `document`, `element.ownerDocument.defaultView.Event`, and `DOMException`,
// all of which resolve correctly under that arrangement.
//
//   node client/test/webmcp-roundtrip.mjs extract <fixture.html>
//   node client/test/webmcp-roundtrip.mjs execute <fixture.html> <module.mjs>
//
// Both modes write a single JSON document to stdout.

import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

import { JSDOM, VirtualConsole } from "jsdom";

import { isFrame, messageOf, repositoryRoot, upgradeDeclarativeShadows, visitTree } from "./helpers.mjs";

const FIXTURE_URL = "https://roundtrip.example/";
const STRING_INPUT = "GV-ROUNDTRIP";

const [mode, fixturePath, modulePath] = process.argv.slice(2);
if (mode !== "extract" && mode !== "execute") {
  throw new Error("usage: webmcp-roundtrip.mjs <extract|execute> <fixture.html> [module.mjs]");
}
if (!fixturePath) {
  throw new Error("a fixture path is required");
}
if (mode === "execute" && !modulePath) {
  throw new Error("execute mode requires the emitted module path");
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
  process.stdout.write(`${JSON.stringify(await executeModule())}\n`);
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
// That is the extract → compile → execute path GV-003's harness was missing.
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

async function executeModule() {
  const dom = buildDOM();
  const document = dom.window.document;

  // Elements are identified by their index in document order. Nothing is
  // stamped onto the DOM, so the document the module resolves against is the
  // same document the extractor saw.
  const elements = collectElements(document);
  const indexOf = (element) => elements.indexOf(element);

  // The runtime identifies itself by what it dispatches: `click` for click
  // actions and `input`/`change` for fill, select, and check. Listening for
  // those is how the driver learns which element each action resolved to, which
  // is more precise than diffing state -- setting a checkbox that is already
  // checked resolves correctly while changing nothing.
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
        // Cancel navigation and submission: the driver observes which element
        // was clicked, it does not exercise what the page would do next.
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

  const registrations = [];
  document.modelContext = {
    registerTool(registration) {
      registrations.push(registration);
    },
  };

  globalThis.document = document;
  globalThis.window = dom.window;

  let moduleError = null;
  try {
    await import(pathToFileURL(resolve(modulePath)).href);
  } catch (error) {
    moduleError = messageOf(error);
  }

  const tools = [];
  for (const registration of registrations) {
    for (const input of inputsFor(registration.inputSchema)) {
      const before = snapshot(elements);
      clicked.length = 0;
      submitted.length = 0;
      dispatched.length = 0;
      inputEvents.length = 0;
      let error = null;
      try {
        await registration.execute(input);
      } catch (caught) {
        error = messageOf(caught);
      }
      const changed = diff(before, snapshot(elements));
      const resolved = unique([...changed, ...clicked, ...dispatched]);
      tools.push({
        name: registration.name,
        annotations: registration.annotations ?? null,
        input,
        error,
        changed,
        clicked: unique(clicked),
        submitted: submitted.map((item) => ({ ...item })),
        dispatched: unique(dispatched),
        inputEvents: [...inputEvents],
        // State is reported for every element the tool resolved to, not only the
        // ones whose state changed, so an assertion can check the end state even
        // when the requested state already held.
        state: resolved.map((index) => ({ index, ...describe(elements[index]) })),
      });
    }
  }

  return {
    moduleError,
    elements: elements.map((element, index) => ({ index, ...describe(element) })),
    tools,
  };
}

// Shadow-retargeted events expose the host as event.target when observed
// from the document. composedPath keeps the originating node so Alpha/Beta
// (and other shadow tools) are not reported as the same host click (P19).
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
    // The advertised enum is built from option labels, so the label is what an
    // assertion has to compare against.
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
    // The *last* enum value, not the first: the first option of a <select> is
    // already selected, so choosing it would let a tool that cannot change the
    // selection at all still look like it worked.
    const usable = property.enum.filter((value) => value !== null);
    return usable.length > 0 ? usable[usable.length - 1] : null;
  }
  const types = Array.isArray(property.type) ? property.type : [property.type];
  if (types.includes("boolean")) return true;
  if (types.includes("integer") || types.includes("number")) return 1;
  return STRING_INPUT;
}
