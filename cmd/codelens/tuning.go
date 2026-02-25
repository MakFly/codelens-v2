package main

import (
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/mem"
)

const (
	profileAuto     = "auto"
	profileLow      = "low"
	profileBalanced = "balanced"
	profileHigh     = "high"
)

type RuntimeTuning struct {
	Profile          string
	RequestedProfile string
	InvalidProfile   bool
	CPUs             int
	MemoryGiB        int
	NumThreads       int
	MaxConcurrent    int
	HardCap          int
	RequestedThreads int
	ThreadsClamped   bool
}

func ResolveRuntimeTuning(profile string, maxCPUThreads int) RuntimeTuning {
	cpus := runtime.NumCPU()
	if cpus < 1 {
		cpus = 1
	}
	memoryGiB := detectTotalMemoryGiB()

	resolved, invalidProfile := normalizeProfile(profile)
	if resolved == profileAuto {
		resolved = autoProfile(cpus, memoryGiB)
	}

	hardCap := cpus - 1
	if hardCap < 1 {
		hardCap = 1
	}
	if hardCap > 12 {
		hardCap = 12
	}

	numThreads := 4
	maxConcurrent := 2
	switch resolved {
	case profileLow:
		numThreads = 2
		maxConcurrent = 1
	case profileHigh:
		// Keep "high" performant but bounded to avoid workstation overload.
		numThreads = 8
		maxConcurrent = 3
	default:
		resolved = profileBalanced
		numThreads = 4
		maxConcurrent = 2
	}

	if numThreads > hardCap {
		numThreads = hardCap
	}
	requestedThreads := maxCPUThreads
	if maxCPUThreads > 0 {
		numThreads = maxCPUThreads
	}
	if numThreads < 1 {
		numThreads = 1
	}
	if numThreads > hardCap {
		numThreads = hardCap
	}
	threadsClamped := requestedThreads > 0 && requestedThreads != numThreads

	if maxConcurrent > 4 {
		maxConcurrent = 4
	}
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	return RuntimeTuning{
		Profile:          resolved,
		RequestedProfile: strings.TrimSpace(profile),
		InvalidProfile:   invalidProfile,
		CPUs:             cpus,
		MemoryGiB:        memoryGiB,
		NumThreads:       numThreads,
		MaxConcurrent:    maxConcurrent,
		HardCap:          hardCap,
		RequestedThreads: requestedThreads,
		ThreadsClamped:   threadsClamped,
	}
}

func normalizeProfile(profile string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", profileAuto:
		return profileAuto, false
	case profileLow:
		return profileLow, false
	case profileBalanced:
		return profileBalanced, false
	case profileHigh:
		return profileHigh, false
	default:
		return profileAuto, true
	}
}

func autoProfile(cpus, memoryGiB int) string {
	if cpus <= 6 {
		return profileLow
	}
	if memoryGiB > 0 && memoryGiB <= 8 {
		return profileLow
	}
	if cpus >= 12 && (memoryGiB == 0 || memoryGiB >= 24) {
		return profileHigh
	}
	return profileBalanced
}

func detectTotalMemoryGiB() int {
	vm, err := mem.VirtualMemory()
	if err != nil || vm == nil || vm.Total == 0 {
		return 0
	}
	gib := int(vm.Total / 1024 / 1024 / 1024)
	if gib < 1 {
		gib = 1
	}
	return gib
}
