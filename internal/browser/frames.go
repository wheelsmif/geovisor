package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/geo-suite/geovisor/internal/compiler"
	"github.com/geo-suite/geovisor/internal/observation"
	"github.com/geo-suite/geovisor/internal/payload"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

const isolatedWorldName = "__geovisor_observer__"

type observeTargetOptions struct {
	requestedURL   string
	navigate       bool
	quietPeriod    time.Duration
	quietTimeout   time.Duration
	extraction     ExtractionOptions
	sourceKind     observation.SourceKind
	stealthEnabled bool
}

type discoveredFrame struct {
	frame   *proto.PageFrame
	path    []observation.FrameReference
	session *sessionClient
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
	}
	if err := waitForDocumentReady(ctx, rootSession); err != nil {
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

	oopifSessions, oopifTrees, detach := attachOOPIFSessions(ctx, browser, targetID)
	defer detach()
	frames := flattenCompleteFrameTree(tree.FrameTree, rootSession, oopifSessions, oopifTrees)
	batch := observation.Batch{
		CoverageReported: true,
		Frames:           make([]observation.Frame, 0, len(frames)),
		Interactions:     []observation.Interaction{},
	}

	for _, frame := range frames {
		fact := observation.Frame{
			Path:       cloneFramePath(frame.path),
			Accessible: true,
			URL:        frame.frame.URL,
			Origin:     frame.frame.SecurityOrigin,
		}
		extracted, code, reason := extractFrame(frame, options.extraction)
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

func waitForDocumentReady(ctx context.Context, session *sessionClient) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := (proto.RuntimeEvaluate{
			Expression:    "document.readyState === 'complete'",
			ReturnByValue: true,
		}).Call(session)
		if err == nil && result.ExceptionDetails == nil && result.Result != nil && result.Result.Value.Bool() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForDOMQuiet(
	session *sessionClient,
	quietPeriod, limit time.Duration,
) (bool, error) {
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
	result, err := (proto.RuntimeEvaluate{
		Expression: expression, AwaitPromise: true, ReturnByValue: true,
		Timeout: proto.RuntimeTimeDelta(limitMS + quietMS + 100),
	}).Call(session)
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
) (
	map[proto.PageFrameID]*sessionClient,
	map[proto.PageFrameID]*proto.PageFrameTree,
	func(),
) {
	result := make(map[proto.PageFrameID]*sessionClient)
	trees := make(map[proto.PageFrameID]*proto.PageFrameTree)
	var attached []*sessionClient
	targets, err := (proto.TargetGetTargets{}).Call(browser.Context(ctx))
	if err == nil {
		sort.Slice(targets.TargetInfos, func(i, j int) bool {
			if targets.TargetInfos[i].URL != targets.TargetInfos[j].URL {
				return targets.TargetInfos[i].URL < targets.TargetInfos[j].URL
			}
			return targets.TargetInfos[i].TargetID < targets.TargetInfos[j].TargetID
		})
		for _, target := range targets.TargetInfos {
			if target == nil || string(target.Type) != "iframe" || target.TargetID == rootTargetID {
				continue
			}
			attachContext, cancel := context.WithTimeout(ctx, defaultDOMQuietLimit)
			session, attachErr := attachTarget(attachContext, browser, target.TargetID)
			if attachErr != nil {
				cancel()
				addUnattachedOOPIF(trees, target)
				continue
			}
			tree, treeErr := (proto.PageGetFrameTree{}).Call(session)
			session.ctx = ctx
			cancel()
			if treeErr != nil || tree.FrameTree == nil || tree.FrameTree.Frame == nil {
				detachTarget(browser, session.sessionID)
				addUnattachedOOPIF(trees, target)
				continue
			}
			result[tree.FrameTree.Frame.ID] = session
			trees[tree.FrameTree.Frame.ID] = tree.FrameTree
			attached = append(attached, session)
		}
	}
	return result, trees, func() {
		for index := len(attached) - 1; index >= 0; index-- {
			detachTarget(browser, attached[index].sessionID)
		}
	}
}

func addUnattachedOOPIF(
	trees map[proto.PageFrameID]*proto.PageFrameTree,
	target *proto.TargetTargetInfo,
) {
	frameID := proto.PageFrameID(target.TargetID)
	trees[frameID] = &proto.PageFrameTree{Frame: &proto.PageFrame{
		ID: frameID, URL: target.URL, SecurityOrigin: originForURL(target.URL),
	}}
}

func originForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func flattenFrameTree(
	root *proto.PageFrameTree,
	rootSession *sessionClient,
	oopifSessions map[proto.PageFrameID]*sessionClient,
) []discoveredFrame {
	result := make([]discoveredFrame, 0)
	var visit func(*proto.PageFrameTree, []observation.FrameReference, *sessionClient)
	visit = func(node *proto.PageFrameTree, path []observation.FrameReference, inherited *sessionClient) {
		if node == nil || node.Frame == nil {
			return
		}
		session := inherited
		if oopif := oopifSessions[node.Frame.ID]; oopif != nil {
			session = oopif
		}
		result = append(result, discoveredFrame{
			frame: node.Frame, path: cloneFramePath(path), session: session,
		})
		for index, child := range node.ChildFrames {
			if child == nil || child.Frame == nil {
				continue
			}
			childPath := append(cloneFramePath(path), observation.FrameReference{
				Index: index, Name: child.Frame.Name, Src: child.Frame.URL,
			})
			visit(child, childPath, session)
		}
	}
	visit(root, []observation.FrameReference{}, rootSession)
	return result
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

	result := make([]discoveredFrame, 0, len(frames))
	visited := make(map[proto.PageFrameID]bool)
	var visit func(proto.PageFrameID, []observation.FrameReference, *sessionClient)
	visit = func(frameID proto.PageFrameID, path []observation.FrameReference, inherited *sessionClient) {
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
			frame: frame, path: cloneFramePath(path), session: session,
		})

		ordered := append([]proto.PageFrameID{}, ownerOrder[frameID]...)
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
			if left.URL != right.URL {
				return left.URL < right.URL
			}
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			return extras[i] < extras[j]
		})
		ordered = append(ordered, extras...)
		for index, childID := range ordered {
			child := frames[childID]
			if child == nil {
				continue
			}
			childPath := append(cloneFramePath(path), observation.FrameReference{
				Index: index, Name: child.Name, Src: child.URL,
			})
			visit(childID, childPath, session)
		}
	}
	visit(root.Frame.ID, []observation.FrameReference{}, rootSession)
	return result
}

func mergeFrameOwnerOrder(
	target map[proto.PageFrameID][]proto.PageFrameID,
	session *sessionClient,
) {
	if session == nil {
		return
	}
	depth := -1
	document, err := (proto.DOMGetDocument{Depth: &depth, Pierce: true}).Call(session)
	if err != nil || document.Root == nil {
		return
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
		walkContent(node, frameID)
	}
	walkContent = func(node *proto.DOMNode, documentFrameID proto.PageFrameID) {
		if node == nil {
			return
		}
		localName := strings.ToLower(node.LocalName)
		if (localName == "iframe" || localName == "frame") && node.FrameID != "" {
			target[documentFrameID] = append(target[documentFrameID], node.FrameID)
			if node.ContentDocument != nil {
				walkDocument(node.ContentDocument, node.FrameID)
			}
			return
		}
		for _, child := range node.Children {
			walkContent(child, documentFrameID)
		}
		for _, shadow := range node.ShadowRoots {
			walkContent(shadow, documentFrameID)
		}
		if node.ContentDocument != nil {
			walkDocument(node.ContentDocument, node.FrameID)
		}
	}
	walkDocument(document.Root, document.Root.FrameID)
	for parentID, children := range target {
		target[parentID] = stableUniqueFrameIDs(children)
	}
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
) (observation.Batch, DiagnosticCode, string) {
	frameTimeout := 3 * time.Second
	if configured := time.Duration(options.TimeoutMS)*time.Millisecond + 2*time.Second; configured > frameTimeout {
		frameTimeout = configured
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
		return observation.Batch{}, DiagnosticFrameUncovered,
			"browser lost the frame context during extraction"
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
	return batch, "", ""
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
}

func frameTraversalNodes(path []observation.FrameReference) []observation.PathNode {
	result := make([]observation.PathNode, len(path))
	for index, reference := range path {
		semantic := &observation.SemanticNode{Role: "iframe", Name: reference.Name}
		result[index] = observation.PathNode{
			Semantic: semantic,
			CSS:      fmt.Sprintf(":is(iframe, frame):nth-child(%d of iframe, frame)", reference.Index+1),
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
