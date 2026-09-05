import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { JSDOM } from "jsdom";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const bundle = await readFile(resolve(repositoryRoot, "internal", "payload", "extractor.js"), "utf8");

function page(html, setup) {
  const dom = new JSDOM(html, {
    runScripts: "outside-only",
    url: "https://example.test/page?access_token=must-not-leak#secret",
  });
  setup?.(dom.window);
  dom.window.eval(bundle);
  return dom;
}

function interactions(batch, kind) {
  return batch.interactions.filter((interaction) => interaction.kind === kind);
}

test("extracts four-tier labels and ordered form controls", async () => {
  const dom = page(`
    <main aria-label="Account">
      <form aria-label="Profile">
        <label for="age">Age</label><input id="age" type="number" step="1" required>
        <input aria-labelledby="email-label"><span id="email-label">Email</span>
        <select aria-label="Plan"><option value="hidden-basic">Basic</option><option>Pro</option></select>
        <input placeholder="Nickname">
        <span>Biography</span><textarea></textarea>
        <input type="password" title="Password">
        <button type="submit">Save</button>
      </form>
    </main>
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const form = interactions(batch, "form")[0];
  assert.ok(form);
  assert.equal(form.name, "Profile");
  assert.deepEqual(
    Array.from(form.parameters, (parameter) => parameter.name),
    ["age", "email", "plan", "nickname", "biography", "password"],
  );
  assert.deepEqual(
    Array.from(form.parameters, (parameter) => parameter.sourceOrder),
    Array.from(form.parameters, (parameter) => parameter.sourceOrder).sort((a, b) => a - b),
  );
  assert.equal(form.parameters[0].type, "integer");
  assert.deepEqual(Array.from(form.parameters[2].enum), ["Basic", "Pro"]);
  assert.match(form.parameters[5].description, /never observed/u);
  assert.equal(
    form.actions.find((action) => action.sideEffect.class === "submission")?.sideEffect
      .safeForExploration,
    false,
  );
});

test("traverses nested open shadow roots with structured host paths", async () => {
  const dom = page("<div id='outer'></div>", (window) => {
    const outer = window.document.querySelector("#outer");
    const first = outer.attachShadow({ mode: "open" });
    first.innerHTML = "<section id='inner'></section>";
    const inner = first.querySelector("#inner");
    const second = inner.attachShadow({ mode: "open" });
    second.innerHTML = "<button aria-label='Shadow action'>ignored current text</button>";
  });

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const action = interactions(batch, "action").find(
    (candidate) => candidate.name === "Shadow action",
  );
  assert.ok(action);
  assert.equal(action.locators[0].shadowPath.length, 2);
  assert.equal(action.locators[0].shadowPath[0].css, "div");
  assert.equal(action.locators[0].shadowPath[1].css, "#inner");
  assert.equal(action.locators[0].framePath.length, 0);
});

test("safe exploration opens details without clicks, submits, navigation, or fetch", async () => {
  let clicks = 0;
  let submits = 0;
  let fetches = 0;
  const dom = page("<details><summary>Advanced</summary><button>Apply</button></details>", (window) => {
    window.HTMLElement.prototype.click = () => {
      clicks++;
    };
    window.HTMLFormElement.prototype.submit = () => {
      submits++;
    };
    window.HTMLFormElement.prototype.requestSubmit = () => {
      submits++;
    };
    window.fetch = async () => {
      fetches++;
      throw new Error("network forbidden");
    };
  });

  const batch = await dom.window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 1,
    maxOperations: 1,
  });
  assert.equal(dom.window.document.querySelector("details").open, true);
  assert.equal(clicks, 0);
  assert.equal(submits, 0);
  assert.equal(fetches, 0);
  const summary = interactions(batch, "action").find((action) => action.name === "Advanced");
  assert.ok(
    summary.evidence.some(
      (evidence) => evidence.reference === "exploration:details-opened-without-click",
    ),
  );
});

test("safe exploration obeys operation and depth caps", async () => {
  const dom = page(`
    <details><summary>One</summary><details><summary>Nested</summary></details></details>
    <details><summary>Two</summary></details>
  `);

  await dom.window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 0,
    maxOperations: 1,
  });
  const details = dom.window.document.querySelectorAll("details");
  assert.equal(details[0].open, true);
  assert.equal(details[1].open, false);
  assert.equal(details[2].open, false);
});

test("keeps duplicate names distinct within the same semantic scope", async () => {
  const dom = page(`
    <section role="region" aria-label="Filters">
      <input aria-label="Query"><input aria-label="Query">
    </section>
    <section role="region" aria-label="Help">
      <input aria-label="Query">
    </section>
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const controls = interactions(batch, "control");
  assert.deepEqual(Array.from(controls, (control) => control.name), ["Query", "Query (2)", "Query"]);
  assert.deepEqual(
    Array.from(controls, (control) => control.scope.at(-1)?.name),
    ["Filters", "Filters", "Help"],
  );
});

test("never serializes current or hidden sensitive values", async () => {
  const dom = page(`
    <input aria-label="Public" value="CURRENT-VALUE-SECRET">
    <input type="password" aria-label="Password" value="PASSWORD-SECRET">
    <input type="hidden" name="csrf" value="TOKEN-SECRET">
    <select aria-label="Tier"><option value="HIDDEN-OPTION-SECRET">Visible option</option></select>
  `);

  const serialized = JSON.stringify(await dom.window.__GEOVISOR_EXTRACT__());
  assert.doesNotMatch(
    serialized,
    /CURRENT-VALUE-SECRET|PASSWORD-SECRET|TOKEN-SECRET|HIDDEN-OPTION-SECRET/u,
  );
  assert.match(serialized, /Sensitive password field/u);
  assert.match(serialized, /Visible option/u);
});

test("extracts semantic custom controls and conservative triggers", async () => {
  const dom = page(`
    <div role="switch" aria-label="Dark mode"></div>
    <div role="tab" aria-label="Settings" aria-controls="panel"></div>
    <button command="show-modal" commandfor="dialog">Open dialog</button>
    <dialog id="dialog"></dialog>
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const control = interactions(batch, "control").find((item) => item.name === "Dark mode");
  assert.equal(control.parameters[0].type, "boolean");
  assert.equal(control.actions[0].kind, "check");
  for (const action of interactions(batch, "action")) {
    assert.equal(action.actions[0].sideEffect.safeForExploration, false);
  }
});

test("classifies submission only for controls associated with a form", async () => {
  const dom = page(`
    <button aria-label="Standalone">Standalone</button>
    <form aria-label="Checkout"><button aria-label="Submit order">Submit</button></form>
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const standalone = interactions(batch, "action").find((item) => item.name === "Standalone");
  const submit = interactions(batch, "action").find((item) => item.name === "Submit order");
  assert.equal(standalone.actions[0].sideEffect.class, "unknown");
  assert.equal(submit.actions[0].sideEffect.class, "submission");
});

test("repeated extraction is deterministic", async () => {
  const dom = page(`
    <form aria-label="Search">
      <input aria-label="Query"><button type="submit">Go</button>
    </form>
    <details><summary>More</summary><a href="/next">Next</a></details>
  `);
  const options = { safeExplore: true, maxDepth: 2, maxOperations: 1, timeoutMs: 5_000 };
  const first = JSON.stringify(await dom.window.__GEOVISOR_EXTRACT__(options));
  const second = JSON.stringify(await dom.window.__GEOVISOR_EXTRACT__(options));
  assert.equal(second, first);
});

test("matches the shared Go and Node observation fixture", async () => {
  const dom = page("<input aria-label='Query'>");
  const actual = JSON.parse(JSON.stringify(await dom.window.__GEOVISOR_EXTRACT__()));
  const expected = JSON.parse(
    await readFile(
      resolve(repositoryRoot, "internal", "payload", "testdata", "observation-batch.json"),
      "utf8",
    ),
  );
  assert.deepEqual(actual, expected);
});
