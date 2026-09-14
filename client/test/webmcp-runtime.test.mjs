import assert from "node:assert/strict";
import { resolve } from "node:path";
import test from "node:test";

import { JSDOM } from "jsdom";

import { loadEsbuildModule, repositoryRoot } from "./helpers.mjs";

async function loadRuntime() {
  return loadEsbuildModule(
    resolve(repositoryRoot, "client", "src", "webmcp-runtime.ts"),
    "geovisor-runtime-",
  );
}

async function registerFill(window, required = true) {
  const { register } = await loadRuntime();
  const tools = [];
  window.document.modelContext = {
    registerTool(registration) {
      tools.push(registration);
    },
  };
  globalThis.document = window.document;
  globalThis.window = window;
  await register([
    {
      name: "fill_query",
      description: "Fill query",
      inputSchema: {
        type: "object",
        properties: { query: { type: "string" } },
        required: required ? ["query"] : [],
      },
      annotations: {},
      actions: [{ action: "fill", inputParameter: "query", locatorCandidateIds: ["q"] }],
      locators: [
        {
          id: "q",
          framePath: [],
          shadowPath: [],
          semantic: { role: "textbox", name: "Query" },
        },
      ],
    },
  ]);
  return tools[0];
}

test("fill uses the native value setter and dispatches InputEvent", async () => {
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

  const tool = await registerFill(dom.window);
  await tool.execute({ query: "typed" });
  assert.equal(input.value, "typed");
  assert.equal(usedNative, true);
  assert.equal(inputEventName, "InputEvent");
});

test("follow_link target clicks the named locator, not the first", async () => {
  const { register } = await loadRuntime();
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

  const tools = [];
  dom.window.document.modelContext = {
    registerTool(registration) {
      tools.push(registration);
    },
  };
  globalThis.document = dom.window.document;
  globalThis.window = dom.window;
  await register([
    {
      name: "follow_link",
      description: "target is the control's accessible name",
      inputSchema: {
        type: "object",
        properties: { target: { type: "string", enum: ["Home", "Next"] } },
        required: ["target"],
      },
      annotations: {},
      actions: [
        {
          action: "click",
          inputParameter: "target",
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
    },
  ]);

  await tools[0].execute({ target: "Next" });
  assert.deepEqual(clicks, ["Next"]);
  await assert.rejects(() => tools[0].execute({ target: "Missing" }), /target does not match a locator/u);
  assert.deepEqual(clicks, ["Next"]);
});

test("execute throws before applying when a required parameter is missing", async () => {
  const dom = new JSDOM(`<input aria-label="Query" value="keep">`, { url: "https://example.test/" });
  const input = dom.window.document.querySelector("input");
  const tool = await registerFill(dom.window, true);
  await assert.rejects(() => tool.execute({}), /missing required parameter "query"/u);
  assert.equal(input.value, "keep");
});
