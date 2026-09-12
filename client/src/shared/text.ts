// Text normalization shared by the extractor and the generated WebMCP runtime.
// Both sides must normalize identically or a name recorded at extraction time
// will not compare equal to the same name recomputed at execution time.

export const TEXT_LIMIT = 256;
export const DESCRIPTION_LIMIT = 512;

/**
 * Collapses whitespace, trims, and bounds a page-derived string.
 *
 * The limit is applied in code points rather than UTF-16 code units so a
 * surrogate pair is never split (GV-032).
 */
export function cleanText(value: string | null | undefined, limit = TEXT_LIMIT): string {
  if (!value) return "";
  const collapsed = value.replace(/\s+/gu, " ").trim();
  const points = Array.from(collapsed);
  return points.length <= limit ? collapsed : points.slice(0, limit).join("");
}

export function humanize(value: string): string {
  const cleaned = cleanText(value.replace(/[-_]+/gu, " "));
  return cleaned ? cleaned[0]!.toUpperCase() + cleaned.slice(1) : "Interaction";
}
