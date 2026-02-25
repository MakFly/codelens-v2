package main

import (
	"runtime"
	"testing"
)

func TestResolveRuntimeTuning_InvalidProfileFallsBackToAuto(t *testing.T) {
	t.Parallel()

	tuning := ResolveRuntimeTuning("ultra", 0)
	if !tuning.InvalidProfile {
		t.Fatalf("expected InvalidProfile=true")
	}
	if tuning.Profile == "ultra" || tuning.Profile == "" {
		t.Fatalf("expected normalized profile, got %q", tuning.Profile)
	}
}

func TestResolveRuntimeTuning_ClampsUserRequestedThreads(t *testing.T) {
	t.Parallel()

	tuning := ResolveRuntimeTuning("high", 999)
	hardCap := runtime.NumCPU() - 1
	if hardCap < 1 {
		hardCap = 1
	}
	if hardCap > 12 {
		hardCap = 12
	}

	if tuning.NumThreads != hardCap {
		t.Fatalf("expected NumThreads=%d, got %d", hardCap, tuning.NumThreads)
	}
	if !tuning.ThreadsClamped {
		t.Fatalf("expected ThreadsClamped=true")
	}
}
