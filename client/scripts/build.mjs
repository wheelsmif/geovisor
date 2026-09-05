import { build } from "esbuild";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const entryPoint = resolve(repositoryRoot, "client", "src", "extractor.ts");
const outputPath = resolve(repositoryRoot, "internal", "payload", "extractor.js");
const verify = process.argv.includes("--verify");

const result = await build({
  absWorkingDir: repositoryRoot,
  bundle: true,
  charset: "utf8",
  entryPoints: [entryPoint],
  format: "iife",
  legalComments: "none",
  minify: true,
  platform: "browser",
  sourcemap: false,
  target: ["es2020"],
  treeShaking: true,
  write: false,
});

const output = result.outputFiles[0];
if (!output) throw new Error("esbuild did not produce the browser payload");
const bytes = Buffer.concat([Buffer.from(output.contents), Buffer.from("\n")]);

if (verify) {
  let committed;
  try {
    committed = await readFile(outputPath);
  } catch (error) {
    throw new Error(`generated bundle is missing: ${outputPath}`, { cause: error });
  }
  if (!committed.equals(bytes)) {
    throw new Error("generated browser bundle is stale; run npm run build");
  }
} else {
  await mkdir(dirname(outputPath), { recursive: true });
  await writeFile(outputPath, bytes);
}
