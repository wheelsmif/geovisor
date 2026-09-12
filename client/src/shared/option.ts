// The single <select> option-label implementation (GV-001).
//
// The extractor advertises visible option labels rather than option values,
// because option values are page data that must not leave the page. The runtime
// must therefore find an option *by the same label it was advertised under*,
// which only works if both sides derive that label here.

import { cleanText } from "./text";

export function optionLabel(option: HTMLOptionElement): string {
  return cleanText(option.label || option.textContent);
}

/** The advertised enum: deduplicated labels in document order. */
export function optionLabels(element: HTMLSelectElement): string[] {
  const seen = new Set<string>();
  const labels: string[] = [];
  for (const option of Array.from(element.options)) {
    const label = optionLabel(option);
    if (label && !seen.has(label)) {
      seen.add(label);
      labels.push(label);
    }
  }
  return labels;
}

/**
 * Finds the index of the first option carrying `label`, or -1.
 *
 * First-match resolution mirrors the deduplication in `optionLabels`: when two
 * options share a label only one is advertised, so only one can be selected.
 */
export function optionIndexByLabel(element: HTMLSelectElement, label: string): number {
  return Array.from(element.options).findIndex((option) => optionLabel(option) === label);
}
