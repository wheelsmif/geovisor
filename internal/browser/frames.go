package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/wheelsmif/geovisor/internal/compiler"
	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/pageurl"
	"github.com/wheelsmif/geovisor/internal/payload"
)

const isolatedWorldName = "__geovisor_observer__"

// frameRole is the role a frame path node addresses. It must stay equal to
// FRAME_ROLE in client/src/shared/locate.ts, which is the consumer side of the
// same contract.
const frameRole = "iframe"

type observeTargetOptions struct {
	requestedURL   string
	navigate       bool
	quietPeriod    time.Duration
	quietTimeout   time.Duration
	frameTimeout   time.Duration
	extraction     ExtractionOptions
	sourceKind     observation.SourceKind
	stealthEnabled bool
}

type discoveredFrame struct {
	frame   *proto.PageFrame
	path    []observation.FrameReference
	session *sessionClient
	// reason, when set, marks the frame uncovered without extracting tools.
	// Closed-shadow-hosted frames are page-JS-unaddressable (P7, P9).
	reason string
}

func observeTarget(
	ctx context.Context,
	browser *rod.Browser,
	rootSession *sessionClient,
	targetID proto.TargetTargetID,
	options observeTargetOptions,
) (Result, error) {
	if err := (proto.PageEnable{}).Call(rootSession); err != nil {
		return Result{}, operationError(ctx, ErrorReadiness, "page.enable", "enable page observation", err)
	}
	var loaderID proto.NetworkLoaderID
	if options.navigate {
		response, err := (proto.PageNavigate{URL: options.requestedURL}).Call(rootSession)
		if err != nil {
			return Result{}, operationError(ctx, ErrorNavigation, "page.navigate", "navigate target", err)
		}
		if response.ErrorText != "" {
			return Result{}, sanitizedError(
				ErrorNavigation, "page.navigate", "Chromium rejected navigation",
				errors.New(response.ErrorText), options.requestedURL,
			)
		}
		loaderID = response.LoaderID
	}
	if err := waitForDocumentReady(ctx, rootSession, loaderID); err != nil {
		return Result{}, operationError(ctx, ErrorReadiness, "page.load", "wait for document load", err)
	}

	result := Result{}
	quiet, err := waitForDOMQuiet(rootSession, options.quietPeriod, options.quietTimeout)
	if err != nil {
		return result, operationError(ctx, ErrorReadiness, "page.dom_quiet", "wait for bounded DOM quiet", err)
	}
	if !quiet {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code: DiagnosticDOMNotQuiet, Severity: SeverityWarning,
			Message:   "DOM continued changing until the bounded quiet-check deadline",
			FramePath: []observation.FrameReference{},
		})
	}

	tree, err := (proto.PageGetFrameTree{}).Call(rootSession)
	if err != nil || tree.FrameTree == nil || tree.FrameTree.Frame == nil {
		return result, operationError(ctx, ErrorFrameDiscovery, "frames.tree", "read browser frame tree", err)
	}
	waitForMissingOOPIFs(ctx, browser, rootSession, tree.FrameTree, options.quietTimeout)
	tree, err = (proto.PageGetFrameTree{}).Call(rootSession)
	if err != nil || tree.FrameTree == nil || tree.FrameTree.Frame == nil {
		return result, operationError(ctx, ErrorFrameDiscovery, "frames.tree", "read browser frame tree", err)
	}

	oopifSessions, oopifTrees, oopifDiagnostics, detach := attachOOPIFSessions(
		ctx, browser, targetID, rootSession, tree.FrameTree,
	)
	defer detach()
	result.Diagnostics = append(result.Diagnostics, oopifDiagnostics...)
	frames := flattenCompleteFrameTree(tree.FrameTree, rootSession, oopifSessions, oopifTrees)
	batch := observation.Batch{
		CoverageReported: true,
		Frames:           make([]observation.Frame, 0, len(frames)),
		Interactions:     []observation.Interaction{},
		Warnings:         []observation.Warning{},
	}

	for _, frame := range frames {
		fact := observation.Frame{
			Path:       cloneFramePath(frame.path),
			Accessible: true,
			URL:        pageurl.PageURL(frame.frame.URL),
			Origin:     frame.frame.SecurityOrigin,
		}
		extracted, code, reason := extractFrame(frame, options.extraction, options.frameTimeout)
		if reason != "" {
			fact.Accessible = false
			fact.Reason = reason
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Code: code, Severity: SeverityWarning, Message: reason,
				FramePath: cloneFramePath(frame.path),
			})
		} else {
			augmentBatch(&extracted, frame.path)
			batch.Interactions = append(batch.Interactions, extracted.Interactions...)
			batch.Warnings = append(batch.Warnings, extracted.Warnings...)
		}
		batch.Frames = append(batch.Frames, fact)
	}

	if len(frames) > 0 && accessInterstitial(frames[0]) {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code: DiagnosticAccessInterstitial, Severity: SeverityWarning,
			Message:   "page content resembles a bot challenge or access interstitial",
			FramePath: []observation.FrameReference{},
		})
	}

	finalURL := tree.FrameTree.Frame.URL
	result.Input = compiler.Input{
		Source: observation.Source{
			Kind: options.sourceKind, RequestedURL: options.requestedURL,
			FinalURL: finalURL, StealthEnabled: options.stealthEnabled,
		},
		Batches: []observation.Batch{batch},
	}
	return result, nil
}

func waitForMissingOOPIFs(
	ctx context.Context,
	browser *rod.Browser,
	session *sessionClient,
	tree *proto.PageFrameTree,
	limit time.Duration,
) {
	expected := countFrameElements(piercedDocument(session))
	have := 0
	if tree != nil {
		have = len(tree.ChildFrames)
	}
	if expected <= have {
		return
	}
	waitForIFrameTargets(ctx, browser, limit)
}

func countFrameElements(node *proto.DOMNode) int {
	if node == nil {
		return 0
	}
	localName := strings.ToLower(node.LocalName)
	if localName == "iframe" || localName == "frame" {
		return 1
	}
	total := 0
	for _, child := range node.Children {
		total += countFrameElements(child)
	}
	for _, shadow := range node.ShadowRoots {
		if shadow == nil || shadow.ShadowRootType == proto.DOMShadowRootTypeClosed {
			continue
		}
		total += countFrameElements(shadow)
	}
	return total
}

func waitForIFrameTargets(ctx context.Context, browser *rod.Browser, limit time.Duration) {
	if browser == nil {
		return
	}
	if limit <= 0 {
		limit = DefaultDOMQuietLimit
	}
	deadline := time.Now().Add(limit)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		targets, err := (proto.TargetGetTargets{}).Call(browser.Context(ctx))
		if err == nil {
			for _, target := range targets.TargetInfos {
				if target != nil && string(target.Type) == "iframe" {
					return
				}
			}
		}
		if !time.Now().Before(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func sessionMainFrameID(session *sessionClient) (proto.PageFrameID, error) {
	tree, err := (proto.PageGetFrameTree{}).Call(session)
	if err != nil || tree.FrameTree == nil || tree.FrameTree.Frame == nil {
		if err == nil {
			err = errors.New("page frame tree is empty")
		}
		return "", err
	}
	return tree.FrameTree.Frame.ID, nil
}

func evaluateIsolated(
	session *sessionClient,
	frameID proto.PageFrameID,
	expression string,
	awaitPromise bool,
	timeout proto.RuntimeTimeDelta,
) (*proto.RuntimeEvaluateResult, error) {
	world, err := (proto.PageCreateIsolatedWorld{
		FrameID: frameID, WorldName: isolatedWorldName,
	}).Call(session)
	if err != nil {
		return nil, err
	}
	params := proto.RuntimeEvaluate{
		Expression:    expression,
		ContextID:     world.ExecutionContextID,
		AwaitPromise:  awaitPromise,
		ReturnByValue: true,
	}
	if timeout != 0 {
		params.Timeout = timeout
	}
	return params.Call(session)
}

func waitForDocumentReady(ctx context.Context, session *sessionClient, loaderID proto.NetworkLoaderID) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready, err := documentMatchesNavigation(session, loaderID); err == nil && ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// documentMatchesNavigation reports whether the session's main document is
// complete and, after a Page.navigate, belongs to that navigation rather than
// the launch about:blank that is already complete.
func documentMatchesNavigation(session *sessionClient, loaderID proto.NetworkLoaderID) (bool, error) {
	frameID, err := sessionMainFrameID(session)
	if err != nil {
		return false, err
	}
	result, err := evaluateIsolated(session, frameID, "document.readyState === 'complete'", false, 0)
	if err != nil || result.ExceptionDetails != nil || result.Result == nil || !result.Result.Value.Bool() {
		return false, err
	}
	if loaderID == "" {
		return true, nil
	}
	tree, err := (proto.PageGetFrameTree{}).Call(session)
	if err != nil || tree.FrameTree == nil || tree.FrameTree.Frame == nil {
		return false, err
	}
	frame := tree.FrameTree.Frame
	if frame.LoaderID == loaderID {
		return true, nil
	}
	// A redirect commits a different loader than Page.navigate returned. Accept
	// that committed document once it is no longer the pre-navigation blank.
	return frame.LoaderID != "" && !isAboutBlankURL(frame.URL), nil
}

func isAboutBlankURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return trimmed == "" || trimmed == "about:blank"
}

func waitForDOMQuiet(
	session *sessionClient,
	quietPeriod, limit time.Duration,
) (bool, error) {
	frameID, err := sessionMainFrameID(session)
	if err != nil {
		return false, err
	}
	quietMS := quietPeriod.Milliseconds()
	limitMS := limit.Milliseconds()
	expression := fmt.Sprintf(`new Promise(resolve => {
		let quietTimer;
		const finish = value => { observer.disconnect(); clearTimeout(deadline); clearTimeout(quietTimer); resolve(value); };
		const arm = () => { clearTimeout(quietTimer); quietTimer = setTimeout(() => finish(true), %d); };
		const observer = new MutationObserver(arm);
		observer.observe(document, {subtree:true, childList:true, attributes:true, characterData:true});
		const deadline = setTimeout(() => finish(false), %d);
		arm();
	})`, quietMS, limitMS)
	result, err := evaluateIsolated(
		session, frameID, expression, true,
		proto.RuntimeTimeDelta(limitMS+quietMS+100),
	)
	if err != nil {
		return false, err
	}
	if result.ExceptionDetails != nil {
		return false, errors.New("DOM quiet check raised a JavaScript exception")
	}
	if result.Result == nil {
		return false, errors.New("DOM quiet check returned no result")
	}
	return result.Result.Value.Bool(), nil
}

func attachOOPIFSessions(
	ctx context.Context,
	browser *rod.Browser,
	rootTargetID proto.TargetTargetID,
	rootSession *sessionClient,
	rootTree *proto.PageFrameTree,
) (
	map[proto.PageFrameID]*sessionClient,
	map[proto.PageFrameID]*proto.PageFrameTree,
	[]Diagnostic,
	func(),
) {
	ownerOrder := make(map[proto.PageFrameID][]proto.PageFrameID)
	mergeFrameOwnerOrder(ownerOrder, rootSession)
	var attached []*sessionClient
	sessions, trees, diagnostics := discoverOOPIFs(
		rootTargetID,
		rootTree,
		ownerOrder,
		func() ([]*proto.TargetTargetInfo, error) {
			targets, err := (proto.TargetGetTargets{}).Call(browser.Context(ctx))
			if err != nil {
				return nil, err
			}
			return targets.TargetInfos, nil
		},
		func(target *proto.TargetTargetInfo) (*sessionClient, *proto.PageFrameTree, error) {
			attachContext, cancel := context.WithTimeout(ctx, DefaultDOMQuietLimit)
			defer cancel()
			session, attachErr := attachTarget(attachContext, browser, target.TargetID)
			if attachErr != nil {
				return nil, nil, attachErr
			}
			_ = (proto.PageEnable{}).Call(session)
			tree, treeErr := (proto.PageGetFrameTree{}).Call(session)
			session.ctx = ctx
			if treeErr != nil || tree.FrameTree == nil || tree.FrameTree.Frame == nil {
				detachTarget(browser, session.sessionID)
				if treeErr == nil {
					treeErr = errors.New("page frame tree is empty")
				}
				return nil, nil, treeErr
			}
			attached = append(attached, session)
			return session, tree.FrameTree, nil
		},
	)
	return sessions, trees, diagnostics, func() {
		for index := len(attached) - 1; index >= 0; index-- {
			detachTarget(browser, attached[index].sessionID)
		}
	}
}

// discoverOOPIFs attaches descendant iframe targets, refetching the target list
// after each successful attach so nested OOPIFs created by that attach can
// enter a later pass (P8).
func discoverOOPIFs(
	rootTargetID proto.TargetTargetID,
	rootTree *proto.PageFrameTree,
	ownerOrder map[proto.PageFrameID][]proto.PageFrameID,
	listTargets func() ([]*proto.TargetTargetInfo, error),
	attach func(*proto.TargetTargetInfo) (*sessionClient, *proto.PageFrameTree, error),
) (
	map[proto.PageFrameID]*sessionClient,
	map[proto.PageFrameID]*proto.PageFrameTree,
	[]Diagnostic,
) {
	result := make(map[proto.PageFrameID]*sessionClient)
	trees := make(map[proto.PageFrameID]*proto.PageFrameTree)
	var diagnostics []Diagnostic
	allowed := unionFrameIDs(rootTree, ownerOrder)
	seen := make(map[proto.TargetTargetID]struct{})
	for progress := true; progress; {
		progress = false
		targets, err := listTargets()
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Code:     DiagnosticFrameUncovered,
				Severity: SeverityWarning,
				Message:  "browser could not list targets for out-of-process frames: " + err.Error(),
			})
			break
		}
		sort.Slice(targets, func(i, j int) bool {
			if targets[i].URL != targets[j].URL {
				return targets[i].URL < targets[j].URL
			}
			return targets[i].TargetID < targets[j].TargetID
		})
		for _, target := range targets {
			if _, done := seen[target.TargetID]; done {
				continue
			}
			if !isDescendantIFrameTarget(allowed, rootTargetID, target) {
				continue
			}
			seen[target.TargetID] = struct{}{}
			progress = true
			session, tree, attachErr := attach(target)
			if attachErr != nil || tree == nil || tree.Frame == nil {
				addUnattachedOOPIF(trees, target, parentFrameID(
					rootTree, trees, ownerOrder, proto.PageFrameID(target.TargetID),
				))
				continue
			}
			result[tree.Frame.ID] = session
			trees[tree.Frame.ID] = tree
			for frameID := range descendantFrameIDs(tree) {
				allowed[frameID] = struct{}{}
			}
		}
	}
	return result, trees, diagnostics
}

func unionFrameIDs(
	tree *proto.PageFrameTree,
	ownerOrder map[proto.PageFrameID][]proto.PageFrameID,
) map[proto.PageFrameID]struct{} {
	allowed := descendantFrameIDs(tree)
	for parent, children := range ownerOrder {
		if parent != "" {
			allowed[parent] = struct{}{}
		}
		for _, child := range children {
			if child != "" {
				allowed[child] = struct{}{}
			}
		}
	}
	return allowed
}

func descendantFrameIDs(tree *proto.PageFrameTree) map[proto.PageFrameID]struct{} {
	result := make(map[proto.PageFrameID]struct{})
	var walk func(*proto.PageFrameTree)
	walk = func(node *proto.PageFrameTree) {
		if node == nil || node.Frame == nil {
			return
		}
		result[node.Frame.ID] = struct{}{}
		for _, child := range node.ChildFrames {
			walk(child)
		}
	}
	walk(tree)
	return result
}

func isDescendantIFrameTarget(
	allowed map[proto.PageFrameID]struct{},
	rootTargetID proto.TargetTargetID,
	target *proto.TargetTargetInfo,
) bool {
	if target == nil || string(target.Type) != "iframe" || target.TargetID == rootTargetID {
		return false
	}
	if _, ok := allowed[proto.PageFrameID(target.TargetID)]; ok {
		return true
	}
	// Page.getFrameTree omits some OOPIFs until attach. The pierce walk and
	// openerFrameId still identify those targets as descendants of this page.
	if target.OpenerFrameID != "" {
		_, ok := allowed[target.OpenerFrameID]
		return ok
	}
	return false
}

func addUnattachedOOPIF(
	trees map[proto.PageFrameID]*proto.PageFrameTree,
	target *proto.TargetTargetInfo,
	parentID proto.PageFrameID,
) {
	frameID := proto.PageFrameID(target.TargetID)
	if parentID == "" {
		parentID = target.OpenerFrameID
	}
	trees[frameID] = &proto.PageFrameTree{Frame: &proto.PageFrame{
		ID: frameID, ParentID: parentID, URL: target.URL, SecurityOrigin: pageurl.Origin(target.URL),
	}}
}

func parentFrameID(
	root *proto.PageFrameTree,
	extra map[proto.PageFrameID]*proto.PageFrameTree,
	ownerOrder map[proto.PageFrameID][]proto.PageFrameID,
	child proto.PageFrameID,
) proto.PageFrameID {
	if found := findParentFrameID(root, child); found != "" {
		return found
	}
	ids := make([]proto.PageFrameID, 0, len(extra))
	for id := range extra {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if found := findParentFrameID(extra[id], child); found != "" {
			return found
		}
	}
	return parentFromOwnerOrder(ownerOrder, child)
}

func parentFromOwnerOrder(
	ownerOrder map[proto.PageFrameID][]proto.PageFrameID,
	child proto.PageFrameID,
) proto.PageFrameID {
	if child == "" {
		return ""
	}
	var matches []proto.PageFrameID
	for parent, children := range ownerOrder {
		for _, id := range children {
			if id == child {
				matches = append(matches, parent)
			}
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i] < matches[j] })
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func findParentFrameID(node *proto.PageFrameTree, child proto.PageFrameID) proto.PageFrameID {
	if node == nil || node.Frame == nil {
		return ""
	}
	for _, next := range node.ChildFrames {
		if next != nil && next.Frame != nil && next.Frame.ID == child {
			return node.Frame.ID
		}
		if found := findParentFrameID(next, child); found != "" {
			return found
		}
	}
	return ""
}

func flattenCompleteFrameTree(
	root *proto.PageFrameTree,
	rootSession *sessionClient,
	oopifSessions map[proto.PageFrameID]*sessionClient,
	oopifTrees map[proto.PageFrameID]*proto.PageFrameTree,
) []discoveredFrame {
	frames := make(map[proto.PageFrameID]*proto.PageFrame)
	fallbackChildren := make(map[proto.PageFrameID][]proto.PageFrameID)
	var addTree func(*proto.PageFrameTree)
	addTree = func(node *proto.PageFrameTree) {
		if node == nil || node.Frame == nil {
			return
		}
		frames[node.Frame.ID] = node.Frame
		for _, child := range node.ChildFrames {
			if child == nil || child.Frame == nil {
				continue
			}
			fallbackChildren[node.Frame.ID] = append(fallbackChildren[node.Frame.ID], child.Frame.ID)
			addTree(child)
		}
	}
	addTree(root)
	oopifIDs := make([]proto.PageFrameID, 0, len(oopifTrees))
	for frameID, tree := range oopifTrees {
		oopifIDs = append(oopifIDs, frameID)
		addTree(tree)
	}
	sort.Slice(oopifIDs, func(i, j int) bool { return oopifIDs[i] < oopifIDs[j] })
	for _, frameID := range oopifIDs {
		frame := frames[frameID]
		if frame != nil && frame.ParentID != "" {
			fallbackChildren[frame.ParentID] = append(fallbackChildren[frame.ParentID], frameID)
		}
	}

	ownerOrder := make(map[proto.PageFrameID][]proto.PageFrameID)
	mergeFrameOwnerOrder(ownerOrder, rootSession)
	for _, frameID := range oopifIDs {
		mergeFrameOwnerOrder(ownerOrder, oopifSessions[frameID])
	}
	seedOwnerOrderFrames(frames, fallbackChildren, ownerOrder)

	result := make([]discoveredFrame, 0, len(frames))
	visited := make(map[proto.PageFrameID]bool)
	var visit func(proto.PageFrameID, []observation.FrameReference, *sessionClient, string)
	visit = func(frameID proto.PageFrameID, path []observation.FrameReference, inherited *sessionClient, reason string) {
		frame := frames[frameID]
		if frame == nil || visited[frameID] {
			return
		}
		visited[frameID] = true
		session := inherited
		if oopif := oopifSessions[frameID]; oopif != nil {
			session = oopif
		}
		result = append(result, discoveredFrame{
			frame: frame, path: cloneFramePath(path), session: session, reason: reason,
		})

		ordered, walked := ownerOrder[frameID]
		ordered = append([]proto.PageFrameID{}, ordered...)
		seen := make(map[proto.PageFrameID]bool, len(ordered))
		for _, childID := range ordered {
			seen[childID] = true
		}
		var extras []proto.PageFrameID
		for _, childID := range fallbackChildren[frameID] {
			if !seen[childID] {
				extras = append(extras, childID)
				seen[childID] = true
			}
		}
		sort.Slice(extras, func(i, j int) bool {
			left, right := frames[extras[i]], frames[extras[j]]
			if left == nil || right == nil {
				return extras[i] < extras[j]
			}
			if left.URL != right.URL {
				return left.URL < right.URL
			}
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			return extras[i] < extras[j]
		})
		if !walked {
			// The pierce walk never ran for this document (no session, DOM.getDocument
			// failed, or the snapshot omitted children). Falling through to "every
			// CDP child is closed-shadow" is what made CI mark live light-DOM frames
			// uncovered and skip extraction.
			ordered = append(ordered, extras...)
			extras = nil
		}
		ownerCount := len(ordered)
		ordered = append(ordered, extras...)
		for index, childID := range ordered {
			child := frames[childID]
			if child == nil {
				continue
			}
			childPath := append(cloneFramePath(path), observation.FrameReference{
				Index: index, Name: child.Name, Src: pageurl.PageURL(child.URL),
			})
			childReason := reason
			if walked && index >= ownerCount && childReason == "" {
				childReason = "frame is not visible to page JavaScript (closed shadow or equivalent)"
			}
			visit(childID, childPath, session, childReason)
		}
	}
	visit(root.Frame.ID, []observation.FrameReference{}, rootSession, "")
	return result
}

func seedOwnerOrderFrames(
	frames map[proto.PageFrameID]*proto.PageFrame,
	fallbackChildren map[proto.PageFrameID][]proto.PageFrameID,
	ownerOrder map[proto.PageFrameID][]proto.PageFrameID,
) {
	parents := make([]proto.PageFrameID, 0, len(ownerOrder))
	for parentID := range ownerOrder {
		parents = append(parents, parentID)
	}
	sort.Slice(parents, func(i, j int) bool { return parents[i] < parents[j] })
	for _, parentID := range parents {
		for _, childID := range ownerOrder[parentID] {
			if childID == "" {
				continue
			}
			if frames[childID] == nil {
				frames[childID] = &proto.PageFrame{ID: childID, ParentID: parentID}
			}
			found := false
			for _, existing := range fallbackChildren[parentID] {
				if existing == childID {
					found = true
					break
				}
			}
			if !found {
				fallbackChildren[parentID] = append(fallbackChildren[parentID], childID)
			}
		}
	}
}

func mergeFrameOwnerOrder(
	target map[proto.PageFrameID][]proto.PageFrameID,
	session *sessionClient,
) {
	if session == nil {
		return
	}
	document := piercedDocument(session)
	if document == nil {
		return
	}
	fallback := document.FrameID
	if fallback == "" {
		fallback, _ = sessionMainFrameID(session)
	}
	collectFrameOwnerOrderWithFallback(target, document, fallback)
}

func pierceIncomplete(root *proto.DOMNode) bool {
	if root == nil {
		return true
	}
	if len(root.Children) == 0 && len(root.ShadowRoots) == 0 {
		return true
	}
	var missing func(*proto.DOMNode) bool
	missing = func(node *proto.DOMNode) bool {
		if node == nil {
			return false
		}
		localName := strings.ToLower(node.LocalName)
		if localName == "iframe" || localName == "frame" {
			return false
		}
		if node.ChildNodeCount != nil && *node.ChildNodeCount > 0 && len(node.Children) == 0 {
			return true
		}
		for _, child := range node.Children {
			if missing(child) {
				return true
			}
		}
		for _, shadow := range node.ShadowRoots {
			if missing(shadow) {
				return true
			}
		}
		return false
	}
	return missing(root)
}

// collectFrameOwnerOrder walks a pierced DOM tree and records each document's
// child frames in the order a page-JS walk would see them. Extracted from the
// CDP call so the ordering contract can be tested without a browser (GV-037).
//
// The walk is the Go counterpart of `frameCandidates` in client/src/shared/locate.ts:
// pre-order, light children before open-shadow content, localName iframe|frame
// (not computed role), and no descent past a frame, whose contents belong to a
// different document. Closed shadow trees are skipped so they do not consume an
// index the page-JS walk cannot see (P9). The two implementations are what keep
// FrameReference.Index and a runtime frame path node pointing at the same
// element (GV-003).
func collectFrameOwnerOrder(target map[proto.PageFrameID][]proto.PageFrameID, root *proto.DOMNode) {
	if root == nil {
		return
	}
	collectFrameOwnerOrderWithFallback(target, root, root.FrameID)
}

func collectFrameOwnerOrderWithFallback(
	target map[proto.PageFrameID][]proto.PageFrameID,
	root *proto.DOMNode,
	fallback proto.PageFrameID,
) {
	if root == nil {
		return
	}
	if fallback == "" {
		fallback = root.FrameID
	}
	var walkDocument func(*proto.DOMNode, proto.PageFrameID)
	var walkContent func(*proto.DOMNode, proto.PageFrameID)
	walkDocument = func(node *proto.DOMNode, fallback proto.PageFrameID) {
		if node == nil {
			return
		}
		frameID := node.FrameID
		if frameID == "" {
			frameID = fallback
		}
		if frameID != "" {
			if _, exists := target[frameID]; !exists {
				target[frameID] = []proto.PageFrameID{}
			}
		}
		walkContent(node, frameID)
	}
	walkContent = func(node *proto.DOMNode, documentFrameID proto.PageFrameID) {
		if node == nil {
			return
		}
		localName := strings.ToLower(node.LocalName)
		if localName == "iframe" || localName == "frame" {
			childID := frameOwnerID(node)
			if childID != "" && documentFrameID != "" {
				target[documentFrameID] = append(target[documentFrameID], childID)
			}
			if node.ContentDocument != nil {
				walkDocument(node.ContentDocument, childID)
			}
			return
		}
		for _, child := range node.Children {
			walkContent(child, documentFrameID)
		}
		for _, shadow := range node.ShadowRoots {
			if shadow != nil && shadow.ShadowRootType == proto.DOMShadowRootTypeClosed {
				continue
			}
			walkContent(shadow, documentFrameID)
		}
		if node.ContentDocument != nil {
			walkDocument(node.ContentDocument, node.FrameID)
		}
	}
	walkDocument(root, fallback)
	for parentID, children := range target {
		target[parentID] = stableUniqueFrameIDs(children)
	}
}

func frameOwnerID(node *proto.DOMNode) proto.PageFrameID {
	if node == nil {
		return ""
	}
	if node.FrameID != "" {
		return node.FrameID
	}
	if node.ContentDocument != nil {
		return node.ContentDocument.FrameID
	}
	return ""
}

func stableUniqueFrameIDs(source []proto.PageFrameID) []proto.PageFrameID {
	seen := make(map[proto.PageFrameID]bool, len(source))
	result := make([]proto.PageFrameID, 0, len(source))
	for _, frameID := range source {
		if frameID != "" && !seen[frameID] {
			seen[frameID] = true
			result = append(result, frameID)
		}
	}
	return result
}

func extractFrame(
	frame discoveredFrame,
	options ExtractionOptions,
	frameTimeout time.Duration,
) (observation.Batch, DiagnosticCode, string) {
	if frame.reason != "" {
		return observation.Batch{}, DiagnosticFrameUncovered, frame.reason
	}
	if frame.session == nil {
		return observation.Batch{}, DiagnosticFrameUncovered,
			"browser has no session for this frame"
	}
	if frameTimeout <= 0 {
		frameTimeout = defaultFrameTimeout(explorationBudget(options.TimeoutMS))
	}
	ctx, cancel := context.WithTimeout(frame.session.ctx, frameTimeout)
	defer cancel()
	session := *frame.session
	session.ctx = ctx

	world, err := (proto.PageCreateIsolatedWorld{
		FrameID: frame.frame.ID, WorldName: isolatedWorldName,
	}).Call(&session)
	if err != nil {
		return observation.Batch{}, DiagnosticFrameUncovered,
			"browser could not create an isolated execution world for this frame"
	}
	installed, err := (proto.RuntimeEvaluate{
		Expression: payload.BrowserBundle(), ContextID: world.ExecutionContextID,
		ReturnByValue: true,
	}).Call(&session)
	if err != nil || installed.ExceptionDetails != nil {
		return observation.Batch{}, DiagnosticFrameUncovered,
			"browser payload installation failed in this frame"
	}
	available, err := (proto.RuntimeEvaluate{
		Expression: "typeof globalThis.__GEOVISOR_EXTRACT__ === 'function'",
		ContextID:  world.ExecutionContextID, ReturnByValue: true,
	}).Call(&session)
	if err != nil || available.ExceptionDetails != nil || available.Result == nil ||
		!available.Result.Value.Bool() {
		return observation.Batch{}, DiagnosticFrameUncovered,
			"browser payload extraction function is missing in this frame"
	}

	encodedOptions, _ := json.Marshal(options)
	invocation := "(async () => JSON.stringify(await globalThis.__GEOVISOR_EXTRACT__(" +
		string(encodedOptions) + ")))()"
	extracted, err := (proto.RuntimeEvaluate{
		Expression: invocation, ContextID: world.ExecutionContextID,
		AwaitPromise: true, ReturnByValue: true,
	}).Call(&session)
	if err != nil {
		return observation.Batch{}, DiagnosticFrameUncovered, extractionEvaluateReason(ctx, err)
	}
	if extracted.ExceptionDetails != nil || extracted.Result == nil {
		return observation.Batch{}, DiagnosticFrameUncovered,
			"browser payload raised a JavaScript exception in this frame"
	}
	batch, err := decodeBatchStrict([]byte(extracted.Result.Value.Str()))
	if err != nil {
		return observation.Batch{}, DiagnosticFrameUncovered,
			"browser payload returned malformed observation JSON in this frame"
	}
	batch.Warnings = append(batch.Warnings, closedShadowWarnings(&session, frame.frame.ID)...)
	return batch, "", ""
}

func extractionEvaluateReason(ctx context.Context, err error) string {
	if isExtractionTimeout(ctx, err) {
		return "extraction exceeded the frame timeout"
	}
	return "browser lost the frame context during extraction"
}

func isExtractionTimeout(ctx context.Context, err error) bool {
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	return err != nil && errors.Is(err, context.DeadlineExceeded)
}

func decodeBatchStrict(data []byte) (observation.Batch, error) {
	var batch observation.Batch
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		return batch, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return batch, errors.New("observation JSON has trailing content")
	}
	if batch.Frames == nil {
		batch.Frames = []observation.Frame{}
	}
	if batch.Interactions == nil {
		batch.Interactions = []observation.Interaction{}
	}
	if batch.Warnings == nil {
		batch.Warnings = []observation.Warning{}
	}
	return batch, nil
}

func augmentBatch(batch *observation.Batch, path []observation.FrameReference) {
	nodes := frameTraversalNodes(path)
	for index := range batch.Interactions {
		interaction := &batch.Interactions[index]
		interaction.FramePath = cloneFramePath(path)
		for locatorIndex := range interaction.Locators {
			interaction.Locators[locatorIndex].FramePath = clonePathNodes(nodes)
		}
	}
	for index := range batch.Warnings {
		if len(batch.Warnings[index].FramePath) == 0 {
			batch.Warnings[index].FramePath = cloneFramePath(path)
		}
	}
}

func closedShadowWarnings(
	session *sessionClient,
	frameID proto.PageFrameID,
) []observation.Warning {
	document := piercedDocument(session)
	if document == nil {
		return nil
	}
	return collectClosedShadows(document, frameID)
}

func piercedDocument(session *sessionClient) *proto.DOMNode {
	if session == nil {
		return nil
	}
	// Newer Chromium builds return a root with no children unless the DOM
	// domain is enabled first. Without that snapshot, every CDP child was
	// treated as closed-shadow and skipped.
	_ = (proto.DOMEnable{}).Call(session)
	depth := -1
	document, err := (proto.DOMGetDocument{Depth: &depth, Pierce: true}).Call(session)
	if err != nil || document == nil || pierceIncomplete(document.Root) {
		return nil
	}
	return document.Root
}

func collectClosedShadows(root *proto.DOMNode, documentFrameID proto.PageFrameID) []observation.Warning {
	var warnings []observation.Warning
	var walk func(*proto.DOMNode, proto.PageFrameID)
	walk = func(node *proto.DOMNode, frameID proto.PageFrameID) {
		if node == nil {
			return
		}
		if node.FrameID != "" {
			frameID = node.FrameID
		}
		inTarget := frameID == documentFrameID
		if inTarget {
			for _, shadow := range node.ShadowRoots {
				if shadow != nil && shadow.ShadowRootType == proto.DOMShadowRootTypeClosed {
					warnings = append(warnings, observation.Warning{
						Code:    observation.WarningClosedShadowRoot,
						Message: "closed shadow root on " + closedShadowHostLabel(node),
					})
				}
				walk(shadow, frameID)
			}
		}
		for _, child := range node.Children {
			walk(child, frameID)
		}
		if node.ContentDocument != nil {
			walk(node.ContentDocument, node.FrameID)
		}
	}
	walk(root, documentFrameID)
	return warnings
}

func closedShadowHostLabel(node *proto.DOMNode) string {
	name := strings.ToLower(node.LocalName)
	if name == "" {
		name = "element"
	}
	for index := 0; index+1 < len(node.Attributes); index += 2 {
		if node.Attributes[index] == "id" && node.Attributes[index+1] != "" {
			return name + "#" + node.Attributes[index+1]
		}
	}
	return name
}

// frameTraversalNodes converts a frame path into locator path nodes.
//
// A frame is addressed by its position among the containing document's frames,
// which is what FrameReference.Index records and what mergeFrameOwnerOrder
// numbers. Two things follow, and getting both wrong is GV-003:
//
// No CSS fallback. `:nth-child(N of iframe, frame)` counts among element
// siblings, not among a document's frames, so on two single-frame wrappers
// index 0 matches both elements and index 1 matches none. No CSS selector
// expresses "the Nth frame of this document", and a selector that quietly
// counts something else is worse than none: it resolves to the wrong frame
// instead of reporting that it cannot resolve.
//
// No name. FrameReference.Name is the CDP frame name, taken from the element's
// `name` or `id` attribute. That is not an accessible name and is not what the
// consumer computes, so matching on it never succeeded for named frames, while
// unnamed frames omitted the field and matched the first frame in the document.
// The name stays on Interaction.FramePath, where it is a diagnostic rather than
// a match key.
func frameTraversalNodes(path []observation.FrameReference) []observation.PathNode {
	result := make([]observation.PathNode, len(path))
	for index, reference := range path {
		result[index] = observation.PathNode{
			Semantic: &observation.SemanticNode{Role: frameRole, Nth: reference.Index},
		}
	}
	return result
}

func accessInterstitial(frame discoveredFrame) bool {
	world, err := (proto.PageCreateIsolatedWorld{
		FrameID: frame.frame.ID, WorldName: isolatedWorldName,
	}).Call(frame.session)
	if err != nil {
		return false
	}
	result, err := (proto.RuntimeEvaluate{
		Expression: `(document.title + "\n" + (document.body?.innerText || "")).slice(0, 8192).toLowerCase()`,
		ContextID:  world.ExecutionContextID, ReturnByValue: true,
	}).Call(frame.session)
	if err != nil || result.ExceptionDetails != nil || result.Result == nil {
		return false
	}
	text := result.Result.Value.Str()
	markers := []string{
		"verify you are human", "checking your browser", "attention required",
		"access denied", "captcha", "unusual traffic",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func cloneFramePath(source []observation.FrameReference) []observation.FrameReference {
	return append([]observation.FrameReference{}, source...)
}

func clonePathNodes(source []observation.PathNode) []observation.PathNode {
	result := make([]observation.PathNode, len(source))
	for index, node := range source {
		result[index] = node
		if node.Semantic != nil {
			semantic := *node.Semantic
			result[index].Semantic = &semantic
		}
	}
	return result
}
