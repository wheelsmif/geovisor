export type InteractionKind = "form" | "control" | "action";
export type ValueType = "string" | "number" | "integer" | "boolean" | "object" | "array";
export type ActionKind = "click" | "fill" | "select" | "check";
export type SideEffectClass =
  | "none"
  | "local_state"
  | "network"
  | "navigation"
  | "submission"
  | "unknown";
export type EvidenceKind = "dom" | "accessibility" | "page_metadata" | "heuristic";

export interface FrameReference {
  index: number;
  name?: string;
  src?: string;
}

export interface Frame {
  path: FrameReference[];
  accessible: boolean;
  url?: string;
  origin?: string;
  reason?: string;
}

export interface SemanticNode {
  role: string;
  name?: string;
}

export interface ParameterShape {
  type: ValueType;
  enum: string[];
  items?: ParameterShape;
  properties: ParameterProperty[];
}

export interface ParameterProperty {
  name: string;
  description?: string;
  required: boolean;
  shape: ParameterShape;
  sourceOrder?: number;
}

export interface Parameter {
  name: string;
  description?: string;
  type: ValueType;
  required: boolean;
  enum: string[];
  items?: ParameterShape;
  properties: ParameterProperty[];
  sourceOrder?: number;
}

export interface PathNode {
  semantic?: SemanticNode;
  css?: string;
}

export interface SemanticLocator {
  scope: SemanticNode[];
  role: string;
  name?: string;
}

export interface Evidence {
  kind: EvidenceKind;
  reference: string;
  score: number;
}

export interface Locator {
  framePath: PathNode[];
  shadowPath: PathNode[];
  semantic?: SemanticLocator;
  css?: string;
  evidence: Evidence[];
}

export interface SideEffect {
  class: SideEffectClass;
  safeForExploration: boolean;
  rationale?: string;
}

export interface Action {
  kind: ActionKind;
  inputParameter?: string;
  locatorIndexes: number[];
  sideEffect: SideEffect;
}

export interface Interaction {
  kind: InteractionKind;
  framePath: FrameReference[];
  scope: SemanticNode[];
  role: string;
  name: string;
  description?: string;
  parameters: Parameter[];
  locators: Locator[];
  actions: Action[];
  evidence: Evidence[];
}

export interface Batch {
  coverageReported: boolean;
  frames: Frame[];
  interactions: Interaction[];
}

export interface ExtractionOptions {
  safeExplore?: boolean;
  maxDepth?: number;
  maxOperations?: number;
  timeoutMs?: number;
}

export type ExtractFunction = (options?: ExtractionOptions) => Promise<Batch>;

declare global {
  // Stable CDP entry point installed by the browser bundle.
  var __GEOVISOR_EXTRACT__: ExtractFunction;
}
