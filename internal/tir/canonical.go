package tir

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Marshal returns compact canonical JSON without a trailing newline. It clones,
// normalizes, orders, and validates the document, leaving caller-owned data
// unchanged.
func Marshal(document *Document) ([]byte, error) {
	cloned := Clone(document)
	if cloned == nil {
		return nil, invalid("", "nil_document", "document must not be nil")
	}
	cloned.Normalize()
	canonicalize(cloned)
	if err := cloned.Validate(); err != nil {
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
	sort.Slice(document.FrameCoverage.Frames, func(i, j int) bool {
		return framePathKey(document.FrameCoverage.Frames[i].Path) <
			framePathKey(document.FrameCoverage.Frames[j].Path)
	})
	sort.Slice(document.FrameCoverage.Uncovered, func(i, j int) bool {
		return framePathKey(document.FrameCoverage.Uncovered[i].Path) <
			framePathKey(document.FrameCoverage.Uncovered[j].Path)
	})
	sort.Slice(document.Tools, func(i, j int) bool {
		return document.Tools[i].ID < document.Tools[j].ID
	})
	sort.Slice(document.Warnings, func(i, j int) bool {
		left, right := document.Warnings[i], document.Warnings[j]
		return warningKey(left) < warningKey(right)
	})
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
	sort.SliceStable(tool.Actions, func(i, j int) bool {
		return actionKey(tool.Actions[i]) < actionKey(tool.Actions[j])
	})
}

func canonicalizeParameter(parameter *Parameter) {
	parameter.Enum = sortedUnique(parameter.Enum)
	if parameter.Items != nil {
		canonicalizeShape(parameter.Items)
	}
	for i := range parameter.Properties {
		canonicalizeShape(&parameter.Properties[i].Shape)
	}
}

func canonicalizeShape(shape *ParameterShape) {
	shape.Enum = sortedUnique(shape.Enum)
	if shape.Items != nil {
		canonicalizeShape(shape.Items)
	}
	for i := range shape.Properties {
		canonicalizeShape(&shape.Properties[i].Shape)
	}
}

func canonicalProvenance(items []Provenance) []Provenance {
	sort.Slice(items, func(i, j int) bool {
		return provenanceKey(items[i]) < provenanceKey(items[j])
	})
	result := items[:0]
	for _, item := range items {
		if len(result) == 0 || provenanceKey(result[len(result)-1]) != provenanceKey(item) {
			result = append(result, item)
		}
	}
	return result
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func warningKey(warning Warning) string {
	return warning.Code + "\x00" + warning.ToolID + "\x00" +
		framePathKey(warning.FramePath) + "\x00" + warning.Message
}

func actionKey(action ActionBinding) string {
	key := string(action.Action) + "\x00" + action.InputParameter + "\x00" +
		string(action.SideEffect.Class) + "\x00"
	for _, id := range action.LocatorCandidateIDs {
		key += id + "\x00"
	}
	return key + action.SideEffect.Rationale
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
	result := parameter
	result.Enum = append([]string(nil), parameter.Enum...)
	if parameter.Items != nil {
		items := cloneShape(*parameter.Items)
		result.Items = &items
	}
	result.Properties = cloneProperties(parameter.Properties)
	return result
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
