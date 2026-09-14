import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { build } from "esbuild";

export const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

export function messageOf(error) {
  return error instanceof Error ? error.message : String(error);
}

export function isFrame(element) {
  return element.localName === "iframe" || element.localName === "frame";
}

// Pre-order, light children before shadow content, no descent past a frame.
export function visitTree(root, visitElement) {
  const visit = (element) => {
    visitElement(element);
    if (isFrame(element)) return;
    for (const child of Array.from(element.children)) visit(child);
    if (element.shadowRoot) {
      for (const child of Array.from(element.shadowRoot.children)) visit(child);
    }
  };
  for (const child of Array.from(root.children)) visit(child);
}

// jsdom 30 parses `<template shadowrootmode>` as an ordinary template. This
// upgrade matches declarative shadow DOM and walks frames/shadows so a
// shadow-hosted frame stays visible to later walks.
export function upgradeDeclarativeShadows(root) {
  visitTree(root, (element) => {
    if (element.shadowRoot) return;
    const template = [...element.children].find(
      (child) => child.localName === "template" && child.hasAttribute("shadowrootmode"),
    );
    if (!template) return;
    const mode = template.getAttribute("shadowrootmode") === "closed" ? "closed" : "open";
    const shadow = element.attachShadow({ mode });
    shadow.append(template.content.cloneNode(true));
    template.remove();
  });
}

export async function loadEsbuildModule(entryPoint, prefix) {
  const result = await build({
    absWorkingDir: repositoryRoot,
    bundle: true,
    charset: "utf8",
    entryPoints: [entryPoint],
    format: "esm",
    legalComments: "none",
    platform: "neutral",
    write: false,
  });
  const output = result.outputFiles[0];
  if (!output) {
    throw new Error(`esbuild did not produce a bundle for ${entryPoint}`);
  }
  const file = join(await mkdtemp(join(tmpdir(), prefix)), "module.mjs");
  await writeFile(file, output.text);
  return import(pathToFileURL(file).href);
}
