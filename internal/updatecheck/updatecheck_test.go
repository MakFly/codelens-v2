package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want [3]int
	}{
		{in: "v1.2.3", ok: true, want: [3]int{1, 2, 3}},
		{in: "1.2", ok: true, want: [3]int{1, 2, 0}},
		{in: "v1.2.3-beta.1", ok: true, want: [3]int{1, 2, 3}},
		{in: "dev", ok: false},
		{in: "1", ok: false},
	}

	for _, tc := range cases {
		got, ok := parseSemver(tc.in)
		if ok != tc.ok {
			t.Fatalf("parseSemver(%q) ok=%v want=%v", tc.in, ok, tc.ok)
		}
		if ok && got != tc.want {
			t.Fatalf("parseSemver(%q)=%v want=%v", tc.in, got, tc.want)
		}
	}
}

func TestCheckUsesCacheWhenFresh(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.7"}`))
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 2, 25, 14, 0, 0, 0, time.UTC)
	checker := &Checker{
		Repo:       "MakFly/codelens-v2",
		CachePath:  cachePath,
		TTL:        12 * time.Hour,
		APIBaseURL: srv.URL,
		HTTPClient: srv.Client(),
		Now:        func() time.Time { return now },
	}

	res, err := checker.Check(context.Background(), "0.2.6")
	if err != nil {
		t.Fatalf("first check error: %v", err)
	}
	if !res.NeedsUpdate || res.LatestTag != "v0.2.7" {
		t.Fatalf("unexpected first result: %+v", res)
	}

	// Server can fail now; cache should be used for second call.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	checker.Now = func() time.Time { return now.Add(2 * time.Hour) }
	res, err = checker.Check(context.Background(), "0.2.6")
	if err != nil {
		t.Fatalf("second check should use cache, got error: %v", err)
	}
	if !res.NeedsUpdate {
		t.Fatalf("expected update available on second check: %+v", res)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one API hit, got %d", hits.Load())
	}
}

func TestCheckSkipsDevVersion(t *testing.T) {
	checker := &Checker{
		Disabled: false,
	}
	res, err := checker.Check(context.Background(), "dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NeedsUpdate {
		t.Fatalf("dev build should not report update: %+v", res)
	}
}
