import { build } from "esbuild";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const verify = process.argv.includes("--verify");

// Evaluated in the page to install globalThis.__GEOVISOR_EXTRACT__.
// Role resolution, accessible-name computation, and element addressing live in
// client/src/shared/ so the extractor and the apply runtime cannot drift.
const entry = resolve(repositoryRoot, "client", "src", "extractor.ts");
const output = resolve(repositoryRoot, "internal", "payload", "extractor.js");

const result = await build({
  absWorkingDir: repositoryRoot,
  bundle: true,
  charset: "utf8",
  entryPoints: [entry],
  format: "iife",
  legalComments: "eof",
  minify: true,
  platform: "browser",
  sourcemap: false,
  target: ["es2020"],
  treeShaking: true,
  write: false,
});

const file = result.outputFiles[0];
if (!file) {
  throw new Error(`esbuild did not produce ${relative(repositoryRoot, output)}`);
}
// @medv/finder marks its MIT notice with "// License:", which esbuild does not
// treat as a legal comment, so legalComments: "eof" does not keep it. Append
// the upstream notice at the end of the bundle.
const finderNotice = `
/*!
 * @license
 * License: MIT
 * Author: Anton Medvedev <anton@medv.io>
 * Source: https://github.com/antonmedv/finder
 */
`;
const bytes = Buffer.concat([
  Buffer.from(file.contents),
  Buffer.from(finderNotice),
]);

if (verify) {
  let committed;
  try {
    committed = await readFile(output);
  } catch (error) {
    throw new Error(`generated bundle is missing: ${output}`, { cause: error });
  }
  if (!committed.equals(bytes)) {
    throw new Error(
      `generated bundle ${relative(repositoryRoot, output)} is stale; run npm run build`,
    );
  }
} else {
  await mkdir(dirname(output), { recursive: true });
  await writeFile(output, bytes);
}
