import assert from "node:assert/strict";
import { resolve } from "node:path";
import test from "node:test";

import { JSDOM } from "jsdom";

import { loadEsbuildModule, repositoryRoot } from "./helpers.mjs";

async function loadRuntime() {
  return loadEsbuildModule(
    resolve(repositoryRoot, "client", "src", "apply-runtime.ts"),
    "geovisor-apply-",
  );
}

function fillDefinition(required = true) {
  return {
    name: "fill_query",
    description: "Fill query",
    inputSchema: {
      type: "object",
      properties: { query: { type: "string" } },
      required: required ? ["query"] : [],
    },
    actions: [{ action: "fill", inputParameter: "query", locatorCandidateIds: ["q"] }],
    locators: [
      {
        id: "q",
        framePath: [],
        shadowPath: [],
        semantic: { role: "textbox", name: "Query" },
      },
    ],
  };
}

test("fill uses the native value setter and dispatches InputEvent", async () => {
  const { executeTool } = await loadRuntime();
  const dom = new JSDOM(`<input aria-label="Query">`, { url: "https://example.test/" });
  const input = dom.window.document.querySelector("input");
  const descriptor = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value");
  let usedNative = false;
  Object.defineProperty(dom.window.HTMLInputElement.prototype, "value", {
    configurable: true,
    get() {
      return descriptor.get.call(this);
    },
    set(value) {
      usedNative = true;
      descriptor.set.call(this, value);
    },
  });
  let inputEventName = "";
  input.addEventListener("input", (event) => {
    inputEventName = event.constructor.name;
  });

  globalThis.document = dom.window.document;
  globalThis.window = dom.window;
  await executeTool(fillDefinition(), { query: "typed" });
  assert.equal(input.value, "typed");
  assert.equal(usedNative, true);
  assert.equal(inputEventName, "InputEvent");
});

test("follow_link target clicks the named locator, not the first", async () => {
  const { executeTool } = await loadRuntime();
  const dom = new JSDOM(
    `<nav><a href="/home">Home</a><a href="/next">Next</a></nav>`,
    { url: "https://example.test/" },
  );
  const home = dom.window.document.querySelector('a[href="/home"]');
  const next = dom.window.document.querySelector('a[href="/next"]');
  const clicks = [];
  home.addEventListener("click", (event) => {
    event.preventDefault();
    clicks.push("Home");
  });
  next.addEventListener("click", (event) => {
    event.preventDefault();
    clicks.push("Next");
  });

  globalThis.document = dom.window.document;
  globalThis.window = dom.window;
  const definition = {
    name: "follow_link",
    description: "target is the control's accessible name",
    inputSchema: {
      type: "object",
      properties: { target: { type: "string", enum: ["Home", "Next"] } },
      required: ["target"],
    },
    actions: [
      {
        action: "click",
        inputParameter: "target",
        locatorSelection: "by_target_name",
        locatorCandidateIds: ["home", "next"],
      },
    ],
    locators: [
      {
        id: "home",
        framePath: [],
        shadowPath: [],
        semantic: { role: "link", name: "Home" },
      },
      {
        id: "next",
        framePath: [],
        shadowPath: [],
        semantic: { role: "link", name: "Next" },
      },
    ],
  };

  await executeTool(definition, { target: "Next" });
  assert.deepEqual(clicks, ["Next"]);
  await assert.rejects(() => executeTool(definition, { target: "Missing" }), /target does not match a locator/u);
  assert.deepEqual(clicks, ["Next"]);
});

test("checkbox labeled Target stays a boolean while a family target stays a name", async () => {
  const { executeTool } = await loadRuntime();
  const dom = new JSDOM(
    `<label><input type="checkbox" checked> Target</label><a href="/home">Home</a><a href="/next">Next</a>`,
    { url: "https://example.test/" },
  );
  const box = dom.window.document.querySelector("input");
  const clicks = [];
  for (const link of dom.window.document.querySelectorAll("a")) {
    link.addEventListener("click", (event) => {
      event.preventDefault();
      clicks.push(link.textContent);
    });
  }
  globalThis.document = dom.window.document;
  globalThis.window = dom.window;

  const checkbox = {
    name: "target",
    description: "Target",
    inputSchema: {
      type: "object",
      properties: { target: { type: "boolean" } },
      required: ["target"],
    },
    actions: [{
      action: "check",
      inputParameter: "target",
      locatorSelection: "ranked",
      locatorCandidateIds: ["box"],
    }],
    locators: [{
      id: "box",
      framePath: [],
      shadowPath: [],
      semantic: { role: "checkbox", name: "Target" },
    }],
  };
  const family = {
    name: "follow_link",
    description: "Follow one of this page's links",
    inputSchema: {
      type: "object",
      properties: { target: { type: "string", enum: ["Home", "Next"] } },
      required: ["target"],
    },
    actions: [{
      action: "click",
      inputParameter: "target",
      locatorSelection: "by_target_name",
      locatorCandidateIds: ["home", "next"],
    }],
    locators: [
      { id: "home", framePath: [], shadowPath: [], semantic: { role: "link", name: "Home" } },
      { id: "next", framePath: [], shadowPath: [], semantic: { role: "link", name: "Next" } },
    ],
  };

  await executeTool(checkbox, { target: false });
  assert.equal(box.checked, false);
  await executeTool(family, { target: "Next" });
  assert.deepEqual(clicks, ["Next"]);
  assert.equal(box.checked, false);
});

test("family target ordinals skip a literal Name (2)", async () => {
  const { executeTool } = await loadRuntime();
  const dom = new JSDOM(
    `<a href="/a">Docs</a><a href="/b">Docs</a><a href="/c">Docs (2)</a>`,
    { url: "https://example.test/" },
  );
  const clicks = [];
  for (const anchor of dom.window.document.querySelectorAll("a")) {
    anchor.addEventListener("click", (event) => {
      event.preventDefault();
      clicks.push(anchor.getAttribute("href"));
    });
  }
  globalThis.document = dom.window.document;
  globalThis.window = dom.window;
  const definition = {
    name: "follow_link",
    description: "Follow one of this page's links",
    inputSchema: {
      type: "object",
      properties: {
        target: { type: "string", enum: ["Docs", "Docs (3)", "Docs (2)"] },
      },
      required: ["target"],
    },
    actions: [{
      action: "click",
      inputParameter: "target",
      locatorSelection: "by_target_name",
      locatorCandidateIds: ["a", "b", "c"],
    }],
    locators: [
      { id: "a", framePath: [], shadowPath: [], semantic: { role: "link", name: "Docs", nth: 0 } },
      { id: "b", framePath: [], shadowPath: [], semantic: { role: "link", name: "Docs", nth: 1 } },
      { id: "c", framePath: [], shadowPath: [], semantic: { role: "link", name: "Docs (2)", nth: 0 } },
    ],
  };
  await executeTool(definition, { target: "Docs (2)" });
  assert.deepEqual(clicks, ["/c"]);
  await executeTool(definition, { target: "Docs (3)" });
  assert.deepEqual(clicks, ["/c", "/b"]);
});

test("execute throws before applying when a required parameter is missing", async () => {
  const { executeTool } = await loadRuntime();
  const dom = new JSDOM(`<input aria-label="Query" value="keep">`, { url: "https://example.test/" });
  const input = dom.window.document.querySelector("input");
  globalThis.document = dom.window.document;
  globalThis.window = dom.window;
  await assert.rejects(() => executeTool(fillDefinition(true), {}), /missing required parameter "query"/u);
  assert.equal(input.value, "keep");
});
