import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";

import { build } from "esbuild";
import { JSDOM } from "jsdom";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

async function loadLocate() {
  const result = await build({
    absWorkingDir: repositoryRoot,
    bundle: true,
    charset: "utf8",
    entryPoints: [resolve(repositoryRoot, "client", "src", "shared", "locate.ts")],
    format: "esm",
    legalComments: "none",
    platform: "neutral",
    write: false,
  });
  const output = result.outputFiles[0];
  if (!output) {
    throw new Error("esbuild did not produce a locate bundle");
  }
  const file = join(await mkdtemp(join(tmpdir(), "geovisor-locate-")), "locate.mjs");
  await writeFile(file, output.text);
  return import(pathToFileURL(file).href);
}

function upgradeDeclarativeShadows(document) {
  const hosts = [...document.querySelectorAll("*")].filter(
    (element) => !element.shadowRoot && [...element.children].some(
      (child) => child.localName === "template" && child.hasAttribute("shadowrootmode"),
    ),
  );
  for (const host of hosts) {
    const template = [...host.children].find(
      (child) => child.localName === "template" && child.hasAttribute("shadowrootmode"),
    );
    const mode = template.getAttribute("shadowrootmode") === "closed" ? "closed" : "open";
    const shadow = host.attachShadow({ mode });
    shadow.append(template.content.cloneNode(true));
    template.remove();
  }
}

// GV-003 / Phase 6.3. The Go walk in collectFrameOwnerOrder and this
// TypeScript walk must number a shadow-hosted frame the same way: the frame
// inside the earlier host is index 0, the later light sibling is index 1.
// jsdom never creates contentDocument for an iframe in a shadow root, so this
// test asserts the numbering rather than executing into the frame.
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
