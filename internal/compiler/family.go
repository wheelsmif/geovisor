package compiler

import (
	"fmt"
	"strings"

	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/tir"
)

const (
	warningDroppedLowValueAction = "dropped_low_value_action"
	familyTargetParameter        = "target"
	familyEnumCap                = 64
)

// isLowValueAction reports citation marks, DOI/RFC tokens, same-document
// citation fragments, and unnamed links. The citation fact is CitationHref,
// not a match against locator CSS. Unnamed buttons are kept so they can still
// receive a fallback name.
func isLowValueAction(source observation.Interaction) bool {
	if source.Kind != observation.InteractionAction {
		return false
	}
	if source.CitationHref {
		return true
	}
	name := cleanText(source.Name)
	role := canonicalText(source.Role)
	if role == "" {
		role = defaultRole(source.Kind)
	}
	return isLowValueName(name, role)
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
	switch strings.ToLower(cleanText(source.InputType)) {
	case "submit", "reset", "image":
		return true
	default:
		return false
	}
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

func familyToolDescription(role string, action tir.ActionKind) string {
	switch {
	case role == "link":
		return "Follow one of this page's links"
	case action == tir.ActionCheck:
		return "Toggle one of this page's controls"
	default:
		return "Click one of this page's controls"
	}
}

func familyParameterDescription(names []string) string {
	if len(names) == 0 || len(names) > familyEnumCap {
		return "Accessible name of the control to use"
	}
	return "Accessible name of the control to use: " + strings.Join(names, ", ")
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
			Action:           action,
			InputParameter:   familyTargetParameter,
			LocatorSelection: tir.LocatorSelectionByTargetName,
			SideEffect:       sideEffect,
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
		Description: familyParameterDescription(names),
		Type:        tir.ValueString,
		Required:    true,
	}
	if len(names) <= familyEnumCap {
		parameter.Enum = uniqueInOrder(names)
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
