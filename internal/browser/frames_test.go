package browser

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-rod/rod/lib/proto"

	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/pageurl"
)

// These cover the live frame-tree functions. The one function that had a test
// was the dead one, which is why GV-003 survived in code that looked covered
// (GV-037). The functions needing a CDP session are exercised through their
// session-free paths; end-to-end frame resolution is asserted by executing the
// emitted module in internal/integration.

func TestFrameTraversalNodesAddressFramesByIndexAlone(t *testing.T) {
	t.Parallel()

	nodes := frameTraversalNodes([]observation.FrameReference{
		{Index: 0, Name: "payments", Src: "https://pay.example.test/"},
		{Index: 3, Name: "", Src: "https://inner.example.test/"},
	})

	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	for index, node := range nodes {
		if node.Semantic == nil {
			t.Fatalf("nodes[%d] has no semantic half", index)
		}
		if node.Semantic.Role != frameRole {
			t.Errorf("nodes[%d].Semantic.Role = %q, want %q", index, node.Semantic.Role, frameRole)
		}
		// The CDP frame name comes from the `name` or `id` attribute, which is
		// not an accessible name and not what the consumer computes. Carrying it
		// as a match key made named frames never match and unnamed frames match
		// the first frame in the document.
		if node.Semantic.Name != "" {
			t.Errorf("nodes[%d].Semantic.Name = %q, want empty", index, node.Semantic.Name)
		}
		// No CSS selector expresses "the Nth frame of this document", and one
		// that counts element siblings instead resolves to the wrong frame
		// rather than failing over.
		if node.CSS != "" {
			t.Errorf("nodes[%d].CSS = %q, want empty", index, node.CSS)
		}
	}
	if nodes[0].Semantic.Nth != 0 {
		t.Errorf("nodes[0].Semantic.Nth = %d, want 0", nodes[0].Semantic.Nth)
	}
	if nodes[1].Semantic.Nth != 3 {
		t.Errorf("nodes[1].Semantic.Nth = %d, want 3", nodes[1].Semantic.Nth)
	}
}

func TestFrameTraversalNodesReturnsEmptyForRootFrame(t *testing.T) {
	t.Parallel()

	if nodes := frameTraversalNodes(nil); len(nodes) != 0 {
		t.Fatalf("got %d nodes for the root frame, want 0", len(nodes))
	}
}

func TestAugmentBatchStampsFramePathOnEveryLocator(t *testing.T) {
	t.Parallel()

	path := []observation.FrameReference{{Index: 1, Name: "inner", Src: "https://inner.example/"}}
	batch := observation.Batch{
		Interactions: []observation.Interaction{
			{
				Name: "first",
				Locators: []observation.Locator{
					{CSS: "input"},
					{Semantic: &observation.SemanticLocator{Role: "textbox", Name: "Query"}},
				},
			},
			{Name: "second", Locators: []observation.Locator{{CSS: "button"}}},
		},
	}

	augmentBatch(&batch, path)

	want := frameTraversalNodes(path)
	for _, interaction := range batch.Interactions {
		if !reflect.DeepEqual(interaction.FramePath, path) {
			t.Errorf("interaction %q FramePath = %+v, want %+v", interaction.Name, interaction.FramePath, path)
		}
		for index, locator := range interaction.Locators {
			if !reflect.DeepEqual(locator.FramePath, want) {
				t.Errorf(
					"interaction %q locator %d FramePath = %+v, want %+v",
					interaction.Name, index, locator.FramePath, want,
				)
			}
		}
	}
}

// Each interaction must own its frame path. Sharing one slice would let a later
// mutation of one interaction's path silently rewrite every other.
func TestAugmentBatchGivesEachLocatorAnIndependentFramePath(t *testing.T) {
	t.Parallel()

	batch := observation.Batch{
		Interactions: []observation.Interaction{
			{Name: "first", Locators: []observation.Locator{{CSS: "input"}}},
			{Name: "second", Locators: []observation.Locator{{CSS: "button"}}},
		},
	}
	augmentBatch(&batch, []observation.FrameReference{{Index: 2}})

	first := batch.Interactions[0].Locators[0].FramePath
	second := batch.Interactions[1].Locators[0].FramePath
	first[0].Semantic.Nth = 99

	if second[0].Semantic.Nth != 2 {
		t.Fatalf("mutating one frame path changed another: got Nth = %d, want 2", second[0].Semantic.Nth)
	}
}

func TestFlattenCompleteFrameTreeNumbersChildrenInOrder(t *testing.T) {
	t.Parallel()

	tree := &proto.PageFrameTree{
		Frame: &proto.PageFrame{ID: "root", URL: "https://example.test/"},
		ChildFrames: []*proto.PageFrameTree{
			{Frame: &proto.PageFrame{ID: "b", ParentID: "root", URL: "https://example.test/b", Name: "b"}},
			{
				Frame: &proto.PageFrame{ID: "a", ParentID: "root", URL: "https://example.test/a", Name: "a"},
				ChildFrames: []*proto.PageFrameTree{
					{Frame: &proto.PageFrame{ID: "a1", ParentID: "a", URL: "https://example.test/a1"}},
				},
			},
		},
	}

	// With no session the DOM owner order is unavailable, so ordering falls back
	// to the deterministic URL, name, then id sort rather than to map order.
	frames := flattenCompleteFrameTree(tree, nil, nil, nil)

	got := make(map[proto.PageFrameID][]observation.FrameReference, len(frames))
	for _, frame := range frames {
		got[frame.frame.ID] = frame.path
	}
	if len(frames) != 4 {
		t.Fatalf("discovered %d frames, want 4", len(frames))
	}
	for _, frame := range frames {
		if frame.reason != "" {
			t.Errorf("frame %s reason = %q, want empty when the pierce walk did not run", frame.frame.ID, frame.reason)
		}
	}
	if path := got["root"]; len(path) != 0 {
		t.Errorf("root path = %+v, want empty", path)
	}
	if path := got["a"]; len(path) != 1 || path[0].Index != 0 || path[0].Name != "a" {
		t.Errorf("frame a path = %+v, want one reference with index 0 and name a", path)
	}
	if path := got["b"]; len(path) != 1 || path[0].Index != 1 || path[0].Name != "b" {
		t.Errorf("frame b path = %+v, want one reference with index 1 and name b", path)
	}
	if path := got["a1"]; len(path) != 2 || path[0].Index != 0 || path[1].Index != 0 {
		t.Errorf("frame a1 path = %+v, want indexes 0 then 0", path)
	}
}

func TestFlattenCompleteFrameTreeIncludesOutOfProcessFrames(t *testing.T) {
	t.Parallel()

	root := &proto.PageFrameTree{Frame: &proto.PageFrame{ID: "root", URL: "https://example.test/"}}
	trees := map[proto.PageFrameID]*proto.PageFrameTree{}
	addUnattachedOOPIF(trees, &proto.TargetTargetInfo{
		TargetID: "oopif", URL: "https://other.example/widget",
	}, "root")

	frames := flattenCompleteFrameTree(root, nil, nil, trees)

	var found *discoveredFrame
	for index := range frames {
		if frames[index].frame.ID == "oopif" {
			found = &frames[index]
		}
	}
	if found == nil {
		t.Fatalf("out-of-process frame was not discovered; got %d frames", len(frames))
	}
	if len(found.path) != 1 || found.path[0].Index != 0 {
		t.Errorf("out-of-process frame path = %+v, want one reference with index 0", found.path)
	}
	if found.frame.SecurityOrigin != "https://other.example" {
		t.Errorf("SecurityOrigin = %q, want %q", found.frame.SecurityOrigin, "https://other.example")
	}
}

func TestCollectFrameOwnerOrderNumbersSiblingFramesInDocumentOrder(t *testing.T) {
	t.Parallel()

	// Two single-frame wrappers: the GV-003 case. Counting among element
	// siblings would give both frames position 0. Counting among the
	// document's frames gives 0 then 1.
	root := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{{
			LocalName: "html",
			Children: []*proto.DOMNode{{
				LocalName: "body",
				Children: []*proto.DOMNode{
					{LocalName: "div", Children: []*proto.DOMNode{
						{LocalName: "iframe", FrameID: "one"},
					}},
					{LocalName: "div", Children: []*proto.DOMNode{
						{LocalName: "iframe", FrameID: "two"},
					}},
				},
			}},
		}},
	}

	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrder(got, root)

	want := []proto.PageFrameID{"one", "two"}
	if !reflect.DeepEqual(got["root"], want) {
		t.Fatalf("root children = %v, want %v", got["root"], want)
	}
}

func TestCollectFrameOwnerOrderCountsShadowHostedFramesAfterLightSiblings(t *testing.T) {
	t.Parallel()

	// Light children before shadow content, matching frameCandidates. A frame
	// inside an earlier host's shadow is still earlier than a later light
	// sibling frame. testdata/corpus/shadow-frame.html is the same tree as a
	// real DOM; client/test/locate.test.mjs runs the TypeScript walk against it.
	// jsdom does not give shadow-hosted iframes a contentDocument, so the
	// agreement is the numbering of the frames, not an executed fill inside them.
	root := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{{
			LocalName: "body",
			Children: []*proto.DOMNode{
				{
					LocalName: "host",
					ShadowRoots: []*proto.DOMNode{
						{LocalName: "iframe", FrameID: "shadowed"},
					},
				},
				{LocalName: "iframe", FrameID: "light"},
			},
		}},
	}

	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrder(got, root)

	want := []proto.PageFrameID{"shadowed", "light"}
	if !reflect.DeepEqual(got["root"], want) {
		t.Fatalf("root children = %v, want %v", got["root"], want)
	}
}

func TestCollectFrameOwnerOrderDoesNotCountNestedFramesAsSiblings(t *testing.T) {
	t.Parallel()

	// Descent stops at a frame. The inner frame belongs to the child document,
	// so it must not take an index in the parent.
	root := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{
			{
				LocalName: "iframe",
				FrameID:   "outer",
				ContentDocument: &proto.DOMNode{
					FrameID:  "outer",
					Children: []*proto.DOMNode{{LocalName: "iframe", FrameID: "inner"}},
				},
			},
			{LocalName: "iframe", FrameID: "sibling"},
		},
	}

	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrder(got, root)

	if !reflect.DeepEqual(got["root"], []proto.PageFrameID{"outer", "sibling"}) {
		t.Fatalf("root children = %v, want [outer sibling]", got["root"])
	}
	if !reflect.DeepEqual(got["outer"], []proto.PageFrameID{"inner"}) {
		t.Fatalf("outer children = %v, want [inner]", got["outer"])
	}
}

func TestCollectFrameOwnerOrderDeduplicatesAndDropsEmptyIDs(t *testing.T) {
	t.Parallel()

	root := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{
			{LocalName: "iframe", FrameID: "kept"},
			{LocalName: "iframe"},
			{LocalName: "iframe", FrameID: "kept"},
		},
	}

	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrder(got, root)

	if !reflect.DeepEqual(got["root"], []proto.PageFrameID{"kept"}) {
		t.Fatalf("root children = %v, want [kept]", got["root"])
	}
}

func TestCollectFrameOwnerOrderUsesContentDocumentFrameID(t *testing.T) {
	t.Parallel()

	root := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{{
			LocalName: "iframe",
			ContentDocument: &proto.DOMNode{
				FrameID: "from-content",
				Children: []*proto.DOMNode{
					{LocalName: "iframe", FrameID: "inner"},
				},
			},
		}},
	}

	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrder(got, root)
	if !reflect.DeepEqual(got["root"], []proto.PageFrameID{"from-content"}) {
		t.Fatalf("root children = %v, want [from-content]", got["root"])
	}
	if !reflect.DeepEqual(got["from-content"], []proto.PageFrameID{"inner"}) {
		t.Fatalf("content-document children = %v, want [inner]", got["from-content"])
	}
}

func TestCollectFrameOwnerOrderRecordsDocumentsWithNoFrames(t *testing.T) {
	t.Parallel()

	root := &proto.DOMNode{
		FrameID:  "root",
		Children: []*proto.DOMNode{{LocalName: "body"}},
	}
	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrder(got, root)
	children, walked := got["root"]
	if !walked {
		t.Fatal("walked document was not recorded")
	}
	if len(children) != 0 {
		t.Fatalf("root children = %v, want none", children)
	}
}

func TestCollectFrameOwnerOrderUsesFallbackWhenDocumentFrameIDEmpty(t *testing.T) {
	t.Parallel()

	root := &proto.DOMNode{
		Children: []*proto.DOMNode{
			{LocalName: "iframe", FrameID: "oopif"},
			{LocalName: "iframe", FrameID: "nested"},
		},
	}
	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrderWithFallback(got, root, "page")
	if !reflect.DeepEqual(got["page"], []proto.PageFrameID{"oopif", "nested"}) {
		t.Fatalf("page children = %v, want [oopif nested]", got["page"])
	}
}

func TestPierceIncompleteDetectsOmittedChildren(t *testing.T) {
	t.Parallel()

	if !pierceIncomplete(nil) {
		t.Fatal("nil root should be incomplete")
	}
	count := 3
	if !pierceIncomplete(&proto.DOMNode{ChildNodeCount: &count}) {
		t.Fatal("childNodeCount without children should be incomplete")
	}
	if !pierceIncomplete(&proto.DOMNode{
		Children: []*proto.DOMNode{{
			LocalName: "html", ChildNodeCount: &count,
		}},
	}) {
		t.Fatal("nested childNodeCount without children should be incomplete")
	}
	if pierceIncomplete(&proto.DOMNode{Children: []*proto.DOMNode{{LocalName: "html"}}}) {
		t.Fatal("populated children should be complete")
	}
	if !pierceIncomplete(&proto.DOMNode{FrameID: "root"}) {
		t.Fatal("document root with no children should be incomplete")
	}
	iframeCount := 2
	if pierceIncomplete(&proto.DOMNode{
		Children: []*proto.DOMNode{{
			LocalName: "iframe", ChildNodeCount: &iframeCount,
		}},
	}) {
		t.Fatal("omitted iframe fallback content should not mark the parent walk incomplete")
	}
}

func TestMergeFrameOwnerOrderNilSessionLeavesMapUntouched(t *testing.T) {
	t.Parallel()

	target := map[proto.PageFrameID][]proto.PageFrameID{"root": {"existing"}}
	mergeFrameOwnerOrder(target, nil)
	if !reflect.DeepEqual(target["root"], []proto.PageFrameID{"existing"}) {
		t.Fatalf("nil session mutated the map: %v", target)
	}
}

func TestAddUnattachedOOPIFRecordsOriginFromURL(t *testing.T) {
	t.Parallel()

	trees := map[proto.PageFrameID]*proto.PageFrameTree{}
	addUnattachedOOPIF(trees, &proto.TargetTargetInfo{
		TargetID: "widget", URL: "https://other.example/path?q=1#frag",
	}, "root")

	tree := trees["widget"]
	if tree == nil || tree.Frame == nil {
		t.Fatal("unattached OOPIF was not recorded")
	}
	if tree.Frame.URL != "https://other.example/path?q=1#frag" {
		t.Errorf("URL = %q", tree.Frame.URL)
	}
	if tree.Frame.SecurityOrigin != "https://other.example" {
		t.Errorf("SecurityOrigin = %q, want https://other.example", tree.Frame.SecurityOrigin)
	}
	if tree.Frame.ParentID != "root" {
		t.Errorf("ParentID = %q, want root", tree.Frame.ParentID)
	}
}

func TestDiscoverOOPIFsRefetchesTargetsAfterParentAttach(t *testing.T) {
	t.Parallel()

	parent := &proto.TargetTargetInfo{TargetID: "parent", Type: "iframe", URL: "https://a.example/"}
	child := &proto.TargetTargetInfo{
		TargetID: "child", Type: "iframe", URL: "https://b.example/", OpenerFrameID: "parent",
	}
	calls := 0
	list := func() ([]*proto.TargetTargetInfo, error) {
		calls++
		if calls == 1 {
			return []*proto.TargetTargetInfo{parent}, nil
		}
		return []*proto.TargetTargetInfo{parent, child}, nil
	}
	var attached []string
	attach := func(target *proto.TargetTargetInfo) (*sessionClient, *proto.PageFrameTree, error) {
		attached = append(attached, string(target.TargetID))
		tree := &proto.PageFrameTree{Frame: &proto.PageFrame{
			ID: proto.PageFrameID(target.TargetID), URL: target.URL,
		}}
		if target.TargetID == "parent" {
			tree.ChildFrames = []*proto.PageFrameTree{{
				Frame: &proto.PageFrame{ID: "child", ParentID: "parent"},
			}}
		}
		return &sessionClient{}, tree, nil
	}
	root := &proto.PageFrameTree{
		Frame:       &proto.PageFrame{ID: "root"},
		ChildFrames: []*proto.PageFrameTree{{Frame: &proto.PageFrame{ID: "parent"}}},
	}

	_, trees, diagnostics := discoverOOPIFs("page", root, nil, list, attach)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if len(attached) != 2 || attached[0] != "parent" || attached[1] != "child" {
		t.Fatalf("attached = %v, want parent then child", attached)
	}
	if trees["child"] == nil {
		t.Fatal("nested OOPIF was not recorded after refetch")
	}
	if calls < 2 {
		t.Fatalf("listTargets calls = %d, want at least 2", calls)
	}
}

func TestDiscoverOOPIFsReportsGetTargetsFailure(t *testing.T) {
	t.Parallel()

	_, _, diagnostics := discoverOOPIFs(
		"page",
		&proto.PageFrameTree{Frame: &proto.PageFrame{ID: "root"}},
		nil,
		func() ([]*proto.TargetTargetInfo, error) { return nil, errors.New("cdp closed") },
		func(*proto.TargetTargetInfo) (*sessionClient, *proto.PageFrameTree, error) {
			t.Fatal("attach should not run when listing targets fails")
			return nil, nil, nil
		},
	)
	if len(diagnostics) != 1 || diagnostics[0].Code != DiagnosticFrameUncovered {
		t.Fatalf("diagnostics = %+v, want one frame_uncovered", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "could not list targets") {
		t.Fatalf("message = %q", diagnostics[0].Message)
	}
}

func TestCollectFrameOwnerOrderSkipsClosedShadowFrames(t *testing.T) {
	t.Parallel()

	root := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{{
			LocalName: "body",
			Children: []*proto.DOMNode{
				{
					LocalName: "host",
					ShadowRoots: []*proto.DOMNode{{
						ShadowRootType: proto.DOMShadowRootTypeClosed,
						Children: []*proto.DOMNode{
							{LocalName: "iframe", FrameID: "closed"},
						},
					}},
				},
				{LocalName: "iframe", FrameID: "open"},
			},
		}},
	}

	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrder(got, root)
	if !reflect.DeepEqual(got["root"], []proto.PageFrameID{"open"}) {
		t.Fatalf("root children = %v, want [open] without the closed-shadow frame", got["root"])
	}
}

func TestCollectFrameOwnerOrderCountsPresentationRoleIframes(t *testing.T) {
	t.Parallel()

	root := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{
			{LocalName: "iframe", FrameID: "presentational"},
			{LocalName: "iframe", FrameID: "normal"},
		},
	}

	got := map[proto.PageFrameID][]proto.PageFrameID{}
	collectFrameOwnerOrder(got, root)
	if !reflect.DeepEqual(got["root"], []proto.PageFrameID{"presentational", "normal"}) {
		t.Fatalf("root children = %v, want presentational then normal", got["root"])
	}
}

func TestPageURLStripsQueryAndFragment(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ raw, want string }{
		{raw: "https://example.test/path?token=CURRENT-URL-SECRET#CURRENT-FRAGMENT-SECRET", want: "https://example.test/path"},
		{raw: "http://example.test:8080/", want: "http://example.test:8080/"},
		{raw: "about:blank", want: "about:blank"},
		{raw: "", want: ""},
		{raw: "https://example.test/foo  bar", want: "https://example.test/foo%20bar"},
	} {
		if got := pageurl.PageURL(test.raw); got != test.want {
			t.Errorf("PageURL(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestDescendantIFrameFilterIgnoresForeignTargets(t *testing.T) {
	t.Parallel()

	root := &proto.PageFrameTree{
		Frame: &proto.PageFrame{ID: "root"},
		ChildFrames: []*proto.PageFrameTree{{
			Frame: &proto.PageFrame{ID: "child-oopif"},
		}},
	}
	allowed := descendantFrameIDs(root)
	if !isDescendantIFrameTarget(allowed, "page", &proto.TargetTargetInfo{
		TargetID: "child-oopif", Type: "iframe",
	}) {
		t.Fatal("selected-page OOPIF was rejected")
	}
	if isDescendantIFrameTarget(allowed, "page", &proto.TargetTargetInfo{
		TargetID: "other-tab-iframe", Type: "iframe",
	}) {
		t.Fatal("foreign iframe target was accepted")
	}
	if isDescendantIFrameTarget(allowed, "page", &proto.TargetTargetInfo{
		TargetID: "child-oopif", Type: proto.TargetTargetInfoTypePage,
	}) {
		t.Fatal("page target was accepted as an iframe")
	}
	if !isDescendantIFrameTarget(allowed, "page", &proto.TargetTargetInfo{
		TargetID: "missing-from-tree", Type: "iframe", OpenerFrameID: "root",
	}) {
		t.Fatal("iframe whose opener is an allowed frame was rejected")
	}
	if isDescendantIFrameTarget(allowed, "page", &proto.TargetTargetInfo{
		TargetID: "other-tab-iframe", Type: "iframe", OpenerFrameID: "other-tab",
	}) {
		t.Fatal("iframe whose opener is a foreign frame was accepted")
	}
}

func TestDiscoverOOPIFsAttachesTargetListedOnlyInOwnerOrder(t *testing.T) {
	t.Parallel()

	root := &proto.PageFrameTree{Frame: &proto.PageFrame{ID: "root"}}
	ownerOrder := map[proto.PageFrameID][]proto.PageFrameID{"root": {"oopif"}}
	var attached []string
	_, trees, diagnostics := discoverOOPIFs(
		"page",
		root,
		ownerOrder,
		func() ([]*proto.TargetTargetInfo, error) {
			return []*proto.TargetTargetInfo{{
				TargetID: "oopif", Type: "iframe", URL: "https://other.example/",
			}}, nil
		},
		func(target *proto.TargetTargetInfo) (*sessionClient, *proto.PageFrameTree, error) {
			attached = append(attached, string(target.TargetID))
			return &sessionClient{}, &proto.PageFrameTree{Frame: &proto.PageFrame{
				ID: proto.PageFrameID(target.TargetID), ParentID: "root", URL: target.URL,
			}}, nil
		},
	)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if len(attached) != 1 || attached[0] != "oopif" {
		t.Fatalf("attached = %v, want [oopif]", attached)
	}
	if trees["oopif"] == nil {
		t.Fatal("owner-order OOPIF was not recorded")
	}
}

func TestDiscoverOOPIFsParentsUnattachedTargetFromOwnerOrder(t *testing.T) {
	t.Parallel()

	root := &proto.PageFrameTree{Frame: &proto.PageFrame{ID: "root"}}
	_, trees, _ := discoverOOPIFs(
		"page",
		root,
		map[proto.PageFrameID][]proto.PageFrameID{"root": {"oopif"}},
		func() ([]*proto.TargetTargetInfo, error) {
			return []*proto.TargetTargetInfo{{
				TargetID: "oopif", Type: "iframe", URL: "https://other.example/",
			}}, nil
		},
		func(*proto.TargetTargetInfo) (*sessionClient, *proto.PageFrameTree, error) {
			return nil, nil, errors.New("attach refused")
		},
	)
	tree := trees["oopif"]
	if tree == nil || tree.Frame == nil || tree.Frame.ParentID != "root" {
		t.Fatalf("unattached OOPIF parent = %+v, want root", tree)
	}
}

func TestSeedOwnerOrderFramesAddsOmittedChild(t *testing.T) {
	t.Parallel()

	frames := map[proto.PageFrameID]*proto.PageFrame{"root": {ID: "root"}}
	fallback := map[proto.PageFrameID][]proto.PageFrameID{}
	seedOwnerOrderFrames(frames, fallback, map[proto.PageFrameID][]proto.PageFrameID{
		"root": {"oopif"},
	})
	if frames["oopif"] == nil || frames["oopif"].ParentID != "root" {
		t.Fatalf("seeded frame = %+v, want parent root", frames["oopif"])
	}
	if !reflect.DeepEqual(fallback["root"], []proto.PageFrameID{"oopif"}) {
		t.Fatalf("fallback children = %v, want [oopif]", fallback["root"])
	}
}

func TestParentFromOwnerOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	got := parentFromOwnerOrder(map[proto.PageFrameID][]proto.PageFrameID{
		"b": {"child"},
		"a": {"child"},
	}, "child")
	if got != "a" {
		t.Fatalf("parent = %q, want the lexicographically first match", got)
	}
}

func TestCollectClosedShadowsRecordsAuthorClosedRoots(t *testing.T) {
	t.Parallel()

	root := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{{
			LocalName:  "widget-host",
			Attributes: []string{"id", "account"},
			ShadowRoots: []*proto.DOMNode{{
				ShadowRootType: proto.DOMShadowRootTypeClosed,
				Children: []*proto.DOMNode{{
					LocalName: "button",
				}},
			}},
		}, {
			LocalName: "input",
			ShadowRoots: []*proto.DOMNode{{
				ShadowRootType: proto.DOMShadowRootTypeUserAgent,
			}},
		}},
	}

	warnings := collectClosedShadows(root, "root")
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1: %+v", len(warnings), warnings)
	}
	if warnings[0].Code != observation.WarningClosedShadowRoot {
		t.Fatalf("code = %q", warnings[0].Code)
	}
	if warnings[0].Message != "closed shadow root on widget-host#account" {
		t.Fatalf("message = %q", warnings[0].Message)
	}

	nested := &proto.DOMNode{
		FrameID: "root",
		Children: []*proto.DOMNode{{
			LocalName: "iframe",
			FrameID:   "child",
			ContentDocument: &proto.DOMNode{
				FrameID: "child",
				Children: []*proto.DOMNode{{
					LocalName: "host",
					ShadowRoots: []*proto.DOMNode{{
						ShadowRootType: proto.DOMShadowRootTypeClosed,
					}},
				}},
			},
		}},
	}
	if got := collectClosedShadows(nested, "root"); len(got) != 0 {
		t.Fatalf("root frame reported a child-frame shadow: %+v", got)
	}
	childWarnings := collectClosedShadows(nested, "child")
	if len(childWarnings) != 1 || childWarnings[0].Message != "closed shadow root on host" {
		t.Fatalf("child-frame shadows = %+v", childWarnings)
	}
}

func TestExtractionEvaluateReasonDistinguishesTimeout(t *testing.T) {
	t.Parallel()

	timedOut, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-timedOut.Done()
	if got := extractionEvaluateReason(timedOut, errors.New("cdp evaluate failed")); got !=
		"extraction exceeded the frame timeout" {
		t.Fatalf("timeout reason = %q", got)
	}

	lost := extractionEvaluateReason(context.Background(), errors.New("session closed"))
	if lost != "browser lost the frame context during extraction" {
		t.Fatalf("context-loss reason = %q", lost)
	}
	if extractionEvaluateReason(context.Background(), context.DeadlineExceeded) !=
		"extraction exceeded the frame timeout" {
		t.Fatal("wrapped deadline was not reported as a timeout")
	}
}

func TestOriginForURLRejectsUnusableURLs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ raw, want string }{
		{raw: "https://example.test/path?query#fragment", want: "https://example.test"},
		{raw: "http://example.test:8080/", want: "http://example.test:8080"},
		{raw: "about:blank", want: ""},
		{raw: "data:text/html,hi", want: ""},
		{raw: "", want: ""},
	} {
		if got := pageurl.Origin(test.raw); got != test.want {
			t.Errorf("Origin(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}
