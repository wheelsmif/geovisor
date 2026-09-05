package tir

import (
	"fmt"
	"math"
	"strings"
)

func validateDocument(d *Document) error {
	if d == nil {
		return invalid("", "nil_document", "document must not be nil")
	}
	if d.SchemaVersion != SchemaVersion {
		return invalid("schemaVersion", "unsupported_version", fmt.Sprintf("must be %q", SchemaVersion))
	}
	if !validSourceKind(d.Source.Kind) {
		return invalid("source.kind", "invalid_source_kind", "must be launch_url or cdp_attach")
	}
	if d.Source.ExecutionBoundary != ExecutionAgentOwned {
		return invalid("source.executionBoundary", "invalid_execution_boundary", "must be agent_owned")
	}
	if err := validateCoverage(d.FrameCoverage); err != nil {
		return err
	}

	toolIDs := make(map[string]struct{}, len(d.Tools))
	locatorIDs := make(map[string]struct{})
	for ti := range d.Tools {
		tool := &d.Tools[ti]
		path := fmt.Sprintf("tools[%d]", ti)
		if strings.TrimSpace(tool.ID) == "" {
			return invalid(path+".id", "required", "must not be empty")
		}
		if _, exists := toolIDs[tool.ID]; exists {
			return invalid(path+".id", "duplicate_tool_id", "must be unique")
		}
		toolIDs[tool.ID] = struct{}{}
		if strings.TrimSpace(tool.Name) == "" {
			return invalid(path+".name", "required", "must not be empty")
		}
		if err := validateConfidence(path+".confidence", tool.Confidence); err != nil {
			return err
		}
		if err := validateProvenance(path+".provenance", tool.Provenance); err != nil {
			return err
		}

		parameterNames := make(map[string]struct{}, len(tool.Parameters))
		for pi := range tool.Parameters {
			parameterPath := fmt.Sprintf("%s.parameters[%d]", path, pi)
			parameter := &tool.Parameters[pi]
			if strings.TrimSpace(parameter.Name) == "" {
				return invalid(parameterPath+".name", "required", "must not be empty")
			}
			if _, exists := parameterNames[parameter.Name]; exists {
				return invalid(parameterPath+".name", "duplicate_parameter_name", "must be unique within a tool")
			}
			parameterNames[parameter.Name] = struct{}{}
			if err := validateParameter(parameterPath, parameter); err != nil {
				return err
			}
		}

		localLocators := make(map[string]struct{}, len(tool.Locators))
		for li := range tool.Locators {
			locatorPath := fmt.Sprintf("%s.locatorCandidates[%d]", path, li)
			locator := &tool.Locators[li]
			if strings.TrimSpace(locator.ID) == "" {
				return invalid(locatorPath+".id", "required", "must not be empty")
			}
			if _, exists := locatorIDs[locator.ID]; exists {
				return invalid(locatorPath+".id", "duplicate_locator_id", "must be globally unique")
			}
			locatorIDs[locator.ID] = struct{}{}
			localLocators[locator.ID] = struct{}{}
			if locator.Semantic == nil && strings.TrimSpace(locator.CSSFallback) == "" {
				return invalid(locatorPath, "missing_locator_strategy", "semantic or cssFallback is required")
			}
			if locator.Semantic != nil {
				if strings.TrimSpace(locator.Semantic.Role) == "" {
					return invalid(locatorPath+".semantic.role", "required", "must not be empty")
				}
				for si, node := range locator.Semantic.Scope {
					if strings.TrimSpace(node.Role) == "" {
						return invalid(fmt.Sprintf("%s.semantic.scope[%d].role", locatorPath, si), "required", "must not be empty")
					}
				}
			}
			if err := validatePathNodes(locatorPath+".framePath", locator.FramePath); err != nil {
				return err
			}
			if err := validatePathNodes(locatorPath+".shadowPath", locator.ShadowPath); err != nil {
				return err
			}
			if err := validateConfidence(locatorPath+".confidence", locator.Confidence); err != nil {
				return err
			}
			if err := validateProvenance(locatorPath+".provenance", locator.Provenance); err != nil {
				return err
			}
		}

		for ai := range tool.Actions {
			actionPath := fmt.Sprintf("%s.actionBindings[%d]", path, ai)
			action := &tool.Actions[ai]
			if !validActionKind(action.Action) {
				return invalid(actionPath+".action", "invalid_action_kind", "contains an unsupported value")
			}
			if action.InputParameter != "" {
				if _, exists := parameterNames[action.InputParameter]; !exists {
					return invalid(actionPath+".inputParameter", "unknown_parameter", "must name a parameter on the same tool")
				}
			}
			seenRefs := make(map[string]struct{}, len(action.LocatorCandidateIDs))
			for ri, id := range action.LocatorCandidateIDs {
				if _, exists := localLocators[id]; !exists {
					return invalid(fmt.Sprintf("%s.locatorCandidateIds[%d]", actionPath, ri), "unknown_locator", "must reference a locator on the same tool")
				}
				if _, exists := seenRefs[id]; exists {
					return invalid(fmt.Sprintf("%s.locatorCandidateIds[%d]", actionPath, ri), "duplicate_locator_reference", "must be unique")
				}
				seenRefs[id] = struct{}{}
			}
			if !validSideEffectClass(action.SideEffect.Class) {
				return invalid(actionPath+".sideEffect.class", "invalid_side_effect", "contains an unsupported value")
			}
			if action.SideEffect.SafeForExploration &&
				(action.SideEffect.Class == SideEffectNavigation || action.SideEffect.Class == SideEffectSubmission) {
				return invalid(actionPath+".sideEffect", "unsafe_exploration", "navigation and submission cannot be safe for exploration")
			}
		}
	}

	for wi := range d.Warnings {
		warning := &d.Warnings[wi]
		path := fmt.Sprintf("warnings[%d]", wi)
		if strings.TrimSpace(warning.Code) == "" {
			return invalid(path+".code", "required", "must not be empty")
		}
		if strings.TrimSpace(warning.Message) == "" {
			return invalid(path+".message", "required", "must not be empty")
		}
		if warning.ToolID != "" {
			if _, exists := toolIDs[warning.ToolID]; !exists {
				return invalid(path+".toolId", "unknown_tool", "must reference an existing tool")
			}
		}
		if err := validateFramePath(path+".framePath", warning.FramePath); err != nil {
			return err
		}
	}
	return nil
}

func validateCoverage(coverage FrameCoverage) error {
	switch coverage.Status {
	case CoverageComplete:
		if len(coverage.Uncovered) != 0 {
			return invalid("frameCoverage.uncovered", "inconsistent_complete_coverage", "must be empty when coverage is complete")
		}
	case CoveragePartial:
		if len(coverage.Uncovered) == 0 {
			return invalid("frameCoverage.uncovered", "missing_partial_coverage", "must identify at least one uncovered frame")
		}
	case CoverageUnavailable:
		if len(coverage.Frames) != 0 || len(coverage.Uncovered) != 0 {
			return invalid("frameCoverage", "inconsistent_unavailable_coverage", "must not contain frames when coverage is unavailable")
		}
	default:
		return invalid("frameCoverage.status", "invalid_coverage_status", "contains an unsupported value")
	}

	paths := make(map[string]string, len(coverage.Frames)+len(coverage.Uncovered))
	for i := range coverage.Frames {
		path := fmt.Sprintf("frameCoverage.frames[%d].path", i)
		if err := validateFramePath(path, coverage.Frames[i].Path); err != nil {
			return err
		}
		key := framePathKey(coverage.Frames[i].Path)
		if previous, exists := paths[key]; exists {
			return invalid(path, "duplicate_frame_path", "duplicates "+previous)
		}
		paths[key] = path
	}
	for i := range coverage.Uncovered {
		path := fmt.Sprintf("frameCoverage.uncovered[%d]", i)
		if err := validateFramePath(path+".path", coverage.Uncovered[i].Path); err != nil {
			return err
		}
		if strings.TrimSpace(coverage.Uncovered[i].Reason) == "" {
			return invalid(path+".reason", "required", "must not be empty")
		}
		key := framePathKey(coverage.Uncovered[i].Path)
		if previous, exists := paths[key]; exists {
			return invalid(path+".path", "duplicate_frame_path", "duplicates "+previous)
		}
		paths[key] = path + ".path"
	}
	return nil
}

func validateFramePath(field string, path []FrameReference) error {
	for i, frame := range path {
		if frame.Index < 0 {
			return invalid(fmt.Sprintf("%s[%d].index", field, i), "invalid_frame_index", "must be nonnegative")
		}
	}
	return nil
}

func validatePathNodes(field string, nodes []PathNode) error {
	for i, node := range nodes {
		path := fmt.Sprintf("%s[%d]", field, i)
		if node.Semantic == nil && strings.TrimSpace(node.CSSFallback) == "" {
			return invalid(path, "missing_path_strategy", "semantic or cssFallback is required")
		}
		if node.Semantic != nil && strings.TrimSpace(node.Semantic.Role) == "" {
			return invalid(path+".semantic.role", "required", "must not be empty")
		}
	}
	return nil
}

func validateParameter(path string, parameter *Parameter) error {
	if !validValueType(parameter.Type) {
		return invalid(path+".type", "invalid_value_type", "contains an unsupported value")
	}
	if err := validateUniqueStrings(path+".enum", parameter.Enum); err != nil {
		return err
	}
	if parameter.Items != nil {
		if err := validateShape(path+".items", parameter.Items); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(parameter.Properties))
	for i := range parameter.Properties {
		propertyPath := fmt.Sprintf("%s.properties[%d]", path, i)
		property := &parameter.Properties[i]
		if strings.TrimSpace(property.Name) == "" {
			return invalid(propertyPath+".name", "required", "must not be empty")
		}
		if _, exists := seen[property.Name]; exists {
			return invalid(propertyPath+".name", "duplicate_property_name", "must be unique")
		}
		seen[property.Name] = struct{}{}
		if err := validateShape(propertyPath+".shape", &property.Shape); err != nil {
			return err
		}
	}
	return nil
}

func validateShape(path string, shape *ParameterShape) error {
	if !validValueType(shape.Type) {
		return invalid(path+".type", "invalid_value_type", "contains an unsupported value")
	}
	if err := validateUniqueStrings(path+".enum", shape.Enum); err != nil {
		return err
	}
	if shape.Items != nil {
		if err := validateShape(path+".items", shape.Items); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(shape.Properties))
	for i := range shape.Properties {
		propertyPath := fmt.Sprintf("%s.properties[%d]", path, i)
		property := &shape.Properties[i]
		if strings.TrimSpace(property.Name) == "" {
			return invalid(propertyPath+".name", "required", "must not be empty")
		}
		if _, exists := seen[property.Name]; exists {
			return invalid(propertyPath+".name", "duplicate_property_name", "must be unique")
		}
		seen[property.Name] = struct{}{}
		if err := validateShape(propertyPath+".shape", &property.Shape); err != nil {
			return err
		}
	}
	return nil
}

func validateConfidence(path string, confidence Confidence) error {
	if math.IsNaN(confidence.Score) || math.IsInf(confidence.Score, 0) ||
		confidence.Score < 0 || confidence.Score > 1 {
		return invalid(path+".score", "invalid_confidence", "must be a finite number in [0, 1]")
	}
	return nil
}

func validateUniqueStrings(path string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		if _, exists := seen[value]; exists {
			return invalid(fmt.Sprintf("%s[%d]", path, i), "duplicate_enum_value", "must be unique")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateProvenance(path string, provenance []Provenance) error {
	for i, item := range provenance {
		switch item.Kind {
		case ProvenanceDOM, ProvenanceAccessibility, ProvenancePageMetadata, ProvenanceHeuristic:
		default:
			return invalid(fmt.Sprintf("%s[%d].kind", path, i), "invalid_provenance_kind", "contains an unsupported value")
		}
	}
	return nil
}

func validSourceKind(kind SourceKind) bool {
	return kind == SourceLaunchURL || kind == SourceCDPAttach
}

func validValueType(value ValueType) bool {
	switch value {
	case ValueString, ValueNumber, ValueInteger, ValueBoolean, ValueObject, ValueArray:
		return true
	default:
		return false
	}
}

func validActionKind(kind ActionKind) bool {
	switch kind {
	case ActionClick, ActionFill, ActionSelect, ActionCheck:
		return true
	default:
		return false
	}
}

func validSideEffectClass(class SideEffectClass) bool {
	switch class {
	case SideEffectNone, SideEffectLocalState, SideEffectNetwork,
		SideEffectNavigation, SideEffectSubmission, SideEffectUnknown:
		return true
	default:
		return false
	}
}

func framePathKey(path []FrameReference) string {
	var builder strings.Builder
	for _, frame := range path {
		fmt.Fprintf(&builder, "%d:%s:%s;", frame.Index, frame.Name, frame.Src)
	}
	return builder.String()
}

func invalid(field, code, message string) error {
	return &ValidationError{Field: field, Code: code, Message: message}
}
