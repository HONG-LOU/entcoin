package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/HONG-LOU/entcoin/entpay"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type EntPayOverview struct {
	Available   bool                   `json:"available"`
	Error       string                 `json:"error,omitempty"`
	Sessions    []entpay.ClientSession `json:"sessions"`
	LaunchError string                 `json:"launch_error,omitempty"`
}

func (a *App) GetEntPayOverview() (EntPayOverview, error) {
	manager, ctx, startErr := a.entpayClient()
	a.mu.Lock()
	launchError := a.launchError
	a.launchError = false
	a.mu.Unlock()
	launchMessage := ""
	if launchError {
		launchMessage = "The EntPay payment link was invalid and was not opened."
	}
	if manager == nil {
		message := "Agent Pay is starting"
		if startErr != nil {
			message = "Agent Pay storage is unavailable"
		}
		return EntPayOverview{Error: message, LaunchError: launchMessage, Sessions: []entpay.ClientSession{}}, nil
	}
	sessions, err := manager.Sessions(ctx, 200)
	if err != nil {
		return EntPayOverview{}, err
	}
	return EntPayOverview{Available: true, LaunchError: launchMessage, Sessions: sessions}, nil
}

func (a *App) GetEntPaySession(id string) (entpay.ClientSessionDetail, error) {
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return entpay.ClientSessionDetail{}, err
	}
	return manager.SessionDetail(ctx, id)
}

func (a *App) ApproveEntPaySession(id string, expectedRevision uint64) (entpay.ClientSession, error) {
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return entpay.ClientSession{}, err
	}
	session, err := manager.Approve(ctx, id, expectedRevision)
	if err != nil {
		return session, err
	}
	manager.Emit(session)
	if session.Stage == entpay.StageBroadcast {
		manager.StartContinuation(session.ID)
	}
	return session, nil
}

func (a *App) RejectEntPaySession(id string, expectedRevision uint64) (entpay.ClientSession, error) {
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return entpay.ClientSession{}, err
	}
	session, err := manager.Reject(ctx, id, expectedRevision)
	if err == nil {
		manager.Emit(session)
	}
	return session, err
}

func (a *App) ReestablishEntPayMerchantTrust(id string, expectedRevision uint64) (entpay.ClientSession, error) {
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return entpay.ClientSession{}, err
	}
	session, err := manager.ReestablishMerchantTrust(ctx, id, expectedRevision)
	if err == nil {
		manager.Emit(session)
	}
	return session, err
}

func (a *App) RetryEntPaySession(id string, expectedRevision uint64) (ActionResult, error) {
	manager, _, err := a.requireEntPayClient()
	if err != nil {
		return ActionResult{}, err
	}
	manager.StartRetry(id, expectedRevision)
	return ActionResult{ID: id, Message: "Retry scheduled"}, nil
}

func (a *App) OpenEntPayLink(value string) (ActionResult, error) {
	launch, err := entpay.ParseLaunchURL(strings.TrimSpace(value))
	if err != nil || launch.Handoff == "" || entpay.ValidateLaunchRequest(launch) != nil {
		return ActionResult{}, fmt.Errorf("invalid EntPay payment link")
	}
	if !a.enqueueHandoff(launch) {
		return ActionResult{}, fmt.Errorf("Agent Pay request queue is full")
	}
	return ActionResult{Message: "Payment request queued"}, nil
}

func (a *App) GetEntPaySettings() (entpay.ClientSettings, error) {
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return entpay.ClientSettings{}, err
	}
	return manager.Settings(ctx)
}

func (a *App) SaveEntPaySettings(settings entpay.ClientSettings, expectedRevision uint64) (entpay.ClientSettings, error) {
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return entpay.ClientSettings{}, err
	}
	updated, err := manager.SaveSettings(ctx, settings, expectedRevision)
	if err == nil {
		wailsruntime.EventsEmit(ctx, "entcoin:entpay-settings", updated)
	}
	return updated, err
}

func (a *App) ChooseEntPayArtifactDirectory() (string, error) {
	_, ctx, err := a.requireEntPayClient()
	if err != nil {
		return "", err
	}
	return wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{Title: "Choose Agent Pay artifact folder"})
}

func (a *App) DeleteEntPaySession(id string) (ActionResult, error) {
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return ActionResult{}, err
	}
	if err := manager.DeleteSession(ctx, id); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{ID: id, Message: "Agent Pay history deleted; delivered files were not removed"}, nil
}

func (a *App) entpayClient() (*entpay.ClientManager, context.Context, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.entpayManager, a.ctx, a.entpayStart
}

func (a *App) requireEntPayClient() (*entpay.ClientManager, context.Context, error) {
	manager, ctx, startErr := a.entpayClient()
	if manager != nil && ctx != nil {
		return manager, ctx, nil
	}
	if startErr != nil {
		return nil, nil, fmt.Errorf("Agent Pay is unavailable: %w", startErr)
	}
	return nil, nil, fmt.Errorf("Agent Pay is starting")
}

func (a *App) emitEntPaySession(session entpay.ClientSession) {
	a.mu.RLock()
	ctx := a.ctx
	closing := a.closing
	a.mu.RUnlock()
	if ctx != nil && !closing {
		wailsruntime.EventsEmit(ctx, "entcoin:entpay-session", session)
	}
}
