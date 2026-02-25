package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbed_UsesV2EndpointFirst(t *testing.T) {
	t.Parallel()

	v2Calls := 0
	legacyCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			v2Calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"embeddings":[[1,2,3]]}`))
		case "/api/embeddings":
			legacyCalls++
			http.Error(w, "legacy should not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewOllama(srv.URL, "nomic-embed-text")
	vec, err := client.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() err = %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3 dims, got %d", len(vec))
	}
	if v2Calls != 1 {
		t.Fatalf("expected v2 to be called once, got %d", v2Calls)
	}
	if legacyCalls != 0 {
		t.Fatalf("expected legacy not called, got %d", legacyCalls)
	}
}

func TestEmbed_FallbacksToLegacyWhenV2Unavailable(t *testing.T) {
	t.Parallel()

	v2Calls := 0
	legacyCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			v2Calls++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		case "/api/embeddings":
			legacyCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"embedding":[0.1,0.2]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewOllama(srv.URL, "nomic-embed-text")
	vec, err := client.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() err = %v", err)
	}
	if len(vec) != 2 {
		t.Fatalf("expected 2 dims from legacy response, got %d", len(vec))
	}
	if v2Calls != 1 || legacyCalls != 1 {
		t.Fatalf("expected v2=1 and legacy=1, got v2=%d legacy=%d", v2Calls, legacyCalls)
	}
}

func TestEmbed_SendsNumThreadOption(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		options, ok := body["options"].(map[string]any)
		if !ok {
			t.Fatalf("expected options object in request")
		}
		if got := int(options["num_thread"].(float64)); got != 3 {
			t.Fatalf("expected num_thread=3, got %d", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[1,1,1]]}`))
	}))
	defer srv.Close()

	client := NewOllama(srv.URL, "nomic-embed-text")
	client.SetNumThreads(3)
	if _, err := client.Embed(context.Background(), "hello"); err != nil {
		t.Fatalf("Embed() err = %v", err)
	}
}

func TestEmbed_ContextLengthErrorDoesNotFallback(t *testing.T) {
	t.Parallel()

	legacyCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"context length exceeded"}`))
		case "/api/embeddings":
			legacyCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"embedding":[1,2,3]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewOllama(srv.URL, "nomic-embed-text")
	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsContextLengthExceeded(err) {
		t.Fatalf("expected context length error, got: %v", err)
	}
	if legacyCalls != 0 {
		t.Fatalf("legacy endpoint should not be called on context length errors")
	}
}
