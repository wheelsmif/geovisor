// Package observation defines browser-independent facts consumed by the TIR
// compiler. Browser adapters may populate these values, but the package does
// not depend on a browser library or an output format.
package observation

// SourceKind identifies how the observing agent obtained the page.
type SourceKind string

const (
	SourceLaunchURL SourceKind = "launch_url"
	SourceCDPAttach SourceKind = "cdp_attach"
)

// Source describes the observed browser session.
type Source struct {
	Kind           SourceKind `json:"kind"`
	RequestedURL   string     `json:"requestedUrl,omitempty"`
	FinalURL       string     `json:"finalUrl,omitempty"`
	StealthEnabled bool       `json:"stealthEnabled"`
}

// Batch is one independently collected set of page facts. Batch and slice
// order are not semantic unless a field explicitly says otherwise.
type Batch struct {
	CoverageReported bool          `json:"coverageReported"`
	Frames           []Frame       `json:"frames"`
	Interactions     []Interaction `json:"interactions"`
}

// Frame records whether a discovered frame could be observed.
type Frame struct {
	Path       []FrameReference `json:"path"`
	Accessible bool             `json:"accessible"`
	URL        string           `json:"url,omitempty"`
	Origin     string           `json:"origin,omitempty"`
	Reason     string           `json:"reason,omitempty"`
}

// FrameReference identifies a frame at one level of the frame tree.
type FrameReference struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Src   string `json:"src,omitempty"`
}

// InteractionKind distinguishes semantic grouping boundaries. In particular,
// controls in different forms are not equivalent merely because their labels
// and roles match.
type InteractionKind string

const (
	InteractionForm    InteractionKind = "form"
	InteractionControl InteractionKind = "control"
	InteractionAction  InteractionKind = "action"
)

// Interaction is a raw semantic unit that compiles to one TIR tool. Scope and
// frame path participate in identity and prevent incorrectly merging controls
// from distinct parts of a page.
type Interaction struct {
	Kind        InteractionKind  `json:"kind"`
	FramePath   []FrameReference `json:"framePath"`
	Scope       []SemanticNode   `json:"scope"`
	Role        string           `json:"role"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Parameters  []Parameter      `json:"parameters"`
	Locators    []Locator        `json:"locators"`
	Actions     []Action         `json:"actions"`
	Evidence    []Evidence       `json:"evidence"`
}

// Parameter describes an input accepted by an interaction. SourceOrder is the
// only ordering hint preserved by the compiler; nil means unordered.
type Parameter struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Type        ValueType           `json:"type"`
	Required    bool                `json:"required"`
	Enum        []string            `json:"enum"`
	Items       *ParameterShape     `json:"items,omitempty"`
	Properties  []ParameterProperty `json:"properties"`
	SourceOrder *int                `json:"sourceOrder,omitempty"`
}

type ValueType string

const (
	ValueString  ValueType = "string"
	ValueNumber  ValueType = "number"
	ValueInteger ValueType = "integer"
	ValueBoolean ValueType = "boolean"
	ValueObject  ValueType = "object"
	ValueArray   ValueType = "array"
)

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
	SourceOrder *int           `json:"sourceOrder,omitempty"`
}

// Locator is a browser-library-neutral locator candidate. Action locator
// indexes refer to this interaction's Locators slice.
type Locator struct {
	FramePath  []PathNode       `json:"framePath"`
	ShadowPath []PathNode       `json:"shadowPath"`
	Semantic   *SemanticLocator `json:"semantic,omitempty"`
	CSS        string           `json:"css,omitempty"`
	Evidence   []Evidence       `json:"evidence"`
}

type PathNode struct {
	Semantic *SemanticNode `json:"semantic,omitempty"`
	CSS      string        `json:"css,omitempty"`
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

type ActionKind string

const (
	ActionClick  ActionKind = "click"
	ActionFill   ActionKind = "fill"
	ActionSelect ActionKind = "select"
	ActionCheck  ActionKind = "check"
)

type Action struct {
	Kind           ActionKind `json:"kind"`
	InputParameter string     `json:"inputParameter,omitempty"`
	LocatorIndexes []int      `json:"locatorIndexes"`
	SideEffect     SideEffect `json:"sideEffect"`
}

type SideEffectClass string

const (
	SideEffectNone       SideEffectClass = "none"
	SideEffectLocalState SideEffectClass = "local_state"
	SideEffectNetwork    SideEffectClass = "network"
	SideEffectNavigation SideEffectClass = "navigation"
	SideEffectSubmission SideEffectClass = "submission"
	SideEffectUnknown    SideEffectClass = "unknown"
)

type SideEffect struct {
	Class              SideEffectClass `json:"class"`
	SafeForExploration bool            `json:"safeForExploration"`
	Rationale          string          `json:"rationale,omitempty"`
}

type EvidenceKind string

const (
	EvidenceDOM           EvidenceKind = "dom"
	EvidenceAccessibility EvidenceKind = "accessibility"
	EvidencePageMetadata  EvidenceKind = "page_metadata"
	EvidenceHeuristic     EvidenceKind = "heuristic"
)

// Evidence contributes confidence and provenance. Score must be in [0, 1].
// Repeated evidence with the same kind and reference is aggregated once.
type Evidence struct {
	Kind      EvidenceKind `json:"kind"`
	Reference string       `json:"reference"`
	Score     float64      `json:"score"`
}
