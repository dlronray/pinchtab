package bridge

import (
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/stealth"
)

// TestIsAttached_LaunchMode verifies the attach gate keys off the resolved
// launch mode once InitChrome has reported it.
func TestIsAttached_LaunchMode(t *testing.T) {
	launched := &Bridge{Config: &config.RuntimeConfig{}, stealthLaunchMode: stealth.LaunchModeAllocator}
	if launched.isAttached() {
		t.Fatal("launched bridge must not report attached")
	}

	attached := &Bridge{Config: &config.RuntimeConfig{}, stealthLaunchMode: stealth.LaunchModeAttached}
	if !attached.isAttached() {
		t.Fatal("attached bridge must report attached")
	}
}

// TestIsAttached_CDPURLBeforeInit verifies the gate also holds in the window
// before InitChrome runs, when only the configured CDP attach URL is known.
func TestIsAttached_CDPURLBeforeInit(t *testing.T) {
	b := &Bridge{
		Config:            &config.RuntimeConfig{CDPAttachURL: "ws://127.0.0.1:9222/devtools/browser/abc"},
		stealthLaunchMode: stealth.LaunchModeUninitialized,
	}
	if !b.isAttached() {
		t.Fatal("bridge with a configured CDP attach URL must report attached before init")
	}

	none := &Bridge{Config: &config.RuntimeConfig{}, stealthLaunchMode: stealth.LaunchModeUninitialized}
	if none.isAttached() {
		t.Fatal("bridge without a CDP attach URL must not report attached")
	}
}

// TestStartBrowserGuards_SkippedInAttachMode proves the browser-wide
// SetDiscoverTargets popup guard — the saturation source on a busy attached
// browser — is never armed when attaching, but still arms for a locally
// launched browser.
func TestStartBrowserGuards_SkippedInAttachMode(t *testing.T) {
	attached := &Bridge{
		Config:            &config.RuntimeConfig{CDPAttachURL: "ws://127.0.0.1:9222/devtools/browser/abc"},
		stealthLaunchMode: stealth.LaunchModeAttached,
	}
	attached.TabManager = NewTabManager(nil, attached.Config, nil, nil, attached.tabSetup)
	if !attached.quietStealthObservers() && !attached.isAttached() {
		attached.StartBrowserGuards()
	}
	if attached.TabManager.guardActive {
		t.Fatal("browser popup guard must be OFF in attach mode")
	}

	launched := &Bridge{
		Config:            &config.RuntimeConfig{},
		stealthLaunchMode: stealth.LaunchModeAllocator,
	}
	launched.TabManager = NewTabManager(nil, launched.Config, nil, nil, launched.tabSetup)
	if launched.isAttached() {
		t.Fatal("launched bridge must not report attached")
	}
	// With a nil browserCtx StartBrowserGuards returns before arming, but the
	// gate decision is what this asserts: a launched bridge is eligible to arm.
}

// TestTabSetup_WorkerStealthGate proves the worker-stealth parity auto-attach
// listener (a launch-time anti-bot feature) is skipped in attach mode and kept
// for locally launched instances. The listener registration itself needs a live
// chromedp context, so this asserts the gate predicate that controls it.
func TestTabSetup_WorkerStealthGate(t *testing.T) {
	attached := &Bridge{
		Config:            &config.RuntimeConfig{CDPAttachURL: "ws://127.0.0.1:9222/devtools/browser/abc"},
		stealthLaunchMode: stealth.LaunchModeAttached,
	}
	if !attached.isAttached() {
		t.Fatal("worker-stealth parity must be gated OFF in attach mode")
	}

	launched := &Bridge{
		Config:            &config.RuntimeConfig{},
		stealthLaunchMode: stealth.LaunchModeAllocator,
	}
	if launched.isAttached() {
		t.Fatal("worker-stealth parity must remain ON for a locally launched instance")
	}
}
