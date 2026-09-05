// Package tir defines the emitter-agnostic Tool Intermediate Representation.
package tir

import (
	"errors"
	"fmt"
)

// SchemaVersion is the only TIR contract version emitted by this package.
const SchemaVersion = "1.0.0"

type SourceKind string

const (
	SourceLaunchURL SourceKind = "launch_url"
	SourceCDPAttach SourceKind = "cdp_attach"
)

type ExecutionBoundary string

const ExecutionAgentOwned ExecutionBoundary = "agent_owned"

type CoverageStatus string

const (
	CoverageComplete    CoverageStatus = "complete"
	CoveragePartial     CoverageStatus = "partial"
	CoverageUnavailable CoverageStatus = "unavailable"
)

type ValueType string

const (
	ValueString  ValueType = "string"
	ValueNumber  ValueType = "number"
	ValueInteger ValueType = "integer"
	ValueBoolean ValueType = "boolean"
	ValueObject  ValueType = "object"
	ValueArray   ValueType = "array"
)

type ActionKind string

const (
	ActionClick  ActionKind = "click"
	ActionFill   ActionKind = "fill"
	ActionSelect ActionKind = "select"
	ActionCheck  ActionKind = "check"
)

type SideEffectClass string

const (
	SideEffectNone       SideEffectClass = "none"
	SideEffectLocalState SideEffectClass = "local_state"
	SideEffectNetwork    SideEffectClass = "network"
	SideEffectNavigation SideEffectClass = "navigation"
	SideEffectSubmission SideEffectClass = "submission"
	SideEffectUnknown    SideEffectClass = "unknown"
)

type ProvenanceKind string

const (
	ProvenanceDOM           ProvenanceKind = "dom"
	ProvenanceAccessibility ProvenanceKind = "accessibility"
	ProvenancePageMetadata  ProvenanceKind = "page_metadata"
	ProvenanceHeuristic     ProvenanceKind = "heuristic"
)

// Document is the canonical deterministic TIR artifact. It intentionally has
// no creation time, run ID, or other wall-clock/random field.
type Document struct {
	SchemaVersion string         `json:"schemaVersion"`
	Source        SourceMetadata `json:"source"`
	FrameCoverage FrameCoverage  `json:"frameCoverage"`
	Tools         []Tool         `json:"tools"`
	Warnings      []Warning      `json:"warnings"`
}

type SourceMetadata struct {
	Kind              SourceKind        `json:"kind"`
	ExecutionBoundary ExecutionBoundary `json:"executionBoundary"`
	RequestedURL      string            `json:"requestedUrl,omitempty"`
	FinalURL          string            `json:"finalUrl,omitempty"`
	StealthEnabled    bool              `json:"stealthEnabled"`
}

type FrameCoverage struct {
	Status    CoverageStatus   `json:"status"`
	Frames    []CoveredFrame   `json:"frames"`
	Uncovered []UncoveredFrame `json:"uncovered"`
}

type CoveredFrame struct {
	Path   []FrameReference `json:"path"`
	URL    string           `json:"url,omitempty"`
	Origin string           `json:"origin,omitempty"`
}

type UncoveredFrame struct {
	Path   []FrameReference `json:"path"`
	Reason string           `json:"reason"`
}

type FrameReference struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Src   string `json:"src,omitempty"`
}

type Tool struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Parameters  []Parameter        `json:"parameters"`
	Locators    []LocatorCandidate `json:"locatorCandidates"`
	Actions     []ActionBinding    `json:"actionBindings"`
	Confidence  Confidence         `json:"confidence"`
	Provenance  []Provenance       `json:"provenance"`
}

// Parameter uses ordered property slices rather than maps so source order and
// JSON serialization are stable.
type Parameter struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Type        ValueType           `json:"type"`
	Required    bool                `json:"required"`
	Enum        []string            `json:"enum"`
	Items       *ParameterShape     `json:"items,omitempty"`
	Properties  []ParameterProperty `json:"properties"`
}

type ParameterShape struct {
	Type       ValueType           `json:"type"`
	Enum       []string            `json:"enum"`
	Items      *ParameterShape     `json:"items,omitempty"`
	Properties []ParameterProperty `json:"properties"`
}

type ParameterProperty struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required"`
	Shape       ParameterShape `json:"shape"`
}

// LocatorCandidate describes traversal and semantics without coupling TIR to
// any browser automation library's selector syntax.
type LocatorCandidate struct {
	ID          string           `json:"id"`
	FramePath   []PathNode       `json:"framePath"`
	ShadowPath  []PathNode       `json:"shadowPath"`
	Semantic    *SemanticLocator `json:"semantic,omitempty"`
	CSSFallback string           `json:"cssFallback,omitempty"`
	Confidence  Confidence       `json:"confidence"`
	Provenance  []Provenance     `json:"provenance"`
}

type PathNode struct {
	Semantic    *SemanticNode `json:"semantic,omitempty"`
	CSSFallback string        `json:"cssFallback,omitempty"`
}

type SemanticLocator struct {
	Scope []SemanticNode `json:"scope"`
	Role  string         `json:"role"`
	Name  string         `json:"name,omitempty"`
}

type SemanticNode struct {
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
}

type ActionBinding struct {
	Action              ActionKind `json:"action"`
	InputParameter      string     `json:"inputParameter,omitempty"`
	LocatorCandidateIDs []string   `json:"locatorCandidateIds"`
	SideEffect          SideEffect `json:"sideEffect"`
}

type SideEffect struct {
	Class              SideEffectClass `json:"class"`
	SafeForExploration bool            `json:"safeForExploration"`
	Rationale          string          `json:"rationale,omitempty"`
}

type Confidence struct {
	Score     float64 `json:"score"`
	Rationale string  `json:"rationale,omitempty"`
}

type Provenance struct {
	Kind      ProvenanceKind `json:"kind"`
	Reference string         `json:"reference,omitempty"`
}

type Warning struct {
	Code      string           `json:"code"`
	Message   string           `json:"message"`
	ToolID    string           `json:"toolId,omitempty"`
	FramePath []FrameReference `json:"framePath"`
}

// ValidationError is a machine-readable contract violation.
type ValidationError struct {
	Field   string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s (%s)", e.Field, e.Message, e.Code)
}

// NewDocument returns a minimal artifact with all collections initialized.
func NewDocument(source SourceMetadata) *Document {
	document := &Document{
		SchemaVersion: SchemaVersion,
		Source:        source,
		FrameCoverage: FrameCoverage{Status: CoverageUnavailable},
	}
	document.Normalize()
	return document
}

// Normalize recursively replaces nil collections with empty collections.
// Producers must call Normalize before marshaling documents not created by
// NewDocument. It preserves element order and never creates IDs or timestamps.
func (d *Document) Normalize() {
	if d.Tools == nil {
		d.Tools = []Tool{}
	}
	if d.Warnings == nil {
		d.Warnings = []Warning{}
	}
	if d.FrameCoverage.Frames == nil {
		d.FrameCoverage.Frames = []CoveredFrame{}
	}
	if d.FrameCoverage.Uncovered == nil {
		d.FrameCoverage.Uncovered = []UncoveredFrame{}
	}

	for i := range d.FrameCoverage.Frames {
		normalizeFramePath(&d.FrameCoverage.Frames[i].Path)
	}
	for i := range d.FrameCoverage.Uncovered {
		normalizeFramePath(&d.FrameCoverage.Uncovered[i].Path)
	}
	for i := range d.Warnings {
		normalizeFramePath(&d.Warnings[i].FramePath)
	}
	for i := range d.Tools {
		normalizeTool(&d.Tools[i])
	}
}

// Validate checks the complete in-memory contract, including cross-field
// invariants that JSON Schema cannot express.
func (d *Document) Validate() error {
	return validateDocument(d)
}

func normalizeTool(tool *Tool) {
	if tool.Parameters == nil {
		tool.Parameters = []Parameter{}
	}
	if tool.Locators == nil {
		tool.Locators = []LocatorCandidate{}
	}
	if tool.Actions == nil {
		tool.Actions = []ActionBinding{}
	}
	if tool.Provenance == nil {
		tool.Provenance = []Provenance{}
	}
	for i := range tool.Parameters {
		normalizeParameter(&tool.Parameters[i])
	}
	for i := range tool.Locators {
		locator := &tool.Locators[i]
		if locator.FramePath == nil {
			locator.FramePath = []PathNode{}
		}
		if locator.ShadowPath == nil {
			locator.ShadowPath = []PathNode{}
		}
		if locator.Provenance == nil {
			locator.Provenance = []Provenance{}
		}
		if locator.Semantic != nil && locator.Semantic.Scope == nil {
			locator.Semantic.Scope = []SemanticNode{}
		}
	}
	for i := range tool.Actions {
		if tool.Actions[i].LocatorCandidateIDs == nil {
			tool.Actions[i].LocatorCandidateIDs = []string{}
		}
	}
}

func normalizeParameter(parameter *Parameter) {
	if parameter.Enum == nil {
		parameter.Enum = []string{}
	}
	if parameter.Properties == nil {
		parameter.Properties = []ParameterProperty{}
	}
	if parameter.Items != nil {
		normalizeShape(parameter.Items)
	}
	for i := range parameter.Properties {
		normalizeShape(&parameter.Properties[i].Shape)
	}
}

func normalizeShape(shape *ParameterShape) {
	if shape.Enum == nil {
		shape.Enum = []string{}
	}
	if shape.Properties == nil {
		shape.Properties = []ParameterProperty{}
	}
	if shape.Items != nil {
		normalizeShape(shape.Items)
	}
	for i := range shape.Properties {
		normalizeShape(&shape.Properties[i].Shape)
	}
}

func normalizeFramePath(path *[]FrameReference) {
	if *path == nil {
		*path = []FrameReference{}
	}
}

// IsValidationError reports whether err is a TIR contract violation.
func IsValidationError(err error) bool {
	var validationErr *ValidationError
	return errors.As(err, &validationErr)
}
