// Package compiler deterministically converts raw page observations into the
// canonical Tool Intermediate Representation.
package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/tir"
)

// Input contains every observation batch for one browser session.
type Input struct {
	Source  observation.Source
	Batches []observation.Batch
}

// Error identifies malformed raw input at the compiler boundary.
type Error struct {
	Field   string
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s (%s)", e.Field, e.Message, e.Code)
}

type toolAccumulator struct {
	identity     string
	framePath    []observation.FrameReference
	role         string
	names        []string
	descriptions []string
	parameters   map[string]*parameterAccumulator
	locators     map[string]*locatorAccumulator
	actions      map[string]*actionAccumulator
	evidence     map[string]observation.Evidence
	warnings     []pendingWarning
}

type parameterAccumulator struct {
	name        string
	definitions map[string]tir.Parameter
	order       int
	hasOrder    bool
}

type locatorAccumulator struct {
	key      string
	locator  tir.LocatorCandidate
	evidence map[string]observation.Evidence
}

type actionAccumulator struct {
	action      tir.ActionBinding
	locatorKeys []string
	classes     map[tir.SideEffectClass]struct{}
	rationales  []string
	allSafe     bool
}

type frameAccumulator struct {
	path         []observation.FrameReference
	accessible   bool
	inaccessible bool
	urls         []string
	origins      []string
	reasons      []string
}

type pendingWarning struct {
	code      string
	message   string
	toolKey   string
	framePath []observation.FrameReference
}

type compiledTool struct {
	key  string
	tool tir.Tool
}

// Compile aggregates all batches without relying on input order. Equivalent
// evidence is deduplicated, while frame path and semantic scope remain part of
// interaction identity.
func Compile(input Input) (*tir.Document, error) {
	document := tir.NewDocument(tir.SourceMetadata{
		Kind:              tir.SourceKind(input.Source.Kind),
		ExecutionBoundary: tir.ExecutionAgentOwned,
		RequestedURL:      cleanText(input.Source.RequestedURL),
		FinalURL:          cleanText(input.Source.FinalURL),
		StealthEnabled:    input.Source.StealthEnabled,
	})

	tools := make(map[string]*toolAccumulator)
	frames := make(map[string]*frameAccumulator)
	var warnings []pendingWarning
	coverageReported := false

	for batchIndex, batch := range input.Batches {
		coverageReported = coverageReported || batch.CoverageReported
		for frameIndex, frame := range batch.Frames {
			field := fmt.Sprintf("batches[%d].frames[%d].path", batchIndex, frameIndex)
			if err := validateRawFramePath(field, frame.Path); err != nil {
				return nil, err
			}
			path := normalizeFrameReferences(frame.Path)
			key := observationFramePathKey(path)
			accumulator := frames[key]
			if accumulator == nil {
				accumulator = &frameAccumulator{path: path}
				frames[key] = accumulator
			}
			if frame.Accessible {
				accumulator.accessible = true
				accumulator.urls = appendNonempty(accumulator.urls, cleanText(frame.URL))
				accumulator.origins = appendNonempty(accumulator.origins, cleanText(frame.Origin))
			} else {
				accumulator.inaccessible = true
				reason := cleanText(frame.Reason)
				if reason == "" {
					reason = "frame inaccessible"
				}
				accumulator.reasons = append(accumulator.reasons, reason)
			}
		}

		for interactionIndex, interaction := range batch.Interactions {
			field := fmt.Sprintf("batches[%d].interactions[%d]", batchIndex, interactionIndex)
			if err := validateRawFramePath(field+".framePath", interaction.FramePath); err != nil {
				return nil, err
			}
			prepared, interactionWarnings, err := prepareInteraction(field, interaction)
			if err != nil {
				return nil, err
			}
			accumulator := tools[prepared.identity]
			if accumulator == nil {
				accumulator = &toolAccumulator{
					identity:   prepared.identity,
					framePath:  prepared.framePath,
					role:       prepared.role,
					parameters: make(map[string]*parameterAccumulator),
					locators:   make(map[string]*locatorAccumulator),
					actions:    make(map[string]*actionAccumulator),
					evidence:   make(map[string]observation.Evidence),
				}
				tools[prepared.identity] = accumulator
			}
			accumulator.names = append(accumulator.names, prepared.names...)
			accumulator.descriptions = append(accumulator.descriptions, prepared.descriptions...)
			mergeEvidence(accumulator.evidence, prepared.evidence)
			mergePreparedInteraction(accumulator, prepared)
			for _, warning := range interactionWarnings {
				warning.toolKey = accumulator.identity
				accumulator.warnings = append(accumulator.warnings, warning)
			}
		}
	}

	compileCoverage(document, coverageReported, frames, &warnings)
	compiled, toolIDs := compileTools(tools, &warnings)
	document.Tools = make([]tir.Tool, len(compiled))
	for i := range compiled {
		document.Tools[i] = compiled[i].tool
	}
	for _, accumulator := range tools {
		warnings = append(warnings, accumulator.warnings...)
	}
	document.Warnings = compileWarnings(warnings, toolIDs)
	document.Normalize()
	if err := document.Validate(); err != nil {
		return nil, fmt.Errorf("validate compiled TIR: %w", err)
	}
	return document, nil
}

type preparedInteraction struct {
	identity     string
	framePath    []observation.FrameReference
	role         string
	names        []string
	descriptions []string
	parameters   []preparedParameter
	locators     []preparedLocator
	actions      []preparedAction
	evidence     []observation.Evidence
}

type preparedParameter struct {
	name     string
	value    tir.Parameter
	key      string
	order    int
	hasOrder bool
}

type preparedLocator struct {
	key      string
	value    tir.LocatorCandidate
	evidence []observation.Evidence
}

type preparedAction struct {
	key         string
	value       tir.ActionBinding
	locatorKeys []string
}

func prepareInteraction(field string, source observation.Interaction) (preparedInteraction, []pendingWarning, error) {
	var result preparedInteraction
	var warnings []pendingWarning
	switch source.Kind {
	case observation.InteractionForm, observation.InteractionControl, observation.InteractionAction:
	default:
		return result, nil, &Error{
			Field: field + ".kind", Code: "invalid_interaction_kind",
			Message: "must be form, control, or action",
		}
	}
	result.framePath = normalizeFrameReferences(source.FramePath)
	result.role = canonicalText(source.Role)
	if result.role == "" {
		result.role = defaultRole(source.Kind)
	}
	name := cleanText(source.Name)
	if name == "" {
		name = fallbackName(result.role, source.Scope)
		warnings = append(warnings, pendingWarning{
			code:      "fallback_name",
			message:   "interaction name was derived from semantic context",
			framePath: result.framePath,
		})
	}
	result.names = []string{name}
	result.descriptions = appendNonempty(nil, cleanText(source.Description))

	for i, evidence := range source.Evidence {
		if err := validateEvidence(fmt.Sprintf("%s.evidence[%d]", field, i), evidence); err != nil {
			return result, nil, err
		}
		result.evidence = append(result.evidence, normalizeEvidence(evidence))
	}
	if len(result.evidence) == 0 {
		result.evidence = []observation.Evidence{fallbackEvidence()}
		warnings = append(warnings, pendingWarning{
			code:      "fallback_confidence",
			message:   "interaction confidence defaulted because no evidence was supplied",
			framePath: result.framePath,
		})
	}

	for i, parameter := range source.Parameters {
		compiled := compileParameter(parameter)
		if compiled.Name == "" {
			return result, nil, &Error{
				Field: fmt.Sprintf("%s.parameters[%d].name", field, i), Code: "required",
				Message: "parameter name must not be empty",
			}
		}
		data, _ := json.Marshal(compiled)
		prepared := preparedParameter{name: compiled.Name, value: compiled, key: string(data)}
		if parameter.SourceOrder != nil {
			prepared.order = *parameter.SourceOrder
			prepared.hasOrder = true
		}
		result.parameters = append(result.parameters, prepared)
	}

	locatorKeys := make([]string, len(source.Locators))
	for i, locator := range source.Locators {
		compiled, evidence, err := compileLocator(fmt.Sprintf("%s.locators[%d]", field, i), locator)
		if err != nil {
			return result, nil, err
		}
		data, _ := json.Marshal(struct {
			FramePath  []tir.PathNode
			ShadowPath []tir.PathNode
			Semantic   *tir.SemanticLocator
			CSS        string
		}{compiled.FramePath, compiled.ShadowPath, compiled.Semantic, compiled.CSSFallback})
		key := string(data)
		locatorKeys[i] = key
		result.locators = append(result.locators, preparedLocator{key: key, value: compiled, evidence: evidence})
		if compiled.Semantic == nil && compiled.CSSFallback != "" {
			warnings = append(warnings, pendingWarning{
				code: "css_fallback", message: "locator relies only on a CSS fallback",
				framePath: result.framePath,
			})
		}
		if len(locator.Evidence) == 0 {
			warnings = append(warnings, pendingWarning{
				code: "fallback_confidence", message: "locator confidence defaulted because no evidence was supplied",
				framePath: result.framePath,
			})
		}
	}

	for i, action := range source.Actions {
		selected := make([]string, 0, len(action.LocatorIndexes))
		if len(action.LocatorIndexes) == 0 {
			selected = append(selected, locatorKeys...)
		} else {
			for j, index := range action.LocatorIndexes {
				if index < 0 || index >= len(locatorKeys) {
					return result, nil, &Error{
						Field: fmt.Sprintf("%s.actions[%d].locatorIndexes[%d]", field, i, j),
						Code:  "invalid_locator_index", Message: "must reference a locator in the same interaction",
					}
				}
				selected = append(selected, locatorKeys[index])
			}
		}
		selected = sortedUniqueStrings(selected)
		compiled := tir.ActionBinding{
			Action:         tir.ActionKind(action.Kind),
			InputParameter: cleanText(action.InputParameter),
			SideEffect: tir.SideEffect{
				Class:              tir.SideEffectClass(action.SideEffect.Class),
				SafeForExploration: action.SideEffect.SafeForExploration,
				Rationale:          cleanText(action.SideEffect.Rationale),
			},
		}
		key := actionIdentity(compiled, selected)
		result.actions = append(result.actions, preparedAction{key: key, value: compiled, locatorKeys: selected})
		if len(selected) == 0 {
			warnings = append(warnings, pendingWarning{
				code: "missing_locator", message: "action has no locator candidates",
				framePath: result.framePath,
			})
		}
	}

	scope := normalizeSemanticNodes(source.Scope)
	identityParts := []string{
		string(source.Kind), observationFramePathKey(result.framePath),
		semanticNodesKey(scope), result.role, canonicalText(name),
	}
	if cleanText(source.Name) == "" {
		keys := append([]string(nil), locatorKeys...)
		sort.Strings(keys)
		identityParts = append(identityParts, strings.Join(keys, "\x1e"))
	}
	result.identity = joinedKey(identityParts...)
	return result, warnings, nil
}

func mergePreparedInteraction(target *toolAccumulator, source preparedInteraction) {
	for _, parameter := range source.parameters {
		accumulator := target.parameters[parameter.name]
		if accumulator == nil {
			accumulator = &parameterAccumulator{
				name: parameter.name, definitions: make(map[string]tir.Parameter),
			}
			target.parameters[parameter.name] = accumulator
		}
		accumulator.definitions[parameter.key] = parameter.value
		if parameter.hasOrder && (!accumulator.hasOrder || parameter.order < accumulator.order) {
			accumulator.order = parameter.order
			accumulator.hasOrder = true
		}
	}
	for _, locator := range source.locators {
		accumulator := target.locators[locator.key]
		if accumulator == nil {
			value := locator.value
			value.ID = ""
			accumulator = &locatorAccumulator{
				key: locator.key, locator: value, evidence: make(map[string]observation.Evidence),
			}
			target.locators[locator.key] = accumulator
		}
		mergeEvidence(accumulator.evidence, locator.evidence)
	}
	for _, action := range source.actions {
		accumulator := target.actions[action.key]
		if accumulator == nil {
			accumulator = &actionAccumulator{
				action: action.value, locatorKeys: action.locatorKeys,
				classes: make(map[tir.SideEffectClass]struct{}), allSafe: true,
			}
			target.actions[action.key] = accumulator
		}
		accumulator.classes[action.value.SideEffect.Class] = struct{}{}
		accumulator.rationales = appendNonempty(accumulator.rationales, action.value.SideEffect.Rationale)
		accumulator.allSafe = accumulator.allSafe && action.value.SideEffect.SafeForExploration
	}
}

func compileTools(accumulators map[string]*toolAccumulator, warnings *[]pendingWarning) ([]compiledTool, map[string]string) {
	keys := sortedMapKeys(accumulators)
	ids := stableIDs(keys, func(key string) string {
		accumulator := accumulators[key]
		return slug(chooseDisplay(accumulator.names), "tool") + "-" + digest(key)
	})
	result := make([]compiledTool, 0, len(keys))
	for _, key := range keys {
		accumulator := accumulators[key]
		id := ids[key]
		score, provenance := aggregateEvidence(accumulator.evidence)
		tool := tir.Tool{
			ID: id, Name: chooseDisplay(accumulator.names),
			Description: chooseDescription(accumulator.descriptions),
			Confidence: tir.Confidence{
				Score: score, Rationale: confidenceRationale(len(accumulator.evidence)),
			},
			Provenance: provenance,
		}
		tool.Parameters = compileParameters(accumulator, warnings)
		tool.Locators = compileLocators(accumulator, id)
		tool.Actions = compileActions(accumulator, id, tool.Locators, warnings)
		result = append(result, compiledTool{key: key, tool: tool})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].tool.ID < result[j].tool.ID })
	return result, ids
}

func compileParameters(accumulator *toolAccumulator, warnings *[]pendingWarning) []tir.Parameter {
	values := make([]*parameterAccumulator, 0, len(accumulator.parameters))
	for _, parameter := range accumulator.parameters {
		values = append(values, parameter)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].hasOrder != values[j].hasOrder {
			return values[i].hasOrder
		}
		if values[i].hasOrder && values[i].order != values[j].order {
			return values[i].order < values[j].order
		}
		return canonicalText(values[i].name) < canonicalText(values[j].name)
	})
	result := make([]tir.Parameter, 0, len(values))
	for _, parameter := range values {
		definitionKeys := sortedMapKeys(parameter.definitions)
		result = append(result, parameter.definitions[definitionKeys[0]])
		if len(definitionKeys) > 1 {
			*warnings = append(*warnings, pendingWarning{
				code:    "ambiguous_parameter",
				message: fmt.Sprintf("conflicting definitions for parameter %q; selected the canonical first", parameter.name),
				toolKey: accumulator.identity, framePath: accumulator.framePath,
			})
		}
	}
	return result
}

func compileLocators(accumulator *toolAccumulator, toolID string) []tir.LocatorCandidate {
	keys := sortedMapKeys(accumulator.locators)
	type ranked struct {
		key   string
		value tir.LocatorCandidate
	}
	values := make([]ranked, 0, len(keys))
	locatorIDKeys := make([]string, len(keys))
	for i, key := range keys {
		item := accumulator.locators[key]
		score, provenance := aggregateEvidence(item.evidence)
		item.locator.Confidence = tir.Confidence{
			Score: score, Rationale: confidenceRationale(len(item.evidence)),
		}
		item.locator.Provenance = provenance
		locatorIDKeys[i] = key
		values = append(values, ranked{key: key, value: item.locator})
	}
	ids := stableIDs(locatorIDKeys, func(key string) string {
		return toolID + "-locator-" + locatorSlug(accumulator.locators[key].locator) + "-" + digest(key)
	})
	for i := range values {
		values[i].value.ID = ids[values[i].key]
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].value.Confidence.Score != values[j].value.Confidence.Score {
			return values[i].value.Confidence.Score > values[j].value.Confidence.Score
		}
		return values[i].key < values[j].key
	})
	result := make([]tir.LocatorCandidate, len(values))
	for i := range values {
		result[i] = values[i].value
	}
	return result
}

func compileActions(accumulator *toolAccumulator, toolID string, locators []tir.LocatorCandidate, warnings *[]pendingWarning) []tir.ActionBinding {
	locatorKeys := sortedMapKeys(accumulator.locators)
	locatorIDs := stableIDs(locatorKeys, func(key string) string {
		return toolID + "-locator-" + locatorSlug(accumulator.locators[key].locator) + "-" + digest(key)
	})
	locatorRank := make(map[string]int, len(locators))
	for index, locator := range locators {
		locatorRank[locator.ID] = index
	}
	keys := sortedMapKeys(accumulator.actions)
	result := make([]tir.ActionBinding, 0, len(keys))
	for _, key := range keys {
		item := accumulator.actions[key]
		item.action.LocatorCandidateIDs = make([]string, 0, len(item.locatorKeys))
		for _, locatorKey := range item.locatorKeys {
			item.action.LocatorCandidateIDs = append(item.action.LocatorCandidateIDs, locatorIDs[locatorKey])
		}
		sort.Slice(item.action.LocatorCandidateIDs, func(i, j int) bool {
			return locatorRank[item.action.LocatorCandidateIDs[i]] < locatorRank[item.action.LocatorCandidateIDs[j]]
		})
		item.action.SideEffect.Class = mostConservativeClass(item.classes)
		item.action.SideEffect.SafeForExploration = item.allSafe &&
			item.action.SideEffect.Class != tir.SideEffectNavigation &&
			item.action.SideEffect.Class != tir.SideEffectSubmission
		item.action.SideEffect.Rationale = chooseDescription(item.rationales)
		if len(item.classes) > 1 {
			*warnings = append(*warnings, pendingWarning{
				code:    "ambiguous_side_effect",
				message: "conflicting side-effect classes; selected the most conservative",
				toolKey: accumulator.identity, framePath: accumulator.framePath,
			})
		}
		result = append(result, item.action)
	}
	sort.Slice(result, func(i, j int) bool {
		return actionOutputKey(result[i]) < actionOutputKey(result[j])
	})
	return result
}

func compileCoverage(document *tir.Document, reported bool, frames map[string]*frameAccumulator, warnings *[]pendingWarning) {
	if !reported {
		document.FrameCoverage.Status = tir.CoverageUnavailable
		return
	}
	keys := sortedMapKeys(frames)
	for _, key := range keys {
		frame := frames[key]
		path := toTIRFramePath(frame.path)
		if frame.inaccessible {
			reasons := sortedUniqueStrings(frame.reasons)
			document.FrameCoverage.Uncovered = append(document.FrameCoverage.Uncovered, tir.UncoveredFrame{
				Path: path, Reason: strings.Join(reasons, "; "),
			})
			*warnings = append(*warnings, pendingWarning{
				code: "frame_uncovered", message: "frame coverage is incomplete",
				framePath: frame.path,
			})
			if frame.accessible {
				*warnings = append(*warnings, pendingWarning{
					code:      "ambiguous_frame_coverage",
					message:   "frame was reported as both accessible and inaccessible; treated as uncovered",
					framePath: frame.path,
				})
			}
			continue
		}
		if frame.accessible {
			urls := sortedUniqueStrings(frame.urls)
			origins := sortedUniqueStrings(frame.origins)
			document.FrameCoverage.Frames = append(document.FrameCoverage.Frames, tir.CoveredFrame{
				Path: path, URL: firstOrEmpty(urls), Origin: firstOrEmpty(origins),
			})
			if len(urls) > 1 || len(origins) > 1 {
				*warnings = append(*warnings, pendingWarning{
					code:      "ambiguous_frame_metadata",
					message:   "conflicting frame metadata; selected the canonical first value",
					framePath: frame.path,
				})
			}
		}
	}
	if len(document.FrameCoverage.Uncovered) > 0 {
		document.FrameCoverage.Status = tir.CoveragePartial
	} else {
		document.FrameCoverage.Status = tir.CoverageComplete
	}
}

func compileWarnings(source []pendingWarning, toolIDs map[string]string) []tir.Warning {
	unique := make(map[string]tir.Warning)
	for _, warning := range source {
		value := tir.Warning{
			Code: warning.code, Message: warning.message,
			ToolID: toolIDs[warning.toolKey], FramePath: toTIRFramePath(warning.framePath),
		}
		key := value.Code + "\x00" + value.ToolID + "\x00" +
			tirFramePathKey(value.FramePath) + "\x00" + value.Message
		unique[key] = value
	}
	keys := sortedMapKeys(unique)
	result := make([]tir.Warning, 0, len(keys))
	for _, key := range keys {
		result = append(result, unique[key])
	}
	return result
}

func compileParameter(source observation.Parameter) tir.Parameter {
	result := tir.Parameter{
		Name: cleanText(source.Name), Description: cleanText(source.Description),
		Type: tir.ValueType(source.Type), Required: source.Required,
		Enum: sortedUniqueStrings(cleanStrings(source.Enum)),
	}
	if source.Items != nil {
		items := compileShape(*source.Items)
		result.Items = &items
	}
	result.Properties = compileProperties(source.Properties)
	return result
}

func compileShape(source observation.ParameterShape) tir.ParameterShape {
	result := tir.ParameterShape{
		Type: tir.ValueType(source.Type), Enum: sortedUniqueStrings(cleanStrings(source.Enum)),
	}
	if source.Items != nil {
		items := compileShape(*source.Items)
		result.Items = &items
	}
	result.Properties = compileProperties(source.Properties)
	return result
}

func compileProperties(source []observation.ParameterProperty) []tir.ParameterProperty {
	type ordered struct {
		value    tir.ParameterProperty
		order    int
		hasOrder bool
	}
	values := make([]ordered, 0, len(source))
	for _, property := range source {
		item := ordered{value: tir.ParameterProperty{
			Name: cleanText(property.Name), Description: cleanText(property.Description),
			Required: property.Required, Shape: compileShape(property.Shape),
		}}
		if property.SourceOrder != nil {
			item.order, item.hasOrder = *property.SourceOrder, true
		}
		values = append(values, item)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].hasOrder != values[j].hasOrder {
			return values[i].hasOrder
		}
		if values[i].hasOrder && values[i].order != values[j].order {
			return values[i].order < values[j].order
		}
		return canonicalText(values[i].value.Name) < canonicalText(values[j].value.Name)
	})
	result := make([]tir.ParameterProperty, len(values))
	for i := range values {
		result[i] = values[i].value
	}
	return result
}

func compileLocator(field string, source observation.Locator) (tir.LocatorCandidate, []observation.Evidence, error) {
	result := tir.LocatorCandidate{
		FramePath:   compilePathNodes(source.FramePath),
		ShadowPath:  compilePathNodes(source.ShadowPath),
		CSSFallback: cleanText(source.CSS),
	}
	if source.Semantic != nil {
		result.Semantic = &tir.SemanticLocator{
			Scope: compileSemanticNodes(source.Semantic.Scope),
			Role:  canonicalText(source.Semantic.Role), Name: cleanText(source.Semantic.Name),
		}
	}
	evidence := make([]observation.Evidence, 0, len(source.Evidence))
	for i, item := range source.Evidence {
		if err := validateEvidence(fmt.Sprintf("%s.evidence[%d]", field, i), item); err != nil {
			return result, nil, err
		}
		evidence = append(evidence, normalizeEvidence(item))
	}
	if len(evidence) == 0 {
		evidence = append(evidence, fallbackEvidence())
	}
	return result, evidence, nil
}

func compilePathNodes(source []observation.PathNode) []tir.PathNode {
	result := make([]tir.PathNode, len(source))
	for i, node := range source {
		result[i].CSSFallback = cleanText(node.CSS)
		if node.Semantic != nil {
			result[i].Semantic = &tir.SemanticNode{
				Role: canonicalText(node.Semantic.Role), Name: cleanText(node.Semantic.Name),
			}
		}
	}
	return result
}

func compileSemanticNodes(source []observation.SemanticNode) []tir.SemanticNode {
	result := make([]tir.SemanticNode, len(source))
	for i, node := range source {
		result[i] = tir.SemanticNode{Role: canonicalText(node.Role), Name: cleanText(node.Name)}
	}
	return result
}

func normalizeSemanticNodes(source []observation.SemanticNode) []observation.SemanticNode {
	result := make([]observation.SemanticNode, len(source))
	for i, node := range source {
		result[i] = observation.SemanticNode{Role: canonicalText(node.Role), Name: cleanText(node.Name)}
	}
	return result
}

func aggregateEvidence(evidence map[string]observation.Evidence) (float64, []tir.Provenance) {
	keys := sortedMapKeys(evidence)
	product := 1.0
	provenance := make([]tir.Provenance, 0, len(keys))
	for _, key := range keys {
		item := evidence[key]
		product *= 1 - item.Score
		provenance = append(provenance, tir.Provenance{
			Kind: tir.ProvenanceKind(item.Kind), Reference: item.Reference,
		})
	}
	score := math.Round((1-product)*1_000_000) / 1_000_000
	return score, provenance
}

func mergeEvidence(target map[string]observation.Evidence, source []observation.Evidence) {
	for _, item := range source {
		key := string(item.Kind) + "\x00" + item.Reference
		if previous, exists := target[key]; !exists || item.Score > previous.Score {
			target[key] = item
		}
	}
}

func validateEvidence(field string, evidence observation.Evidence) error {
	if math.IsNaN(evidence.Score) || math.IsInf(evidence.Score, 0) ||
		evidence.Score < 0 || evidence.Score > 1 {
		return &Error{Field: field + ".score", Code: "invalid_confidence", Message: "must be a finite number in [0, 1]"}
	}
	return nil
}

func validateRawFramePath(field string, path []observation.FrameReference) error {
	for i, frame := range path {
		if frame.Index < 0 {
			return &Error{
				Field: fmt.Sprintf("%s[%d].index", field, i),
				Code:  "invalid_frame_index", Message: "must be nonnegative",
			}
		}
	}
	return nil
}

func normalizeEvidence(evidence observation.Evidence) observation.Evidence {
	evidence.Reference = cleanText(evidence.Reference)
	return evidence
}

func fallbackEvidence() observation.Evidence {
	return observation.Evidence{
		Kind: observation.EvidenceHeuristic, Reference: "compiler:fallback", Score: 0.5,
	}
}

func stableIDs(keys []string, base func(string) string) map[string]string {
	groups := make(map[string][]string)
	for _, key := range keys {
		value := base(key)
		groups[value] = append(groups[value], key)
	}
	result := make(map[string]string, len(keys))
	for value, group := range groups {
		sort.Strings(group)
		for i, key := range group {
			result[key] = value
			if i > 0 {
				result[key] = value + "-" + strconv.Itoa(i+1)
			}
		}
	}
	return result
}

func mostConservativeClass(classes map[tir.SideEffectClass]struct{}) tir.SideEffectClass {
	order := []tir.SideEffectClass{
		tir.SideEffectSubmission, tir.SideEffectNavigation, tir.SideEffectNetwork,
		tir.SideEffectUnknown, tir.SideEffectLocalState, tir.SideEffectNone,
	}
	for _, class := range order {
		if _, exists := classes[class]; exists {
			return class
		}
	}
	return tir.SideEffectUnknown
}

func actionIdentity(action tir.ActionBinding, locatorKeys []string) string {
	return joinedKey(string(action.Action), action.InputParameter, strings.Join(locatorKeys, "\x1e"))
}

func actionOutputKey(action tir.ActionBinding) string {
	return joinedKey(string(action.Action), action.InputParameter,
		strings.Join(action.LocatorCandidateIDs, "\x1e"), string(action.SideEffect.Class))
}

func locatorSlug(locator tir.LocatorCandidate) string {
	if locator.Semantic != nil {
		return slug(locator.Semantic.Role+" "+locator.Semantic.Name, "semantic")
	}
	return "css"
}

func semanticLocatorKey(locator *tir.SemanticLocator) string {
	if locator == nil {
		return ""
	}
	parts := []string{locator.Role, locator.Name}
	for _, scope := range locator.Scope {
		parts = append(parts, scope.Role, scope.Name)
	}
	return joinedKey(parts...)
}

func semanticNodesKey(nodes []observation.SemanticNode) string {
	parts := make([]string, 0, len(nodes)*2)
	for _, node := range nodes {
		parts = append(parts, node.Role, canonicalText(node.Name))
	}
	return joinedKey(parts...)
}

func observationFramePathKey(path []observation.FrameReference) string {
	parts := make([]string, 0, len(path)*3)
	for _, frame := range path {
		parts = append(parts, strconv.Itoa(frame.Index), frame.Name, frame.Src)
	}
	return joinedKey(parts...)
}

func tirFramePathKey(path []tir.FrameReference) string {
	parts := make([]string, 0, len(path)*3)
	for _, frame := range path {
		parts = append(parts, strconv.Itoa(frame.Index), frame.Name, frame.Src)
	}
	return joinedKey(parts...)
}

func joinedKey(parts ...string) string {
	var builder strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&builder, "%d:%s", len(part), part)
	}
	return builder.String()
}

func normalizeFrameReferences(path []observation.FrameReference) []observation.FrameReference {
	result := make([]observation.FrameReference, len(path))
	for i, frame := range path {
		result[i] = observation.FrameReference{
			Index: frame.Index, Name: cleanText(frame.Name), Src: cleanText(frame.Src),
		}
	}
	return result
}

func toTIRFramePath(path []observation.FrameReference) []tir.FrameReference {
	result := make([]tir.FrameReference, len(path))
	for i, frame := range path {
		result[i] = tir.FrameReference{Index: frame.Index, Name: frame.Name, Src: frame.Src}
	}
	return result
}

func defaultRole(kind observation.InteractionKind) string {
	switch kind {
	case observation.InteractionForm:
		return "form"
	case observation.InteractionAction:
		return "button"
	default:
		return "control"
	}
}

func fallbackName(role string, scope []observation.SemanticNode) string {
	for i := len(scope) - 1; i >= 0; i-- {
		if name := cleanText(scope[i].Name); name != "" {
			return name + " " + humanize(role)
		}
	}
	return humanize(role)
}

func humanize(value string) string {
	value = cleanText(strings.ReplaceAll(value, "-", " "))
	if value == "" {
		return "Interaction"
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func slug(value, fallback string) string {
	var builder strings.Builder
	separator := false
	for _, character := range strings.ToLower(value) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			builder.WriteRune(character)
			separator = false
		} else if builder.Len() > 0 && !separator {
			builder.WriteByte('-')
			separator = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return fallback
	}
	return result
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:6])
}

func chooseDisplay(values []string) string {
	values = sortedUniqueStrings(cleanStrings(values))
	if len(values) == 0 {
		return "Interaction"
	}
	sort.Slice(values, func(i, j int) bool {
		left, right := canonicalText(values[i]), canonicalText(values[j])
		if left != right {
			return left < right
		}
		return values[i] < values[j]
	})
	return values[0]
}

func chooseDescription(values []string) string {
	values = sortedUniqueStrings(cleanStrings(values))
	sort.Slice(values, func(i, j int) bool {
		if len(values[i]) != len(values[j]) {
			return len(values[i]) > len(values[j])
		}
		return values[i] < values[j]
	})
	return firstOrEmpty(values)
}

func confidenceRationale(count int) string {
	if count <= 0 {
		return "no evidence"
	}
	return fmt.Sprintf("noisy-or aggregation of %d unique evidence item(s)", count)
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if cleaned := cleanText(value); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func canonicalText(value string) string {
	return strings.ToLower(cleanText(value))
}

func appendNonempty(values []string, value string) []string {
	if value != "" {
		return append(values, value)
	}
	return values
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func sortedUniqueStrings(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
