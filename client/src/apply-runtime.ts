// Applies TIR action bindings against the current document.
//
// This is not a product emitter. GEO-Visor writes LLM API catalogs (MCP,
// OpenAI) plus bindings; an agent-owned executor interprets those recipes.
// Tests (and that executor) share this module with the extractor through
// client/src/shared/ so a locator the extractor records is one apply can
// resolve.

import { isContentEditable } from "./shared/role";
import { type LocatorCandidate, messageOf, resolveCandidate } from "./shared/locate";
import { optionIndexByLabel } from "./shared/option";

type ActionKind = "click" | "fill" | "select" | "check";

export interface ActionBinding {
  action: ActionKind;
  inputParameter?: string;
  locatorCandidateIds: string[];
}

export interface ToolDefinition {
  name: string;
  description?: string;
  inputSchema: unknown;
  actions: ActionBinding[];
  locators: LocatorCandidate[];
}

export async function executeTool(
  definition: ToolDefinition,
  input: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<void> {
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
      const element = resolve(definition, binding, input);
      const value = isFamilyTargetBinding(binding)
        ? undefined
        : binding.inputParameter
          ? input[binding.inputParameter]
          : undefined;
      apply(element, binding, value);
    } catch (error) {
      throw new Error(
        `GEO-Visor tool ${JSON.stringify(definition.name)} action[${index}] ` +
          `${JSON.stringify(binding.action)} failed: ${messageOf(error)}`,
      );
    }
  }
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

function isFamilyTargetBinding(binding: ActionBinding): boolean {
  return (
    binding.inputParameter === "target" &&
    (binding.action === "click" || binding.action === "check")
  );
}

function familyLocatorName(locator: LocatorCandidate | undefined): string {
  const name = locator?.semantic?.name;
  if (typeof name === "string") {
    const cleaned = name.replace(/\s+/gu, " ").trim();
    if (cleaned) return cleaned;
  }
  return "Control";
}

function familyDisplayNames(
  definition: ToolDefinition,
  locatorIDs: string[],
): Map<string, string> {
  const counts = new Map<string, number>();
  const names = new Map<string, string>();
  for (const id of locatorIDs) {
    const base = familyLocatorName(definition.locators.find((item) => item.id === id));
    const next = (counts.get(base) ?? 0) + 1;
    counts.set(base, next);
    names.set(id, next === 1 ? base : `${base} (${next})`);
  }
  return names;
}

function resolve(
  definition: ToolDefinition,
  binding: ActionBinding,
  input: Record<string, unknown>,
): Element {
  let locatorIDs = binding.locatorCandidateIds;
  if (isFamilyTargetBinding(binding)) {
    const target = String(input[binding.inputParameter ?? ""]);
    const names = familyDisplayNames(definition, locatorIDs);
    locatorIDs = locatorIDs.filter((id) => names.get(id) === target);
    if (locatorIDs.length === 0) {
      throw new Error("target does not match a locator");
    }
  }
  const failures: string[] = [];
  for (const locatorID of locatorIDs) {
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
      applyCheck(element, value);
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
 * values differ from its text; matching the label and setting `selectedIndex`
 * keeps one source of truth for both.
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

function applyCheck(element: Element, value: unknown): void {
  const input = element as HTMLInputElement;
  if (
    element.localName !== "input" ||
    !["checkbox", "radio"].includes((input.type || "").toLowerCase())
  ) {
    throw new Error("resolved element is not a checkbox or radio input");
  }
  setNativeProperty(input, "checked", value === undefined ? true : Boolean(value));
  notifyValueChange(element);
}
