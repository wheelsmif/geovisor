package compiler

import (
	"fmt"
	"regexp"

	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/tir"
)

const (
	warningDroppedLowValueAction = "dropped_low_value_action"
	familyTargetParameter        = "target"
	familyEnumCap                = 64
)

// citationMarkName, doiName, bareRFCOrDOI, and citationFragment are compiled
// from lowvalue_patterns.json at init (see lowvalue.go).
var reservedInputType = regexp.MustCompile(`(?i)\[type\s*=\s*["']?(submit|reset|image)["']?\]`)

// isLowValueAction reports citation marks, DOI/RFC tokens, same-document
// citation fragments, and unnamed links. Unnamed buttons are kept so they can
// still receive a fallback name.
func isLowValueAction(source observation.Interaction) bool {
	if source.Kind != observation.InteractionAction {
		return false
	}
	name := cleanText(source.Name)
	role := canonicalText(source.Role)
	if role == "" {
		role = defaultRole(source.Kind)
	}
	if isLowValueName(name, role) {
		return true
	}
	for _, locator := range source.Locators {
		if citationFragment.MatchString(locator.CSS) {
			return true
		}
	}
	return false
}

func familyEligible(source observation.Interaction) (tir.ActionKind, tir.SideEffectClass, bool) {
	if source.Kind != observation.InteractionAction {
		return "", "", false
	}
	if isReservedStandaloneAction(source) {
		return "", "", false
	}
	if len(source.Actions) != 1 || len(source.Parameters) != 0 {
		return "", "", false
	}
	action := source.Actions[0]
	if action.Kind != observation.ActionClick && action.Kind != observation.ActionCheck {
		return "", "", false
	}
	return tir.ActionKind(action.Kind), tir.SideEffectClass(action.SideEffect.Class), true
}

func isReservedStandaloneAction(source observation.Interaction) bool {
	for _, action := range source.Actions {
		if action.SideEffect.Class == observation.SideEffectSubmission {
			return true
		}
	}
	for _, locator := range source.Locators {
		if reservedInputType.MatchString(locator.CSS) {
			return true
		}
	}
	return false
}

func familyIdentityKey(prepared preparedInteraction, action tir.ActionKind, class tir.SideEffectClass) string {
	return joinedKey(
		string(observation.InteractionAction),
		observationFramePathKey(prepared.framePath),
		prepared.role,
		string(action),
		string(class),
	)
}

func familyActionIdentity(action tir.ActionKind) string {
	return joinedKey(string(action), familyTargetParameter)
}

func familyDisplayName(role string, action tir.ActionKind) string {
	if role == "link" {
		return "Follow link"
	}
	if action == tir.ActionCheck {
		return "Toggle"
	}
	return "Click"
}

func familyToolDescription() string {
	return "target is the control's accessible name"
}

func applyFamilyIdentity(prepared *preparedInteraction, action tir.ActionKind, class tir.SideEffectClass) {
	prepared.identity = familyIdentityKey(*prepared, action, class)
	prepared.family = true
	prepared.familyAction = action
	locatorKeys := make([]string, 0, len(prepared.locators))
	for _, locator := range prepared.locators {
		locatorKeys = append(locatorKeys, locator.key)
	}
	locatorKeys = sortedUniqueStrings(locatorKeys)
	sideEffect := tir.SideEffect{Class: class, SafeForExploration: false}
	if len(prepared.actions) == 1 {
		sideEffect = prepared.actions[0].value.SideEffect
	}
	prepared.actions = []preparedAction{{
		key: familyActionIdentity(action),
		value: tir.ActionBinding{
			Action:         action,
			InputParameter: familyTargetParameter,
			SideEffect:     sideEffect,
		},
		locatorKeys: locatorKeys,
	}}
}

func assignFamilyIdentities(items []preparedItem) {
	counts := make(map[string]int)
	meta := make([]struct {
		ok     bool
		action tir.ActionKind
		class  tir.SideEffectClass
		key    string
	}, len(items))
	for i, item := range items {
		action, class, ok := familyEligible(item.source)
		if !ok {
			continue
		}
		key := familyIdentityKey(item.prepared, action, class)
		meta[i].ok = true
		meta[i].action = action
		meta[i].class = class
		meta[i].key = key
		counts[key]++
	}
	for i := range items {
		if !meta[i].ok {
			continue
		}
		if items[i].prepared.role != "link" && counts[meta[i].key] < 2 {
			continue
		}
		applyFamilyIdentity(&items[i].prepared, meta[i].action, meta[i].class)
	}
}

func familyTargetParameterSpec(locators []tir.LocatorCandidate) tir.Parameter {
	names := familyTargetNames(locators)
	parameter := tir.Parameter{
		Name:        familyTargetParameter,
		Description: familyToolDescription(),
		Type:        tir.ValueString,
		Required:    true,
	}
	if len(names) <= familyEnumCap {
		parameter.Enum = sortedUniqueStrings(names)
	}
	return parameter
}

func familyTargetNames(locators []tir.LocatorCandidate) []string {
	counts := make(map[string]int, len(locators))
	names := make([]string, 0, len(locators))
	for _, locator := range locators {
		base := familyLocatorName(locator)
		counts[base]++
		if counts[base] == 1 {
			names = append(names, base)
			continue
		}
		names = append(names, fmt.Sprintf("%s (%d)", base, counts[base]))
	}
	return names
}

func familyLocatorName(locator tir.LocatorCandidate) string {
	if locator.Semantic != nil {
		if name := cleanText(locator.Semantic.Name); name != "" {
			return name
		}
	}
	return "Control"
}
