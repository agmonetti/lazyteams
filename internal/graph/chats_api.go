package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Chat struct {
	ID                 string              `json:"id"`
	Topic              string              `json:"topic"`
	ChatType           string              `json:"chatType"`
	Members            []ChatMember        `json:"members"`
	LastMessagePreview *ChatMessagePreview `json:"lastMessagePreview,omitempty"`
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

func (c *Client) GetConsumptionHorizon(convID string) (int64, error) {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/threads/%s/consumptionhorizons", convID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("consumption horizon error %d", resp.StatusCode)
	}
	var res struct {
		ConsumptionHorizons []struct {
			ID                 string `json:"id"`
			ConsumptionHorizon string `json:"consumptionHorizon"`
		} `json:"consumptionHorizons"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return 0, err
	}
	selfMRI := "8:orgid:" + c.SelfID
	for _, h := range res.ConsumptionHorizons {
		if h.ID == selfMRI {
			parts := strings.SplitN(h.ConsumptionHorizon, ";", 2)
			if len(parts) > 0 {
				ts, err := strconv.ParseInt(parts[0], 10, 64)
				if err == nil {
					return ts, nil
				}
			}
		}
	}
	return 0, nil
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
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var res struct {
		Value []UserSearchResult `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("error parsing user search: %w", err)
	}
	return res.Value, nil
}

func (c *Client) CreateOneOnOneChat(selfID, targetID string) (Chat, error) {
	payload := fmt.Sprintf(`{
		"chatType": "oneOnOne",
		"members": [
			{
				"@odata.type": "#microsoft.graph.aadUserConversationMember",
				"roles": ["owner"],
				"user@odata.bind": "https://graph.microsoft.com/v1.0/users/%s"
			},
			{
				"@odata.type": "#microsoft.graph.aadUserConversationMember",
				"roles": ["owner"],
				"user@odata.bind": "https://graph.microsoft.com/v1.0/users/%s"
			}
		]
	}`, selfID, targetID)

	req, err := http.NewRequest("POST", baseURL+"/chats", strings.NewReader(payload))
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
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Chat{}, fmt.Errorf("create chat error %d", resp.StatusCode)
	}
	var chat Chat
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return Chat{}, err
	}
	return chat, nil
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
