package graph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetConsumptionHorizon(t *testing.T) {
	selfID := "user-self-123"
	otherID := "user-other-456"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/consumptionhorizons") {
			resp := map[string]any{
				"consumptionHorizons": []map[string]any{
					{
						"id":                 "8:orgid:" + selfID,
						"consumptionHorizon": "2000;2000;2000",
					},
					{
						"id":                 "8:orgid:" + otherID,
						"consumptionHorizon": "5000;5000;5000",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		SelfID:     selfID,
		WebToken:   "test-token",
		HTTPClient: server.Client(),
	}

	origTransport := client.HTTPClient.Transport
	client.HTTPClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req)
	})
	defer func() {
		client.HTTPClient.Transport = origTransport
	}()

	res, err := client.GetConsumptionHorizon("conv-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.LastReadTs != 2000 {
		t.Errorf("expected LastReadTs 2000, got %d", res.LastReadTs)
	}
	if res.ChatVersion != 5000 {
		t.Errorf("expected ChatVersion 5000, got %d", res.ChatVersion)
	}
}

func TestParseArrivalTime(t *testing.T) {
	t.Run("RFC3339Nano with fractional seconds", func(t *testing.T) {
		raw := "2026-08-31T15:24:00.1234567Z"
		tm := parseArrivalTime(raw, "0")
		if tm.IsZero() || tm.Year() != 2026 {
			t.Errorf("failed to parse RFC3339Nano timestamp: %v", tm)
		}
	})

	t.Run("RFC3339 standard", func(t *testing.T) {
		raw := "2026-08-31T15:24:00Z"
		tm := parseArrivalTime(raw, "0")
		if tm.IsZero() || tm.Year() != 2026 {
			t.Errorf("failed to parse standard RFC3339 timestamp: %v", tm)
		}
	})

	t.Run("fallback to numeric ID timestamp", func(t *testing.T) {
		id := "1725128640000"
		tm := parseArrivalTime("", id)
		if tm.UnixMilli() != 1725128640000 {
			t.Errorf("expected %d, got %d", 1725128640000, tm.UnixMilli())
		}
	})
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
