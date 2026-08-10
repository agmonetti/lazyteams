package version

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.2.4", "v1.2.3", 1},
		{"v1.10.0", "v1.9.0", 1}, // numeric, not lexicographic
		{"v2.0.0", "v1.9.9", 1},
		{"dev", "dev", 0},
		{"dev", "v0.0.1", -1}, // a source build sees any release as newer
		{"v2.0.0", "dev", 1},
		{"v1.0.0-beta", "v1.0.0", 0}, // suffixes are ignored
		{"v1.0.0", "v1.0.0-beta", 0},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestLatestRelease(t *testing.T) {
	t.Run("returns tag_name", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") == "" {
				t.Error("request did not include a User-Agent header")
			}
			fmt.Fprintln(w, `{"tag_name": "v1.2.3"}`)
		}))
		defer ts.Close()

		tag, err := LatestRelease(ts.URL)
		if err != nil {
			t.Fatalf("LatestRelease() error: %v", err)
		}
		if tag != "v1.2.3" {
			t.Errorf("LatestRelease() = %q, want %q", tag, "v1.2.3")
		}
	})

	t.Run("returns error on non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		if _, err := LatestRelease(ts.URL); err == nil {
			t.Error("LatestRelease() expected error on non-200 response")
		}
	})

	t.Run("returns error on invalid json", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "not json")
		}))
		defer ts.Close()

		if _, err := LatestRelease(ts.URL); err == nil {
			t.Error("LatestRelease() expected error on invalid JSON")
		}
	})
}
