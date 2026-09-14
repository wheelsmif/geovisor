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
  for (const name of requiredParameters(definition.inputSchema)) {
    if (!Object.prototype.hasOwnProperty.call(input, name)) {
      throw new Error(
        `GEO-Visor tool ${JSON.stringify(definition.name)} is missing required parameter ${JSON.stringify(name)}`,
      );
    }
  }
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

function requiredParameters(schema: unknown): string[] {
  if (!schema || typeof schema !== "object") return [];
  const required = (schema as { required?: unknown }).required;
  if (!Array.isArray(required)) return [];
  return required.filter((item): item is string => typeof item === "string");
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

function ownerView(element: Element): Window {
  const view = element.ownerDocument.defaultView;
  if (!view) throw new Error("resolved element has no window to dispatch events in");
  return view;
}

function prototypeName(element: Element): string {
  const tag = element.localName;
  if (tag === "textarea") return "HTMLTextAreaElement";
  if (tag === "select") return "HTMLSelectElement";
  if (tag === "input") return "HTMLInputElement";
  return "HTMLElement";
}

function setNativeProperty(element: Element, property: string, value: unknown): void {
  const view = ownerView(element) as unknown as Record<string, { prototype?: object } | undefined>;
  const proto = view[prototypeName(element)]?.prototype;
  const setter = proto
    ? Object.getOwnPropertyDescriptor(proto, property)?.set
    : undefined;
  if (setter) {
    setter.call(element, value);
    return;
  }
  (element as unknown as Record<string, unknown>)[property] = value;
}

function viewEvent(view: Window): new (type: string, init?: EventInit) => Event {
  return (view as unknown as { Event: new (type: string, init?: EventInit) => Event }).Event;
}

function dispatch(element: Element, name: string): void {
  const view = ownerView(element);
  const EventCtor = viewEvent(view);
  element.dispatchEvent(new EventCtor(name, { bubbles: true }));
}

function dispatchInput(element: Element): void {
  const view = ownerView(element);
  const InputEventCtor = (view as unknown as { InputEvent?: typeof InputEvent }).InputEvent;
  if (typeof InputEventCtor === "function") {
    element.dispatchEvent(new InputEventCtor("input", { bubbles: true }));
    return;
  }
  const EventCtor = viewEvent(view);
  element.dispatchEvent(new EventCtor("input", { bubbles: true }));
}

function notifyValueChange(element: Element): void {
  dispatchInput(element);
  dispatch(element, "change");
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
  const text = String(value);
  if ("value" in element) {
    setNativeProperty(element, "value", text);
  } else if (isContentEditable(element)) {
    element.textContent = text;
  } else {
    throw new Error("resolved element cannot accept text");
  }
  notifyValueChange(element);
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
  setNativeProperty(select, "selectedIndex", index);
  notifyValueChange(element);
}

function applyCheck(element: Element, binding: ActionBinding, value: unknown): void {
  const input = element as HTMLInputElement;
  if (
    element.localName !== "input" ||
    !["checkbox", "radio"].includes((input.type || "").toLowerCase())
  ) {
    throw new Error("resolved element is not a checkbox or radio input");
  }
  setNativeProperty(input, "checked", binding.inputParameter ? Boolean(value) : true);
  notifyValueChange(element);
}
