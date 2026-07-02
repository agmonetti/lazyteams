package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
		ID           string `json:"id"`
		SequenceID   int64  `json:"sequenceId"`
		Composetime  string `json:"composetime"`
		Properties   struct {
			Activity *struct {
				ActivityType    string `json:"activityType"`
				ActivitySubtype string `json:"activitySubtype"`
				SourceUserImDisplayName string `json:"sourceUserImDisplayName"`
				MessagePreview  string `json:"messagePreview"`
				SourceThreadId  string `json:"sourceThreadId"`
			} `json:"activity"`
		} `json:"properties"`
	} `json:"messages"`
}

const notifConvURL = "https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/48:notifications/messages?startTime=0&pageSize=20&view=msnp24Equivalent&targetType=Passport"

func (c *Client) FetchNotifications() ([]NotificationItem, error) {
	if c.NotifToken == "" {
		return nil, fmt.Errorf("TEAMS_NOTIF_TOKEN not configured")
	}

	req, err := http.NewRequest("GET", notifConvURL, nil)
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
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("TEAMS_NOTIF_TOKEN expired (401)")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("chatsvc error %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	var raw activityResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	items := make([]NotificationItem, 0, len(raw.Messages))
	for _, msg := range raw.Messages {
		if msg.Properties.Activity == nil {
			continue
		}
		act := msg.Properties.Activity

		// Assignments go to Workspace 4 (Education Assignments), not 3
		if act.ActivitySubtype == "assignmentPublishedNotification" ||
			act.ActivitySubtype == "assignmentDueDateNotification" {
			continue
		}

		ts, _ := time.Parse("2006-01-02T15:04:05.0000000Z", msg.Composetime)

		preview := cleanHTML(act.MessagePreview)
		preview = strings.TrimSpace(preview)

		items = append(items, NotificationItem{
			ID:           msg.ID,
			SequenceID:   fmt.Sprintf("%d", msg.SequenceID),
			ActivityType: act.ActivityType,
			Subtype:      act.ActivitySubtype,
			SenderName:   act.SourceUserImDisplayName,
			Preview:      preview,
			Timestamp:    ts,
			SourceThread: act.SourceThreadId,
		})
	}

	return items, nil
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
