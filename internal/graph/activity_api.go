package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type NotificationItem struct {
	ID           string
	ActivityType string
	Subtype      string
	SenderName   string
	Preview      string
	Timestamp    time.Time
	IsRead       bool
	SourceThread string
	SequenceID   string
}

type activityResponse struct {
	Messages []struct {
		ID          string `json:"id"`
		SequenceID  int64  `json:"sequenceId"`
		Composetime string `json:"composetime"`
		Properties  struct {
			Activity *struct {
				ActivityType            string `json:"activityType"`
				ActivitySubtype         string `json:"activitySubtype"`
				SourceUserImDisplayName string `json:"sourceUserImDisplayName"`
				MessagePreview          string `json:"messagePreview"`
				SourceThreadId          string `json:"sourceThreadId"`
			} `json:"activity"`
			IsRead json.RawMessage `json:"isread"`
		} `json:"properties"`
	} `json:"messages"`
	Metadata struct {
		BackwardLink string `json:"backwardLink"`
	} `json:"_metadata"`
}

const (
	notifConvURL  = "https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/48:notifications/messages?startTime=0&pageSize=20&view=msnp24Equivalent&targetType=Passport"
	notifMaxPages = 5
)

func (c *Client) FetchNotifications() ([]NotificationItem, error) {
	if c.NotifToken == "" {
		return nil, fmt.Errorf("TEAMS_NOTIF_TOKEN not configured")
	}

	items := make([]NotificationItem, 0, 20)
	seen := make(map[string]struct{})
	urlStr := notifConvURL

	for page := 0; page < notifMaxPages && urlStr != ""; page++ {
		if err := validateMicrosoftURL(urlStr); err != nil {
			return nil, err
		}

		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.NotifToken)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Ms-Client-Type", "web")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("network error: %w", err)
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("error reading response: %w", readErr)
		}
		if resp.StatusCode == 401 {
			return nil, fmt.Errorf("TEAMS_NOTIF_TOKEN expired (401)")
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("chatsvc error %d", resp.StatusCode)
		}

		var raw activityResponse
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("error parsing JSON: %w", err)
		}

		for _, msg := range raw.Messages {
			if msg.Properties.Activity == nil {
				continue
			}
			if _, ok := seen[msg.ID]; ok {
				continue
			}
			seen[msg.ID] = struct{}{}

			act := msg.Properties.Activity

			// Assignments go to Workspace 4 (Education Assignments), not 3
			if act.ActivitySubtype == "assignmentPublishedNotification" ||
				act.ActivitySubtype == "assignmentDueDateNotification" {
				continue
			}

			preview := strings.TrimSpace(cleanHTML(act.MessagePreview))

			// Parse isread (can be bool or string)
			isRead := false
			if raw := msg.Properties.IsRead; len(raw) > 0 {
				var b bool
				if err := json.Unmarshal(raw, &b); err == nil {
					isRead = b
				} else {
					var s string
					if err := json.Unmarshal(raw, &s); err == nil {
						isRead = strings.EqualFold(s, "true")
					}
				}
			}

			items = append(items, NotificationItem{
				ID:           msg.ID,
				SequenceID:   fmt.Sprintf("%d", msg.SequenceID),
				ActivityType: act.ActivityType,
				Subtype:      act.ActivitySubtype,
				SenderName:   act.SourceUserImDisplayName,
				Preview:      preview,
				Timestamp:    parseArrivalTime(msg.Composetime, msg.ID),
				SourceThread: act.SourceThreadId,
				IsRead:       isRead,
			})
		}

		nextURL := raw.Metadata.BackwardLink
		if nextURL == urlStr {
			break
		}
		urlStr = nextURL
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Timestamp.After(items[j].Timestamp)
	})
	return items, nil
}

func (c *Client) MarkNotificationRead(msgID string) error {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/48:notifications/messages/%s/properties?name=isread", msgID)
	body := strings.NewReader(`{"isread":"true"}`)
	req, err := http.NewRequest("PUT", url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.NotifToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ms-Client-Type", "web")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("mark read error %d", resp.StatusCode)
	}
	return nil
}

func ActivityTypeLabel(subtype string) string {
	switch subtype {
	case "assignmentPublishedNotification":
		return "[ASSIGN] "
	case "assignmentDueDateNotification":
		return "[DUE]    "
	case "mention":
		return "[@]      "
	case "like":
		return "[👍]     "
	case "reply":
		return "[REPLY]  "
	case "channelMessage":
		return "[MSG]    "
	default:
		return "[NOTIF]  "
	}
}
