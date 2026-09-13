import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";

import { build } from "esbuild";
import { JSDOM } from "jsdom";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

async function loadRuntime() {
  const result = await build({
    absWorkingDir: repositoryRoot,
    bundle: true,
    charset: "utf8",
    entryPoints: [resolve(repositoryRoot, "client", "src", "webmcp-runtime.ts")],
    format: "esm",
    legalComments: "none",
    platform: "neutral",
    write: false,
  });
  const output = result.outputFiles[0];
  if (!output) {
    throw new Error("esbuild did not produce a runtime bundle");
  }
  const file = join(await mkdtemp(join(tmpdir(), "geovisor-runtime-")), "runtime.mjs");
  await writeFile(file, output.text);
  return import(pathToFileURL(file).href);
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

test("execute throws before applying when a required parameter is missing", async () => {
  const dom = new JSDOM(`<input aria-label="Query" value="keep">`, { url: "https://example.test/" });
  const input = dom.window.document.querySelector("input");
  const tool = await registerFill(dom.window, true);
  await assert.rejects(() => tool.execute({}), /missing required parameter "query"/u);
  assert.equal(input.value, "keep");
});
