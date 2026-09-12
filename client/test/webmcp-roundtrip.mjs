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

const repositoryRoot = resolve(import.meta.dirname, "..", "..");
const html = await readFile(resolve(repositoryRoot, fixturePath), "utf8");

function buildDOM() {
  // jsdom reports unimplemented navigation and form submission as jsdomError.
  // The driver cancels both (see the capturing click listener), so the channel
  // is silenced rather than allowed to pollute stdout.
  const virtualConsole = new VirtualConsole();
  virtualConsole.on("jsdomError", () => {});
  return new JSDOM(html, {
    runScripts: "outside-only",
    url: FIXTURE_URL,
    virtualConsole,
  });
}

if (mode === "extract") {
  const bundle = await readFile(
    resolve(repositoryRoot, "internal", "payload", "extractor.js"),
    "utf8",
  );
  const dom = buildDOM();
  dom.window.eval(bundle);
  const batch = await dom.window.__GEOVISOR_EXTRACT__({ safeExplore: false });
  process.stdout.write(`${JSON.stringify(batch)}\n`);
} else {
  process.stdout.write(`${JSON.stringify(await executeModule())}\n`);
}

async function executeModule() {
  const dom = buildDOM();
  const document = dom.window.document;

  // Elements are identified by their index in document order. Nothing is
  // stamped onto the DOM, so the document the module resolves against is the
  // same document the extractor saw.
  const elements = Array.from(document.querySelectorAll("*"));
  const indexOf = (element) => elements.indexOf(element);

  // The runtime identifies itself by what it dispatches: `click` for click
  // actions and `input`/`change` for fill, select, and check. Listening for
  // those is how the driver learns which element each action resolved to, which
  // is more precise than diffing state -- setting a checkbox that is already
  // checked resolves correctly while changing nothing.
  const clicked = [];
  const dispatched = [];
  document.addEventListener(
    "click",
    (event) => {
      clicked.push(indexOf(event.target));
      // Cancel navigation and submission: the driver observes which element was
      // clicked, it does not exercise what the page would do next.
      event.preventDefault();
    },
    true,
  );
  for (const name of ["input", "change"]) {
    document.addEventListener(name, (event) => dispatched.push(indexOf(event.target)), true);
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
    const before = snapshot(elements);
    clicked.length = 0;
    dispatched.length = 0;
    const input = inputFor(registration.inputSchema);
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
      dispatched: unique(dispatched),
      // State is reported for every element the tool resolved to, not only the
      // ones whose state changed, so an assertion can check the end state even
      // when the requested state already held.
      state: resolved.map((index) => ({ index, ...describe(elements[index]) })),
    });
  }

  return {
    moduleError,
    elements: elements.map((element, index) => ({ index, ...describe(element) })),
    tools,
  };
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

function messageOf(error) {
  if (error instanceof Error) return error.message;
  return String(error);
}
