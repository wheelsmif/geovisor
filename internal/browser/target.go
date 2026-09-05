package browser

import (
	"context"
	"errors"
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
	focused := make(map[proto.TargetTargetID]bool)
	if selector.Mode == SelectActiveTopLevel {
		candidates := canonicalTopLevelTargets(response.TargetInfos)
		for _, candidate := range candidates {
			focused[candidate.TargetID] = targetHasFocus(ctx, browser, candidate.TargetID)
		}
	}
	return selectTarget(response.TargetInfos, selector, focused)
}

func selectTarget(
	targets []*proto.TargetTargetInfo,
	selector TargetSelector,
	focused map[proto.TargetTargetID]bool,
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
		for _, candidate := range candidates {
			if focused[candidate.TargetID] {
				return candidate, nil
			}
		}
		if len(candidates) > 0 {
			return candidates[0], nil
		}
	}
	return nil, errors.New("no matching top-level HTTP(S) target")
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

func targetHasFocus(ctx context.Context, browser *rod.Browser, targetID proto.TargetTargetID) bool {
	session, err := attachTarget(ctx, browser, targetID)
	if err != nil {
		return false
	}
	defer detachTarget(browser, session.sessionID)
	result, err := (proto.RuntimeEvaluate{
		Expression:    "document.hasFocus()",
		ReturnByValue: true,
	}).Call(session)
	return err == nil && result.ExceptionDetails == nil && result.Result != nil && result.Result.Value.Bool()
}
