package graph

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type activityPageTransport struct {
	pages    map[string]string
	requests []*http.Request
}

func (t *activityPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req)
	page := req.URL.Query().Get("page")
	body, ok := t.pages[page]
	if !ok {
		body = t.pages["first"]
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestFetchNotificationsFollowsPagesAndKeepsReadActivity(t *testing.T) {
	transport := &activityPageTransport{pages: map[string]string{
		"first": `{
			"messages": [{
				"id": "new",
				"sequenceId": 20,
				"composetime": "2026-09-15T10:00:00Z",
				"properties": {
					"activity": {
						"activityType": "like",
						"activitySubtype": "like",
						"sourceUserImDisplayName": "Ada",
						"messagePreview": "<p>New reaction</p>",
						"sourceThreadId": "thread-new"
					},
					"isread": "true"
				}
			}],
			"_metadata": {
				"backwardLink": "https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/48:notifications/messages?page=2"
			}
		}`,
		"2": `{
			"messages": [
				{
					"id": "new",
					"sequenceId": 20,
					"composetime": "2026-09-15T10:00:00Z",
					"properties": {"activity": {"activitySubtype": "like"}, "isread": true}
				},
				{
					"id": "old",
					"sequenceId": 10,
					"composetime": "2026-09-15T09:00:00Z",
					"properties": {
						"activity": {
							"activityType": "mention",
							"activitySubtype": "mention",
							"sourceUserImDisplayName": "Bea",
							"messagePreview": "<p>Older mention</p>"
						},
						"isread": false
					}
				}
			]
		}`,
	}}
	client := &Client{
		NotifToken: "notif-token",
		HTTPClient: &http.Client{Transport: transport},
	}

	items, err := client.FetchNotifications()
	if err != nil {
		t.Fatalf("FetchNotifications() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("FetchNotifications() returned %d items, want 2", len(items))
	}
	if items[0].ID != "new" || items[1].ID != "old" {
		t.Fatalf("notifications order = [%s, %s], want [new, old]", items[0].ID, items[1].ID)
	}
	if !items[0].IsRead {
		t.Error("read Activity notification was not preserved")
	}
	if items[0].Preview != "New reaction" {
		t.Errorf("preview = %q, want %q", items[0].Preview, "New reaction")
	}
	if len(transport.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(transport.requests))
	}
	for _, req := range transport.requests {
		if req.Header.Get("Authorization") != "Bearer notif-token" {
			t.Error("notification request did not carry the notification token")
		}
	}
}

func TestFetchNotificationsRejectsUntrustedBackwardLink(t *testing.T) {
	transport := &activityPageTransport{pages: map[string]string{
		"first": `{"messages": [], "_metadata": {"backwardLink": "https://evil.example/notifications"}}`,
	}}
	client := &Client{
		NotifToken: "notif-token",
		HTTPClient: &http.Client{Transport: transport},
	}

	if _, err := client.FetchNotifications(); err == nil {
		t.Fatal("FetchNotifications() accepted an untrusted backward link")
	}
	if len(transport.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(transport.requests))
	}
}
