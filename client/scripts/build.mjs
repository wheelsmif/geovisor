import { build } from "esbuild";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const verify = process.argv.includes("--verify");

// Two bundles are built from one source tree so the extractor and the generated
// WebMCP runtime share role resolution, accessible-name computation, and
// element addressing. Both are committed and staleness-checked; neither may be
// edited by hand.
const bundles = [
  {
    // Evaluated in the page to install globalThis.__GEOVISOR_EXTRACT__.
    entry: resolve(repositoryRoot, "client", "src", "extractor.ts"),
    output: resolve(repositoryRoot, "internal", "payload", "extractor.js"),
  },
  {
    // Concatenated into the emitted WebMCP module by internal/emitter. The
    // global name becomes a module-scoped `var` in the generated ES module, so
    // the emitter can call __geovisorRuntime.register(definitions).
    entry: resolve(repositoryRoot, "client", "src", "webmcp-runtime.ts"),
    output: resolve(repositoryRoot, "internal", "emitter", "webmcp-runtime.js"),
    globalName: "__geovisorRuntime",
  },
];

for (const bundle of bundles) {
  const result = await build({
    absWorkingDir: repositoryRoot,
    bundle: true,
    charset: "utf8",
    entryPoints: [bundle.entry],
    format: "iife",
    ...(bundle.globalName ? { globalName: bundle.globalName } : {}),
    legalComments: "none",
    minify: true,
    platform: "browser",
    sourcemap: false,
    target: ["es2020"],
    treeShaking: true,
    write: false,
  });

  const output = result.outputFiles[0];
  if (!output) {
    throw new Error(`esbuild did not produce ${relative(repositoryRoot, bundle.output)}`);
  }
  const bytes = Buffer.concat([Buffer.from(output.contents), Buffer.from("\n")]);

  if (verify) {
    let committed;
    try {
      committed = await readFile(bundle.output);
    } catch (error) {
      throw new Error(`generated bundle is missing: ${bundle.output}`, { cause: error });
    }
    if (!committed.equals(bytes)) {
      throw new Error(
        `generated bundle ${relative(repositoryRoot, bundle.output)} is stale; run npm run build`,
      );
    }
  } else {
    await mkdir(dirname(bundle.output), { recursive: true });
    await writeFile(bundle.output, bytes);
  }
}
