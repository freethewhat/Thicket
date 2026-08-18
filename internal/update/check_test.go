package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"equal versions", "v1.2.3", "v1.2.3", false},
		{"older major", "v2.0.0", "v1.9.9", false},
		{"newer major", "v1.9.9", "v2.0.0", true},
		{"newer minor", "v1.2.3", "v1.3.0", true},
		{"newer patch", "v1.2.3", "v1.2.4", true},
		{"older patch", "v1.2.4", "v1.2.3", false},
		{"malformed latest", "v1.2.3", "not-a-version", false},
		{"malformed current", "not-a-version", "v1.2.3", false},
		{"dev build never newer", "dev", "v99.0.0", false},
		{"missing v prefix still parses", "1.2.3", "1.2.4", true},
		{"beta older than later beta, same core", "v1.0.0-beta.1", "v1.0.0-beta.2", true},
		{"beta not newer than earlier beta, same core", "v1.0.0-beta.2", "v1.0.0-beta.1", false},
		{"equal betas", "v1.0.0-beta.1", "v1.0.0-beta.1", false},
		{"stable promotion from beta of same core is newer", "v1.0.0-beta.1", "v1.0.0", true},
		{"beta of same core as an already-stable current is not newer", "v1.0.0", "v1.0.0-beta.1", false},
		{"beta of a newer core beats stable of an older core", "v1.0.0", "v1.1.0-beta.1", true},
		{"numeric beta ordinal compares numerically, not lexically", "v1.0.0-beta.9", "v1.0.0-beta.10", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNewer(tc.current, tc.latest)
			if got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

func TestComparePrerelease_NumericFieldsCompareNumerically(t *testing.T) {
	if got := comparePrerelease("beta.9", "beta.10"); got >= 0 {
		t.Errorf("comparePrerelease(beta.9, beta.10) = %d, want < 0", got)
	}
	if got := comparePrerelease("beta.10", "beta.9"); got <= 0 {
		t.Errorf("comparePrerelease(beta.10, beta.9) = %d, want > 0", got)
	}
	if got := comparePrerelease("beta.1", "beta.1"); got != 0 {
		t.Errorf("comparePrerelease(beta.1, beta.1) = %d, want 0", got)
	}
}

func TestFetchTag_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.2.3"})
	}))
	defer srv.Close()

	tag, err := fetchTag(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchTag: %v", err)
	}
	if tag != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3", tag)
	}
}

func TestFetchTag_404IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := fetchTag(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchTag: want error on 404, got nil")
	}
}

func TestFetchTag_RateLimitBodyIsError(t *testing.T) {
	// GitHub's rate-limit response: 403 with a body that has no tag_name
	// field at all. Must be rejected at the status check, not decoded
	// into an empty-string tag that would look like "no update".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "API rate limit exceeded"})
	}))
	defer srv.Close()

	tag, err := fetchTag(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("fetchTag: want error on 403, got tag %q", tag)
	}
}

func TestFetchTag_MalformedJSONIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := fetchTag(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchTag: want error on malformed JSON, got nil")
	}
}

func TestFetchTag_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.2.3"})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := fetchTag(ctx, srv.URL)
	if err == nil {
		t.Fatal("fetchTag: want error on context timeout, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && ctx.Err() == nil {
		t.Fatalf("fetchTag: want a context-deadline error, got %v", err)
	}
}
