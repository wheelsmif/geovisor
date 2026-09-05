import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

import { JSDOM } from "jsdom";

const [fixturePath] = process.argv.slice(2);
if (!fixturePath) {
  throw new Error("usage: node client/test/extract-fixture.mjs <fixture.html>");
}

const repositoryRoot = resolve(import.meta.dirname, "..", "..");
const [html, bundle] = await Promise.all([
  readFile(resolve(repositoryRoot, fixturePath), "utf8"),
  readFile(resolve(repositoryRoot, "internal", "payload", "extractor.js"), "utf8"),
]);
const dom = new JSDOM(html, {
  runScripts: "outside-only",
  url: "https://corpus.example/forms?token=CURRENT-URL-SECRET#CURRENT-FRAGMENT-SECRET",
});
dom.window.eval(bundle);
const batch = await dom.window.__GEOVISOR_EXTRACT__({
  safeExplore: true,
  maxDepth: 3,
  maxOperations: 20,
  timeoutMs: 1_000,
});
process.stdout.write(`${JSON.stringify(batch)}\n`);
