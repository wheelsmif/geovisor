import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import test from "node:test";

import { JSDOM } from "jsdom";

import { loadEsbuildModule, repositoryRoot, upgradeDeclarativeShadows } from "./helpers.mjs";

async function loadLocate() {
  return loadEsbuildModule(
    resolve(repositoryRoot, "client", "src", "shared", "locate.ts"),
    "geovisor-locate-",
  );
}

// The Go walk in collectFrameOwnerOrder and this TypeScript walk must number a
// shadow-hosted frame the same way: the frame inside the earlier host is index
// 0, the later light sibling is index 1. jsdom never creates contentDocument
// for an iframe in a shadow root, so this test asserts the numbering rather
// than executing into the frame.
test("numbers a shadow-hosted frame before a later light sibling", async () => {
  const html = await readFile(
    resolve(repositoryRoot, "testdata", "corpus", "shadow-frame.html"),
    "utf8",
  );
  const dom = new JSDOM(html, { url: "https://example.test/shadow-frame" });
  upgradeDeclarativeShadows(dom.window.document);
  const { frameCandidates } = await loadLocate();
  const frames = frameCandidates(dom.window.document);
  assert.equal(frames.length, 2);
  assert.equal(frames[0].getRootNode(), dom.window.document.querySelector(".host").shadowRoot);
  assert.equal(frames[1].getRootNode(), dom.window.document);
});

test("counts role=presentation iframes by localName", async () => {
  const html = await readFile(
    resolve(repositoryRoot, "testdata", "corpus", "presentation-frames.html"),
    "utf8",
  );
  const dom = new JSDOM(html, { url: "https://example.test/presentation-frames" });
  const { frameCandidates } = await loadLocate();
  const frames = frameCandidates(dom.window.document);
  assert.equal(frames.length, 2);
  assert.equal(frames[0].getAttribute("role"), "presentation");
  assert.equal(frames[1].hasAttribute("role"), false);
});
