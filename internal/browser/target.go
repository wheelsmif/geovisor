package browser

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type sessionClient struct {
	ctx       context.Context
	browser   *rod.Browser
	sessionID proto.TargetSessionID
}

func (client *sessionClient) Call(
	ctx context.Context,
	sessionID, method string,
	params interface{},
) ([]byte, error) {
	return client.browser.Call(ctx, sessionID, method, params)
}

func (client *sessionClient) GetContext() context.Context {
	return client.ctx
}

func (client *sessionClient) GetSessionID() proto.TargetSessionID {
	return client.sessionID
}

func attachTarget(
	ctx context.Context,
	browser *rod.Browser,
	targetID proto.TargetTargetID,
) (*sessionClient, error) {
	attached, err := (proto.TargetAttachToTarget{
		TargetID: targetID,
		Flatten:  true,
	}).Call(browser.Context(ctx))
	if err != nil {
		return nil, err
	}
	return &sessionClient{ctx: ctx, browser: browser, sessionID: attached.SessionID}, nil
}

func detachTarget(browser *rod.Browser, sessionID proto.TargetSessionID) {
	if browser == nil || sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultDOMQuietLimit)
	defer cancel()
	_ = (proto.TargetDetachFromTarget{SessionID: sessionID}).Call(browser.Context(ctx))
}

func chooseTarget(
	ctx context.Context,
	browser *rod.Browser,
	selector TargetSelector,
) (*proto.TargetTargetInfo, error) {
	response, err := (proto.TargetGetTargets{}).Call(browser.Context(ctx))
	if err != nil {
		return nil, err
	}
	// Never attach to a tab to ask whether it has focus (GV-011). Selection
	// uses only Target.getTargets plus the caller's explicit selector.
	return selectTarget(response.TargetInfos, selector)
}

func selectTarget(
	targets []*proto.TargetTargetInfo,
	selector TargetSelector,
) (*proto.TargetTargetInfo, error) {
	candidates := canonicalTopLevelTargets(targets)
	switch selector.Mode {
	case SelectExactTargetID:
		for _, candidate := range candidates {
			if string(candidate.TargetID) == selector.TargetID {
				return candidate, nil
			}
		}
	case SelectExactURL:
		for _, candidate := range candidates {
			if candidate.URL == selector.URL {
				return candidate, nil
			}
		}
	case SelectActiveTopLevel:
		if len(candidates) == 1 {
			return candidates[0], nil
		}
		if len(candidates) > 1 {
			return nil, fmt.Errorf(
				"multiple top-level HTTP(S) pages are open (%d); pass --target id:<id> or --target url:<url>",
				len(candidates),
			)
		}
		return nil, fmt.Errorf(
			"no matching top-level HTTP(S) target: found 0 HTTP(S) pages; %d non-HTTP(S) tab(s) were ignored",
			countIgnoredTopLevelTabs(targets),
		)
	}
	return nil, errors.New("no matching top-level HTTP(S) target")
}

func countIgnoredTopLevelTabs(targets []*proto.TargetTargetInfo) int {
	count := 0
	for _, target := range targets {
		if target == nil || target.Type != proto.TargetTargetInfoTypePage {
			continue
		}
		if _, err := validateTargetURL(target.URL); err != nil {
			count++
		}
	}
	return count
}

func canonicalTopLevelTargets(targets []*proto.TargetTargetInfo) []*proto.TargetTargetInfo {
	result := make([]*proto.TargetTargetInfo, 0, len(targets))
	for _, target := range targets {
		if target == nil || target.Type != proto.TargetTargetInfoTypePage {
			continue
		}
		if _, err := validateTargetURL(target.URL); err != nil {
			continue
		}
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].URL != result[j].URL {
			return result[i].URL < result[j].URL
		}
		if result[i].Title != result[j].Title {
			return result[i].Title < result[j].Title
		}
		return result[i].TargetID < result[j].TargetID
	})
	return result
}
