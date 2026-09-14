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

// jsdom does not ship the UA rule that hides closed <details> content.
function hideClosedDetailsContent(window) {
  const style = window.document.createElement("style");
  style.textContent = "details:not([open]) > *:not(summary) { display: none !important; }";
  window.document.head.appendChild(style);
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
    form.actions.some((action) => action.sideEffect.class === "submission"),
    false,
  );
  const save = interactions(batch, "action").find((item) => item.name === "Save");
  assert.ok(save);
  assert.equal(save.actions[0].kind, "click");
  assert.equal(save.actions[0].sideEffect.class, "submission");
  assert.equal(save.actions[0].sideEffect.safeForExploration, false);
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
  assert.equal(action.locators[0].shadowPath[0].css, "#outer");
  assert.equal(action.locators[0].shadowPath[1].css, "#inner");
  assert.equal(action.locators[0].framePath.length, 0);
});

test("safe exploration opens details without clicks, submits, navigation, or fetch", async () => {
  let clicks = 0;
  let submits = 0;
  let fetches = 0;
  const html = "<details><summary>Advanced</summary><button>Apply</button></details>";
  const dom = page(html, (window) => {
    hideClosedDetailsContent(window);
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
  const before = dom.window.document.documentElement.outerHTML;

  const batch = await dom.window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 1,
    maxOperations: 1,
  });
  assert.equal(dom.window.document.querySelector("details").open, false);
  assert.equal(dom.window.document.documentElement.outerHTML, before);
  assert.equal(clicks, 0);
  assert.equal(submits, 0);
  assert.equal(fetches, 0);
  const summary = interactions(batch, "action").find((action) => action.name === "Advanced");
  assert.ok(
    summary.evidence.some(
      (evidence) => evidence.reference === "exploration:details-opened-without-click",
    ),
  );
  assert.ok(interactions(batch, "action").some((action) => action.name === "Apply"));
});

// GV-009. A microtask yield never ran toggle handlers. A macrotask does, so a
// details whose body is populated on toggle is visible to the second traverse.
test("safe exploration yields revealed controls from a toggle handler", async () => {
  const dom = page("<details><summary>Advanced</summary></details>", (window) => {
    window.document.querySelector("details").addEventListener("toggle", (event) => {
      const details = event.currentTarget;
      if (!details.open || details.querySelector("input")) return;
      const input = window.document.createElement("input");
      input.setAttribute("aria-label", "Revealed field");
      details.appendChild(input);
    });
  });

  const batch = await dom.window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 1,
    maxOperations: 1,
  });
  assert.equal(dom.window.document.querySelector("details").open, false);
  assert.ok(interactions(batch, "control").some((control) => control.name === "Revealed field"));
});

// GV-010. Depth 0 is no exploration. The operations cap is a separate budget.
test("safe exploration treats depth 0 as no exploration", async () => {
  const dom = page(`
    <details><summary>One</summary><button>First</button></details>
    <details><summary>Two</summary><button>Second</button></details>
  `, hideClosedDetailsContent);

  const batch = await dom.window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 0,
    maxOperations: 100,
  });
  const details = dom.window.document.querySelectorAll("details");
  assert.equal(details[0].open, false);
  assert.equal(details[1].open, false);
  assert.equal(
    interactions(batch, "action").some((action) => action.name === "First"),
    false,
  );
});

test("safe exploration distinguishes the operations cap from the depth cap", async () => {
  const html = `
    <details><summary>One</summary><button>First</button>
      <details><summary>Nested</summary><button>Deep</button></details>
    </details>
    <details><summary>Two</summary><button>Second</button></details>
  `;

  const depthLimited = await page(html, hideClosedDetailsContent).window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 1,
    maxOperations: 100,
  });
  const depthNames = interactions(depthLimited, "action").map((action) => action.name);
  assert.equal(depthNames.includes("First"), true);
  assert.equal(depthNames.includes("Second"), true);
  assert.equal(depthNames.includes("Deep"), false);

  const operationLimited = await page(html, hideClosedDetailsContent).window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 2,
    maxOperations: 1,
  });
  const operationNames = interactions(operationLimited, "action").map((action) => action.name);
  assert.equal(operationNames.includes("First"), true);
  assert.equal(operationNames.includes("Second"), false);
  assert.equal(operationNames.includes("Deep"), false);
});

test("safe exploration stops after a yield that exhausts the time budget", async () => {
  const html = `
    <details><summary>One</summary><button>First</button></details>
    <details><summary>Two</summary><button>Second</button></details>
  `;
  const dom = page(html, (window) => {
    hideClosedDetailsContent(window);
    window.document.querySelector("details").addEventListener("toggle", () => {
      const start = window.performance.now();
      while (window.performance.now() - start < 30) {
        // Burn the exploration budget after the macrotask yield.
      }
    });
  });
  const batch = await dom.window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 1,
    maxOperations: 100,
    timeoutMs: 5,
  });
  const names = interactions(batch, "action").map((action) => action.name);
  assert.equal(names.includes("First"), true);
  assert.equal(names.includes("Second"), false);
});

test("safe exploration enforces the time budget independently of operations", async () => {
  const html = `
    <details><summary>One</summary><button>First</button></details>
    <details><summary>Two</summary><button>Second</button></details>
  `;
  const batch = await page(html, hideClosedDetailsContent).window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 1,
    maxOperations: 100,
    timeoutMs: 0,
  });
  assert.equal(
    interactions(batch, "action").some((action) => action.name === "First"),
    false,
  );
});

// GV-018. A document-wide traversal index in the fallback name would shift
// every downstream tool ID when an unrelated earlier element is inserted.
test("positional fallback names do not enter identity or locators", async () => {
  const extractUnnamed = async (prefix) => {
    const batch = await page(`${prefix}<input><button>Go</button>`).window.__GEOVISOR_EXTRACT__();
    return {
      control: interactions(batch, "control")[0],
      action: interactions(batch, "action").find((item) => item.name === "Go"),
    };
  };

  const baseline = await extractUnnamed("");
  const shifted = await extractUnnamed('<div id="pad"></div>');
  assert.equal(baseline.control.name, "");
  assert.equal(shifted.control.name, "");
  assert.equal(baseline.control.locators[0].semantic.name, undefined);
  assert.equal(shifted.control.locators[0].semantic.name, undefined);
  assert.equal(baseline.control.locators[0].semantic.nth, shifted.control.locators[0].semantic.nth);
  assert.equal(baseline.action.name, shifted.action.name);
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
  assert.deepEqual(
    Array.from(controls, (control) => control.locators[0].semantic.nth),
    [0, 1, undefined],
  );
});

test("records match ordinals on ambiguous semantic scopes", async () => {
  const dom = page(`
    <div role="region" aria-label="Panel"><input aria-label="Query"></div>
    <div role="region" aria-label="Panel"><input aria-label="Query"></div>
  `);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const nodes = interactions(batch, "control").map(
    (control) => control.locators[0].semantic.scope.at(-1),
  );
  assert.equal(nodes.length, 2);
  assert.equal(nodes[0].role, "region");
  assert.equal(nodes[0].name, "Panel");
  assert.equal(nodes[0].nth, 0);
  assert.equal(nodes[1].role, "region");
  assert.equal(nodes[1].name, "Panel");
  assert.equal(nodes[1].nth, 1);
});

test("never serializes current or hidden sensitive values", async () => {
  const dom = page(`
    <input aria-label="Public" value="CURRENT-VALUE-SECRET">
    <label>Name <input value="INPUT-SECRET"></label>
    <label>Bio <textarea>TEXTAREA-SECRET</textarea></label>
    <textarea id="src">LABELLEDBY-SECRET</textarea>
    <input aria-labelledby="src" aria-label="Cited">
    <textarea id="hint">DESCRIBED-SECRET</textarea>
    <input aria-label="Noted" aria-describedby="hint">
    <input type="password" aria-label="Password" value="PASSWORD-SECRET">
    <input type="hidden" name="csrf" value="TOKEN-SECRET">
    <select aria-label="Tier"><option value="HIDDEN-OPTION-SECRET">Visible option</option></select>
  `);

  const serialized = JSON.stringify(await dom.window.__GEOVISOR_EXTRACT__());
  assert.doesNotMatch(
    serialized,
    /CURRENT-VALUE-SECRET|INPUT-SECRET|TEXTAREA-SECRET|LABELLEDBY-SECRET|DESCRIBED-SECRET|PASSWORD-SECRET|TOKEN-SECRET|HIDDEN-OPTION-SECRET/u,
  );
  assert.match(serialized, /Sensitive password field/u);
  assert.match(serialized, /Visible option/u);
});

test("extracts semantic custom controls and conservative triggers", async () => {
  const dom = page(`
    <div role="switch" aria-label="Dark mode"></div>
    <div role="textbox" aria-label="Custom box"></div>
    <div role="combobox" aria-label="Custom combo"></div>
    <div role="slider" aria-label="Custom slider"></div>
    <div role="tab" aria-label="Settings" aria-controls="panel"></div>
    <button command="show-modal" commandfor="dialog">Open dialog</button>
    <dialog id="dialog"></dialog>
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const dark = interactions(batch, "action").find((item) => item.name === "Dark mode");
  assert.ok(dark);
  assert.equal(dark.actions[0].kind, "click");
  assert.equal(
    interactions(batch, "control").some((item) =>
      ["Custom box", "Custom combo", "Custom slider"].includes(item.name),
    ),
    false,
  );
  assert.ok(batch.warnings.some((warning) => warning.code === "custom_control_omitted"));
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

// GV-002. Hiding is inherited, but only `visibility` is an inherited CSS
// property: a descendant of a `display: none` wrapper still reports its own
// `display`, so an element-local check cannot see it.
test("excludes controls hidden by an ancestor, not just by themselves", async () => {
  const dom = page(`
    <div style="display:none"><input aria-label="Under display none"></div>
    <div style="visibility:hidden"><input aria-label="Under visibility hidden"></div>
    <div hidden><input aria-label="Under hidden attribute"></div>
    <div aria-hidden="true"><input aria-label="Under aria-hidden"></div>
    <input aria-label="Visible">
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    ["Visible"],
  );
});

// A descendant may opt back in to visibility, which is why `visibility` must
// not be treated as hiding the whole subtree.
test("honors a visibility:visible override inside a hidden ancestor", async () => {
  const dom = page(`
    <div style="visibility:hidden">
      <input aria-label="Still hidden">
      <input aria-label="Opted back in" style="visibility:visible">
    </div>
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    ["Opted back in"],
  );
});

// GV-007. A control owned by a form is a parameter of the form's tool, so
// emitting it standalone as well would give an agent two ways to fill one field
// and no basis for choosing. Submit buttons stay standalone: they are actions
// rather than parameters, and the form tool requires every required parameter.
test("emits form-owned controls only as form parameters", async () => {
  const dom = page(`
    <form aria-label="Profile">
      <input aria-label="Display name">
      <button type="submit">Save</button>
    </form>
    <input aria-label="Unowned">
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.deepEqual(
    Array.from(interactions(batch, "form"), (item) => item.name),
    ["Profile"],
  );
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    ["Unowned"],
  );
  assert.deepEqual(
    Array.from(interactions(batch, "action"), (item) => item.name),
    ["Save"],
  );
});

// Ownership follows HTML form-associated elements. A raw `form` attribute on a
// contenteditable div does not make it a form control.
test("does not claim contenteditable divs from a raw form attribute", async () => {
  const dom = page(`
    <form id="notes" aria-label="Notes"></form>
    <div contenteditable aria-label="Memo" form="notes"></div>
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.deepEqual(
    Array.from(interactions(batch, "form")[0].parameters, (parameter) => parameter.name),
    [],
  );
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    ["Memo"],
  );
});

test("claims controls associated with a form by attribute", async () => {
  const dom = page(`
    <form id="checkout" aria-label="Checkout"></form>
    <input aria-label="Coupon" form="checkout">
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.deepEqual(
    Array.from(interactions(batch, "form")[0].parameters, (parameter) => parameter.name),
    ["coupon"],
  );
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    [],
  );
});

// GV-005. `hasAttribute("contenteditable")` is true for the string "false".
test("treats contenteditable as editable only when its value says so", async () => {
  const dom = page(`
    <div contenteditable="false" aria-label="Not editable"></div>
    <div contenteditable aria-label="Bare"></div>
    <div contenteditable="" aria-label="Empty"></div>
    <div contenteditable="true" aria-label="True"></div>
    <div contenteditable="plaintext-only" aria-label="Plaintext"></div>
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    ["Bare", "Empty", "True", "Plaintext"],
  );
});

// GV-030. A throw while building one interaction is isolated and counted.
test("reports a warning when a suppressed DOM read fails", async () => {
  const dom = page(`<input aria-label="Visible">`);
  const input = dom.window.document.querySelector("input");
  Object.defineProperty(input, "shadowRoot", {
    configurable: true,
    get() {
      throw new Error("shadow probe failed");
    },
  });
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.equal(batch.warnings[0].code, "element_extraction_failed");
  assert.equal(interactions(batch, "control")[0].name, "Visible");
});

test("reports a warning when an element cannot be extracted", async () => {
  const dom = page(`
    <input aria-label="Visible">
    <input aria-label="Hostile">
  `);
  const hostile = [...dom.window.document.querySelectorAll("input")].at(-1);
  hostile.matches = () => {
    throw new Error("hostile element");
  };

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    ["Visible"],
  );
  assert.equal(batch.warnings.length, 1);
  assert.equal(batch.warnings[0].code, "element_extraction_failed");
  assert.match(batch.warnings[0].message, /1 element/u);
});

test("records a css fallback and still extracts when many siblings compete", async () => {
  const buttons = Array.from({ length: 40 }, (_, index) => `<button>Go ${index}</button>`).join("");
  const dom = page(`<main>${buttons}</main>`);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const actions = interactions(batch, "action");
  assert.equal(actions.length, 40);
  for (const action of actions) {
    assert.ok(action.locators[0].css, "finder exhaustion must still leave a simpleSelector");
  }
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

// GV-032. The 80-character cap used to apply only when the name did not start
// with a digit, so numeric labels were unbounded.
test("caps parameter names that start with a digit", async () => {
  const label = `1${"a".repeat(90)}`;
  const dom = page(`<input aria-label="${label}">`);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const name = interactions(batch, "control")[0].parameters[0].name;
  assert.equal(name.length, 80);
  assert.match(name, /^value1/u);
});

// GV-032. Bounding by UTF-16 code units can split a surrogate pair. 255 BMP
// characters plus an emoji is 256 code points and 257 UTF-16 units; the emoji
// must survive.
test("does not split a surrogate pair when bounding page text", async () => {
  const label = `${"x".repeat(255)}\u{1F600}`;
  const dom = page(`<input aria-label="${label}">`);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.equal(interactions(batch, "control")[0].name, label);
});

// GV-032. `explicitRole` already maps role="search"; a second branch was
// unreachable. The form still records search rather than falling through to
// the "form" default.
test("records an explicit search role on a form", async () => {
  const dom = page(`<form role="search" aria-label="Find"><input aria-label="Query"></form>`);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.equal(interactions(batch, "form")[0].role, "search");
});

test("omits inert, disabled, and readonly controls", async () => {
  const dom = page(`
    <div inert><input aria-label="Inert field"></div>
    <input aria-label="Disabled field" disabled>
    <fieldset disabled><input aria-label="Fieldset disabled"></fieldset>
    <input aria-label="Readonly field" readonly>
    <input aria-label="Aria readonly" aria-readonly="true">
    <input aria-label="Live field">
  `);

  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    ["Live field"],
  );
});

test("omits file inputs instead of emitting a string fill", async () => {
  const dom = page(`
    <form aria-label="Resume"><input type="file" aria-label="Resume"></form>
    <input type="file" aria-label="Standalone file">
    <input aria-label="Visible">
  `);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  assert.equal(interactions(batch, "form")[0].parameters.length, 0);
  assert.deepEqual(
    Array.from(interactions(batch, "control"), (item) => item.name),
    ["Visible"],
  );
});

test("treats button-like inputs as actions, never fill parameters", async () => {
  const dom = page(`
    <form aria-label="Checkout">
      <label for="email">Email</label>
      <input id="email" type="email" required>
      <input type="submit" value="Place order">
    </form>
    <input type="reset" value="Reset standalone">
  `);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const form = interactions(batch, "form")[0];
  assert.deepEqual(
    Array.from(form.parameters, (parameter) => parameter.name),
    ["email"],
  );
  assert.equal(
    form.actions.some((action) => action.kind === "click"),
    false,
  );
  const names = interactions(batch, "action").map((item) => item.name);
  assert.equal(names.includes("Place order"), true);
  assert.equal(names.includes("Reset standalone"), true);
  assert.equal(
    interactions(batch, "control").some((item) => item.parameters?.[0]?.name === "checkoutButton"),
    false,
  );
});

test("form tools are fill-only on a two-submit form", async () => {
  const dom = page(`
    <form aria-label="Login">
      <label for="user">User</label>
      <input id="user" required>
      <label for="note">Note</label>
      <input id="note">
      <button type="submit">Save</button>
      <button type="submit">Save and continue</button>
    </form>
  `);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const form = interactions(batch, "form")[0];
  assert.deepEqual(
    Array.from(form.parameters, (parameter) => parameter.name),
    ["user", "note"],
  );
  assert.equal(form.actions.some((action) => action.kind === "click"), false);
  assert.deepEqual(
    Array.from(interactions(batch, "action"), (item) => item.name),
    ["Save", "Save and continue"],
  );
});

test("unique CSS among sibling shadow hosts", async () => {
  const html = await readFile(resolve(repositoryRoot, "testdata", "corpus", "sibling-shadow.html"), "utf8");
  const dom = page(html);
  const upgrade = (root) => {
    for (const host of [...root.querySelectorAll("*")]) {
      if (host.shadowRoot) continue;
      const template = [...host.children].find(
        (child) => child.localName === "template" && child.hasAttribute("shadowrootmode"),
      );
      if (!template) continue;
      const shadow = host.attachShadow({ mode: "open" });
      shadow.append(template.content.cloneNode(true));
      template.remove();
      upgrade(shadow);
    }
  };
  upgrade(dom.window.document);
  const batch = await dom.window.__GEOVISOR_EXTRACT__();
  const alpha = interactions(batch, "action").find((item) => item.name === "Alpha");
  const beta = interactions(batch, "action").find((item) => item.name === "Beta");
  assert.ok(alpha && beta);
  const alphaHost = alpha.locators[0].shadowPath.at(-1).css;
  const betaHost = beta.locators[0].shadowPath.at(-1).css;
  assert.notEqual(alphaHost, betaHost);
  assert.match(alphaHost, /nth-of-type/u);
  assert.match(betaHost, /nth-of-type/u);
});

// jsdom 30 does not implement exclusive <details name>. The toggle handler
// here simulates that accordion so restore must reopen siblings it did not open.
test("safe exploration restores every observed details open state", async () => {
  const html = `
    <details open><summary>One</summary><button>First</button></details>
    <details><summary>Two</summary><button>Second</button></details>
  `;
  const dom = page(html, (window) => {
    hideClosedDetailsContent(window);
    window.document.querySelectorAll("details").forEach((details) => {
      details.addEventListener("toggle", () => {
        if (!details.open) return;
        window.document.querySelectorAll("details").forEach((other) => {
          if (other !== details) other.open = false;
        });
      });
    });
  });
  await dom.window.__GEOVISOR_EXTRACT__({
    safeExplore: true,
    maxDepth: 1,
    maxOperations: 1,
  });
  const details = dom.window.document.querySelectorAll("details");
  assert.equal(details[0].open, true);
  assert.equal(details[1].open, false);
});

test("omits citation, DOI, RFC, and unnamed cite links", async () => {
  const html = await readFile(resolve(repositoryRoot, "testdata", "corpus", "links.html"), "utf8");
  const batch = await page(html).window.__GEOVISOR_EXTRACT__();
  const actions = interactions(batch, "action");
  assert.deepEqual(
    Array.from(actions, (item) => item.name).sort(),
    ["CSS", "HTML", "Save"],
  );
  assert.equal(
    actions.some((item) => /^\[\d+\]$/u.test(item.name) || item.name.includes("10.1000") || item.name.startsWith("RFC")),
    false,
  );
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
