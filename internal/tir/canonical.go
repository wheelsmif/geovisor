package tir

import (
	"encoding/json"
	"fmt"
	"io"
)

// Canonical returns a cloned, normalized, deduplicated, and validated document
// without marshaling. It does not reorder collections: the producer owns
// tool, parameter, action, enum, warning, frame, and provenance order.
// Caller-owned data is left unchanged.
func Canonical(document *Document) (*Document, error) {
	cloned := Clone(document)
	if cloned == nil {
		return nil, invalid("", "nil_document", "document must not be nil")
	}
	cloned.Normalize()
	canonicalize(cloned)
	if err := cloned.Validate(); err != nil {
		return nil, err
	}
	return cloned, nil
}

// Marshal returns compact canonical JSON without a trailing newline. It clones,
// normalizes, deduplicates, and validates the document, leaving caller-owned
// data unchanged. It does not sort.
func Marshal(document *Document) ([]byte, error) {
	cloned, err := Canonical(document)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(cloned)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical TIR: %w", err)
	}
	return data, nil
}

// Write writes exactly the bytes returned by Marshal, with no trailing newline.
func Write(writer io.Writer, document *Document) error {
	if writer == nil {
		return fmt.Errorf("write canonical TIR: nil writer")
	}
	data, err := Marshal(document)
	if err != nil {
		return err
	}
	written, err := writer.Write(data)
	if err != nil {
		return fmt.Errorf("write canonical TIR: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("write canonical TIR: %w", io.ErrShortWrite)
	}
	return nil
}

// Clone returns a deep copy of document.
func Clone(document *Document) *Document {
	if document == nil {
		return nil
	}
	clone := *document
	clone.FrameCoverage.Frames = cloneCoveredFrames(document.FrameCoverage.Frames)
	clone.FrameCoverage.Uncovered = cloneUncoveredFrames(document.FrameCoverage.Uncovered)
	clone.Tools = make([]Tool, len(document.Tools))
	for i := range document.Tools {
		clone.Tools[i] = cloneTool(document.Tools[i])
	}
	clone.Warnings = make([]Warning, len(document.Warnings))
	for i, warning := range document.Warnings {
		clone.Warnings[i] = warning
		clone.Warnings[i].FramePath = cloneFrameReferences(warning.FramePath)
	}
	return &clone
}

func canonicalize(document *Document) {
	for i := range document.Tools {
		canonicalizeTool(&document.Tools[i])
	}
}

func canonicalizeTool(tool *Tool) {
	tool.Provenance = canonicalProvenance(tool.Provenance)
	for i := range tool.Parameters {
		canonicalizeParameter(&tool.Parameters[i])
	}
	for i := range tool.Locators {
		tool.Locators[i].Provenance = canonicalProvenance(tool.Locators[i].Provenance)
	}
}

func canonicalizeParameter(parameter *Parameter) {
	parameter.Enum = uniqueInOrder(parameter.Enum)
	if parameter.Items != nil {
		canonicalizeShape(parameter.Items)
	}
	for i := range parameter.Properties {
		canonicalizeShape(&parameter.Properties[i].Shape)
	}
}

func canonicalizeShape(shape *ParameterShape) {
	shape.Enum = uniqueInOrder(shape.Enum)
	if shape.Items != nil {
		canonicalizeShape(shape.Items)
	}
	for i := range shape.Properties {
		canonicalizeShape(&shape.Properties[i].Shape)
	}
}

// canonicalProvenance returns a copy of items with duplicate keys removed,
// keeping the first occurrence. It does not mutate the argument and does not
// sort: the producer chooses provenance order.
func canonicalProvenance(items []Provenance) []Provenance {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]Provenance, 0, len(items))
	for _, item := range items {
		key := provenanceKey(item)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

// uniqueInOrder returns a copy of values with duplicates removed, keeping the
// first occurrence. It does not mutate the argument and does not sort.
func uniqueInOrder(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func provenanceKey(provenance Provenance) string {
	return string(provenance.Kind) + "\x00" + provenance.Reference
}

func cloneCoveredFrames(frames []CoveredFrame) []CoveredFrame {
	result := make([]CoveredFrame, len(frames))
	for i, frame := range frames {
		result[i] = frame
		result[i].Path = cloneFrameReferences(frame.Path)
	}
	return result
}

func cloneUncoveredFrames(frames []UncoveredFrame) []UncoveredFrame {
	result := make([]UncoveredFrame, len(frames))
	for i, frame := range frames {
		result[i] = frame
		result[i].Path = cloneFrameReferences(frame.Path)
	}
	return result
}

func cloneFrameReferences(path []FrameReference) []FrameReference {
	return append([]FrameReference(nil), path...)
}

func cloneTool(tool Tool) Tool {
	result := tool
	result.Parameters = make([]Parameter, len(tool.Parameters))
	for i := range tool.Parameters {
		result.Parameters[i] = cloneParameter(tool.Parameters[i])
	}
	result.Locators = make([]LocatorCandidate, len(tool.Locators))
	for i, locator := range tool.Locators {
		result.Locators[i] = locator
		result.Locators[i].FramePath = clonePathNodes(locator.FramePath)
		result.Locators[i].ShadowPath = clonePathNodes(locator.ShadowPath)
		result.Locators[i].Provenance = append([]Provenance(nil), locator.Provenance...)
		if locator.Semantic != nil {
			semantic := *locator.Semantic
			semantic.Scope = append([]SemanticNode(nil), locator.Semantic.Scope...)
			result.Locators[i].Semantic = &semantic
		}
	}
	result.Actions = make([]ActionBinding, len(tool.Actions))
	for i, action := range tool.Actions {
		result.Actions[i] = action
		result.Actions[i].LocatorCandidateIDs = append([]string(nil), action.LocatorCandidateIDs...)
	}
	result.Provenance = append([]Provenance(nil), tool.Provenance...)
	return result
}

func cloneParameter(parameter Parameter) Parameter {
	shape := cloneShape(ParameterShape{
		Type: parameter.Type, Enum: parameter.Enum,
		Items: parameter.Items, Properties: parameter.Properties,
	})
	parameter.Enum = shape.Enum
	parameter.Items = shape.Items
	parameter.Properties = shape.Properties
	return parameter
}

func cloneShape(shape ParameterShape) ParameterShape {
	result := shape
	result.Enum = append([]string(nil), shape.Enum...)
	if shape.Items != nil {
		items := cloneShape(*shape.Items)
		result.Items = &items
	}
	result.Properties = cloneProperties(shape.Properties)
	return result
}

func cloneProperties(properties []ParameterProperty) []ParameterProperty {
	result := make([]ParameterProperty, len(properties))
	for i, property := range properties {
		result[i] = property
		result[i].Shape = cloneShape(property.Shape)
	}
	return result
}

func clonePathNodes(nodes []PathNode) []PathNode {
	result := make([]PathNode, len(nodes))
	for i, node := range nodes {
		result[i] = node
		if node.Semantic != nil {
			semantic := *node.Semantic
			result[i].Semantic = &semantic
		}
	}
	return result
}
