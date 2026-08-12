package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Chat struct {
	ID                  string              `json:"id"`
	Topic               string              `json:"topic"`
	ChatType            string              `json:"chatType"`
	LastUpdatedDateTime string              `json:"lastUpdatedDateTime"`
	Members             []ChatMember        `json:"members"`
	LastMessagePreview  *ChatMessagePreview `json:"lastMessagePreview,omitempty"`
}

type ChatMessagePreview struct {
	ID   string `json:"id"`
	From *struct {
		User *struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	} `json:"from"`
}

type ChatMember struct {
	DisplayName string `json:"displayName"`
	UserID      string `json:"userId"`
}

// DisplayName resolves the display name of a chat. Group chats
// typically have a "topic"; 1:1 chats don't, so we use the other
// participant's name (excluding the logged-in user, identified by selfID).
func (ch Chat) DisplayName(selfID string) string {
	if strings.TrimSpace(ch.Topic) != "" {
		return ch.Topic
	}

	// Exact and reliable detection of the "Personal notes (You)" chat.
	// In Teams, this chat always has an ID formed by the user's ID repeated.
	expectedSelfChatID := fmt.Sprintf("19:%s_%s@unq.gbl.spaces", selfID, selfID)
	if ch.ID == expectedSelfChatID {
		return "Personal notes (You)"
	}

	var names []string
	for _, m := range ch.Members {
		if m.UserID != selfID && strings.TrimSpace(m.DisplayName) != "" {
			names = append(names, m.DisplayName)
		}
	}
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}

	// Fallback for legacy or orphaned chats where Graph API omits
	// the other member from the response. We try to extract the name
	// of the person who sent the last message (if not us).
	if ch.LastMessagePreview != nil && ch.LastMessagePreview.From != nil && ch.LastMessagePreview.From.User != nil {
		u := ch.LastMessagePreview.From.User
		if u.ID != selfID && strings.TrimSpace(u.DisplayName) != "" {
			return strings.TrimSpace(u.DisplayName) + " (Legacy)"
		}
	}

	if ch.ChatType == "oneOnOne" {
		return "1:1 Chat (Legacy)"
	}
	if ch.ChatType != "" {
		return fmt.Sprintf("Unnamed chat (%s)", ch.ChatType)
	}
	return "Unnamed chat"
}

// GetMe fetches the logged-in user's ID, needed to resolve
// the other participant's name in 1:1 chats.
func (c *Client) GetMe() (string, error) {
	body, err := c.doReq("/me")
	if err != nil {
		return "", err
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		return "", fmt.Errorf("error parsing /me: %w", err)
	}
	return me.ID, nil
}

// GetChats lists the user's personal chats (1:1 and group).
func (c *Client) GetChats() ([]Chat, error) {
	body, err := c.doReq("/me/chats?$expand=members&$top=50")
	if err != nil {
		return nil, err
	}
	var res struct {
		Value []Chat `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parsing chats: %w", err)
	}
	return res.Value, nil
}

type ConsumptionHorizonResult struct {
	LastReadTs  int64
	ChatVersion int64
}

func (c *Client) GetConsumptionHorizon(convID string) (ConsumptionHorizonResult, error) {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/threads/%s/consumptionhorizons", convID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ConsumptionHorizonResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return ConsumptionHorizonResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return ConsumptionHorizonResult{}, fmt.Errorf("consumption horizon error %d", resp.StatusCode)
	}
	var res struct {
		ConsumptionHorizons []struct {
			ID                 string `json:"id"`
			ConsumptionHorizon string `json:"consumptionHorizon"`
		} `json:"consumptionHorizons"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return ConsumptionHorizonResult{}, err
	}

	selfMRI := "8:orgid:" + c.SelfID
	var myLastRead int64
	var otherLastRead int64

	for _, h := range res.ConsumptionHorizons {
		parts := strings.SplitN(h.ConsumptionHorizon, ";", 2)
		if len(parts) == 0 {
			continue
		}
		ts, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		if h.ID == selfMRI {
			myLastRead = ts
		} else {
			if ts > otherLastRead {
				otherLastRead = ts
			}
		}
	}

	return ConsumptionHorizonResult{
		LastReadTs:  myLastRead,
		ChatVersion: otherLastRead,
	}, nil
}

type UserSearchResult struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Mail        string `json:"mail"`
}

func (c *Client) SearchUsers(query string) ([]UserSearchResult, error) {
	encoded := url.QueryEscape(fmt.Sprintf(`"displayName:%s"`, query))
	endpoint := fmt.Sprintf("/users?$search=%s&$select=id,displayName,mail&$top=10", encoded)
	req, err := http.NewRequest("GET", baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("ConsistencyLevel", "eventual")
	resp, err := c.HTTPClient.Do(req)

	var results []UserSearchResult
	if err == nil {
		defer resp.Body.Close()
		var res struct {
			Value []UserSearchResult `json:"value"`
		}
		if json.NewDecoder(resp.Body).Decode(&res) == nil {
			results = res.Value
		}
	}

	// Fallback/additive search for external users by exact email
	if strings.Contains(query, "@") {
		extURL := fmt.Sprintf("https://teams.microsoft.com/api/mt/part/amer-02/beta/users/%s/externalsearchv3?includeTFLUsers=true", url.PathEscape(query))

		tokens := []string{c.WebToken, c.SpacesToken}
		for _, token := range tokens {
			if token == "" {
				continue
			}
			extReq, err := http.NewRequest("GET", extURL, nil)
			if err != nil {
				continue
			}
			extReq.Header.Set("Authorization", "Bearer "+token)
			extReq.Header.Set("Accept", "application/json")
			extReq.Header.Set("x-ms-client-caller", "newChat")
			extReq.Header.Set("x-ms-client-type", "cdlworker")
			extReq.Header.Set("x-ms-migration", "True")

			extResp, err := c.HTTPClient.Do(extReq)
			if err != nil {
				continue
			}

			if extResp.StatusCode == 200 {
				var extRes []struct {
					UserPrincipalName string `json:"userPrincipalName"`
					DisplayName       string `json:"displayName"`
					MRI               string `json:"mri"`
					ObjectID          string `json:"objectId"`
				}
				err := json.NewDecoder(extResp.Body).Decode(&extRes)
				extResp.Body.Close()
				if err == nil {
					for _, ex := range extRes {
						exists := false
						for _, r := range results {
							if strings.EqualFold(r.Mail, ex.UserPrincipalName) {
								exists = true
								break
							}
						}
						if !exists {
							results = append(results, UserSearchResult{
								ID:          "ext:" + ex.ObjectID,
								DisplayName: ex.DisplayName + " (External)",
								Mail:        ex.UserPrincipalName,
							})
						}
					}
				}
				break
			}
			extResp.Body.Close()
			// status != 200: try the next token
		}
	}

	return results, nil
}

func (c *Client) CreateOneOnOneChat(selfID, targetID string) (Chat, error) {
	if strings.HasPrefix(targetID, "ext:") {
		targetUUID := strings.TrimPrefix(targetID, "ext:")
		u1, u2 := selfID, targetUUID
		if u1 > u2 {
			u1, u2 = u2, u1
		}
		chatID := fmt.Sprintf("19:%s_%s@unq.gbl.spaces", u1, u2)
		return Chat{
			ID:       chatID,
			ChatType: "oneOnOne",
		}, nil
	}

	memberBind := func(userID string) map[string]any {
		return map[string]any{
			"@odata.type":     "#microsoft.graph.aadUserConversationMember",
			"roles":           []string{"owner"},
			"user@odata.bind": "https://graph.microsoft.com/v1.0/users/" + userID,
		}
	}
	payload := struct {
		ChatType string           `json:"chatType"`
		Members  []map[string]any `json:"members"`
	}{
		ChatType: "oneOnOne",
		Members:  []map[string]any{memberBind(selfID), memberBind(targetID)},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return Chat{}, err
	}

	req, err := http.NewRequest("POST", baseURL+"/chats", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return Chat{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Chat{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		// Graph API blocked by tenant — fallback to ChatSvc
		return c.CreateOneOnOneChatViaChatSvc(selfID, targetID)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Chat{}, fmt.Errorf("create chat error %d", resp.StatusCode)
	}
	var chat Chat
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return Chat{}, err
	}
	return chat, nil
}

// CreateOneOnOneChatViaChatSvc creates a 1:1 chat using the Teams ChatSvc API
// instead of Graph API. This bypasses tenant restrictions on POST /v1.0/chats.
func (c *Client) CreateOneOnOneChatViaChatSvc(selfID, targetID string) (Chat, error) {
	payload := struct {
		Members    []map[string]any `json:"members"`
		Properties map[string]any   `json:"properties"`
	}{
		Members: []map[string]any{
			{"id": "8:orgid:" + selfID, "role": "Admin"},
			{"id": "8:orgid:" + targetID, "role": "Admin"},
		},
		Properties: map[string]any{
			"threadType":         "chat",
			"fixedRoster":        true,
			"uniquerosterthread": true,
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return Chat{}, err
	}

	req, err := http.NewRequest("POST",
		"https://teams.microsoft.com/api/chatsvc/amer/v1/threads",
		bytes.NewBuffer(bodyBytes),
	)
	if err != nil {
		return Chat{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	req.Header.Set("x-ms-test-user", "False")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Chat{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return Chat{}, fmt.Errorf("chatsvc create chat error %d", resp.StatusCode)
	}

	// Extract chat ID from Location header
	location := resp.Header.Get("Location")
	// Location: https://amer.ng.msg.teams.microsoft.com/v1/threads/19:xxx@unq.gbl.spaces
	parts := strings.Split(location, "/threads/")
	if len(parts) < 2 || parts[1] == "" {
		return Chat{}, fmt.Errorf("could not extract chat ID from location: %s", location)
	}
	chatID := parts[1]

	return Chat{
		ID:       chatID,
		ChatType: "oneOnOne",
		Members: []ChatMember{
			{UserID: selfID},
			{UserID: targetID, DisplayName: targetID},
		},
	}, nil
}

// DiscoverSelfChatID brute-forces ChatSvc to find the real
// identifier (MRI) of the user's "Personal notes" chat.
func (c *Client) DiscoverSelfChatID(selfID string) string {
	candidates := []string{
		fmt.Sprintf("8:orgid:%s", selfID),
		"48:notes",
		fmt.Sprintf("8:%s", selfID),
		fmt.Sprintf("19:%s_%s@unq.gbl.spaces", selfID, selfID),
	}

	for _, id := range candidates {
		endpoint := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages?pageSize=1", id)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}

		req.Header.Set("Authorization", "Bearer "+c.WebToken)
		req.Header.Set("behavioroverride", "redirectAs404")

		resp, err := c.HTTPClient.Do(req)
		if err == nil {
			resp.Body.Close()
			// If the server responds 200 OK, we found the correct ID.
			if resp.StatusCode == http.StatusOK {
				return id
			}
		}
	}
	return ""
}
