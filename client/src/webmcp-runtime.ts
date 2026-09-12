// Entry point for the generated WebMCP module's runtime half.
//
// Built by client/scripts/build.mjs into internal/emitter/webmcp-runtime.js and
// embedded by internal/emitter/webmcp.go, mirroring how the extractor bundle is
// embedded by internal/payload. The emitter concatenates a capability guard, the
// tool definitions, this bundle, and a registration call.
//
// It shares role resolution, accessible-name computation, and element
// addressing with the extractor, so a locator the extractor can record is a
// locator this runtime can resolve.

import { isContentEditable } from "./shared/role";
import { type LocatorCandidate, messageOf, resolveCandidate } from "./shared/locate";
import { optionIndexByLabel } from "./shared/option";

type ActionKind = "click" | "fill" | "select" | "check";

interface ActionBinding {
  action: ActionKind;
  inputParameter?: string;
  locatorCandidateIds: string[];
}

interface ToolDefinition {
  name: string;
  description: string;
  inputSchema: unknown;
  annotations: unknown;
  actions: ActionBinding[];
  locators: LocatorCandidate[];
}

interface ToolRegistration {
  name: string;
  description: string;
  inputSchema: unknown;
  annotations: unknown;
  execute: (input?: Record<string, unknown>, options?: { signal?: AbortSignal }) => Promise<unknown>;
}

interface ModelContextDocument extends Document {
  modelContext: { registerTool: (registration: ToolRegistration) => Promise<void> | void };
}

export async function register(definitions: ToolDefinition[]): Promise<ToolRegistration[]> {
  const registrations: ToolRegistration[] = [];
  for (const definition of definitions) {
    const registration: ToolRegistration = {
      name: definition.name,
      description: definition.description,
      inputSchema: definition.inputSchema,
      annotations: definition.annotations,
      execute: async (input, options = {}) =>
        executeTool(definition, input ?? {}, options.signal),
    };
    await (document as ModelContextDocument).modelContext.registerTool(registration);
    registrations.push(registration);
  }
  return registrations;
}

async function executeTool(
  definition: ToolDefinition,
  input: Record<string, unknown>,
  signal: AbortSignal | undefined,
): Promise<unknown> {
  throwIfAborted(signal);
  for (let index = 0; index < definition.actions.length; index += 1) {
    throwIfAborted(signal);
    const binding = definition.actions[index]!;
    if (
      binding.inputParameter &&
      !Object.prototype.hasOwnProperty.call(input, binding.inputParameter)
    ) {
      continue;
    }
    try {
      const element = resolve(definition, binding);
      const value = binding.inputParameter ? input[binding.inputParameter] : undefined;
      apply(element, binding, value);
    } catch (error) {
      throw new Error(
        `GEO-Visor tool ${JSON.stringify(definition.name)} action[${index}] ` +
          `${JSON.stringify(binding.action)} failed: ${messageOf(error)}`,
      );
    }
  }
  return { content: [{ type: "text", text: `Executed ${definition.name}` }] };
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (signal?.aborted) {
    throw new DOMException("Tool execution was canceled", "AbortError");
  }
}

function resolve(definition: ToolDefinition, binding: ActionBinding): Element {
  const failures: string[] = [];
  for (const locatorID of binding.locatorCandidateIds) {
    const candidate = definition.locators.find((item) => item.id === locatorID);
    if (!candidate) {
      failures.push(`${locatorID}: locator definition is missing`);
      continue;
    }
    try {
      return resolveCandidate(candidate);
    } catch (error) {
      failures.push(`${locatorID}: ${messageOf(error)}`);
    }
  }
  throw new Error(`all locator candidates failed (${failures.join(" | ")})`);
}

function dispatch(element: Element, name: string): void {
  const view = element.ownerDocument.defaultView;
  if (!view) throw new Error("resolved element has no window to dispatch events in");
  element.dispatchEvent(new view.Event(name, { bubbles: true }));
}

function apply(element: Element, binding: ActionBinding, value: unknown): void {
  switch (binding.action) {
    case "click":
      applyClick(element);
      return;
    case "fill":
      applyFill(element, value);
      return;
    case "select":
      applySelect(element, value);
      return;
    case "check":
      applyCheck(element, binding, value);
      return;
    default: {
      const unsupported: never = binding.action;
      throw new Error(`unsupported action ${JSON.stringify(unsupported)}`);
    }
  }
}

function applyClick(element: Element): void {
  const clickable = element as Partial<HTMLElement>;
  if (typeof clickable.click !== "function") {
    throw new Error("resolved element is not clickable");
  }
  clickable.click();
}

function applyFill(element: Element, value: unknown): void {
  if ("value" in element) {
    (element as HTMLInputElement).value = String(value);
  } else if (isContentEditable(element)) {
    element.textContent = String(value);
  } else {
    throw new Error("resolved element cannot accept text");
  }
  dispatch(element, "input");
  dispatch(element, "change");
}

/**
 * Selects the option whose label matches the advertised enum value.
 *
 * The extractor advertises visible option labels, never option values, because
 * option values are page data that must not leave the page. Assigning the
 * advertised label to `element.value` therefore fails on every <select> whose
 * values differ from its text (GV-001); matching the label and setting
 * `selectedIndex` keeps one source of truth for both.
 */
function applySelect(element: Element, value: unknown): void {
  if (element.localName !== "select") {
    throw new Error("resolved element is not a select");
  }
  const select = element as HTMLSelectElement;
  const wanted = String(value);
  const index = optionIndexByLabel(select, wanted);
  if (index < 0) {
    throw new Error(`no option labelled ${JSON.stringify(wanted)} exists`);
  }
  select.selectedIndex = index;
  dispatch(element, "input");
  dispatch(element, "change");
}

function applyCheck(element: Element, binding: ActionBinding, value: unknown): void {
  const input = element as HTMLInputElement;
  if (
    element.localName !== "input" ||
    !["checkbox", "radio"].includes((input.type || "").toLowerCase())
  ) {
    throw new Error("resolved element is not a checkbox or radio input");
  }
  input.checked = binding.inputParameter ? Boolean(value) : true;
  dispatch(element, "input");
  dispatch(element, "change");
}
