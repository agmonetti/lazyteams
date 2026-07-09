package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Team struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	IsArchived  bool   `json:"isArchived"`
	WebUrl      string `json:"webUrl"`
	Summary     struct {
		OwnersCount  int `json:"ownersCount"`
		MembersCount int `json:"membersCount"`
		GuestsCount  int `json:"guestsCount"`
	} `json:"summary"`
	MemberSettings struct {
		AllowCreateUpdateChannels bool `json:"allowCreateUpdateChannels"`
		AllowDeleteChannels       bool `json:"allowDeleteChannels"`
		AllowAddRemoveApps        bool `json:"allowAddRemoveApps"`
	} `json:"memberSettings"`
}

type Channel struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	MembershipType  string `json:"membershipType"`
	Email           string `json:"email"`
	CreatedDateTime string `json:"createdDateTime"`
}

type TeamMember struct {
	ID          string
	DisplayName string
	Mail        string
	Role        string // "Owner" o "Member"
}

// GetJoinedTeams fetches the teams the user is a member of.
func (c *Client) GetJoinedTeams() ([]Team, error) {
	body, err := c.doReq("/me/joinedTeams")
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []Team `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parsing teams: %w", err)
	}

	return res.Value, nil
}

// GetChannels fetches the channels of a specific team.
func (c *Client) GetChannels(teamID string) ([]Channel, error) {
	endpoint := fmt.Sprintf("/teams/%s/channels", teamID)

	var allChannels []Channel

	for endpoint != "" {
		body, err := c.doReq(endpoint)
		if err != nil {
			return nil, err
		}

		var res struct {
			Value    []Channel `json:"value"`
			NextLink string    `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, fmt.Errorf("error parsing channels: %w", err)
		}

		allChannels = append(allChannels, res.Value...)

		if res.NextLink != "" {
			// NextLink is usually absolute; strip the baseURL if present
			if strings.HasPrefix(res.NextLink, baseURL) {
				endpoint = strings.TrimPrefix(res.NextLink, baseURL)
			} else {
				endpoint = res.NextLink
			}
		} else {
			endpoint = ""
		}
	}

	// Sort channels: "General" first, then alphabetically
	var general *Channel
	var others []Channel
	for _, ch := range allChannels {
		if strings.EqualFold(ch.DisplayName, "General") {
			chCopy := ch
			general = &chCopy
		} else {
			others = append(others, ch)
		}
	}

	// Simple insertion sort for others (or we can use sort package)
	for i := 0; i < len(others); i++ {
		for j := i + 1; j < len(others); j++ {
			if strings.ToLower(others[i].DisplayName) > strings.ToLower(others[j].DisplayName) {
				others[i], others[j] = others[j], others[i]
			}
		}
	}

	var sortedChannels []Channel
	if general != nil {
		sortedChannels = append(sortedChannels, *general)
	}
	sortedChannels = append(sortedChannels, others...)

	return sortedChannels, nil
}

func (c *Client) CreateTeam(displayName string) error {
	payload := fmt.Sprintf(`{
		"displayName": "%s",
		"mailNickname": "%s",
		"template@odata.bind": "https://graph.microsoft.com/v1.0/teamsTemplates('standard')"
	}`, displayName, sanitizeMailNickname(displayName))

	req, err := http.NewRequest("POST", baseURL+"/teams", strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 202 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("create team error %d: %s", resp.StatusCode, string(body))
}

func sanitizeMailNickname(name string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		}
	}
	s := result.String()
	if len(s) == 0 {
		return "team"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func (c *Client) CreateChannel(teamGUID, teamThreadID, name, channelType string) error {
	payload := fmt.Sprintf(`{
		"displayName": "%s",
		"description": "",
		"groupId": "%s",
		"channelType": "%s"
	}`, name, teamGUID, channelType)
	url := fmt.Sprintf("https://teams.microsoft.com/api/mt/part/amer-02/beta/teams/%s/channels", teamThreadID)
	req, err := http.NewRequest("POST", url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.SpacesToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 || resp.StatusCode == 201 || resp.StatusCode == 202 {
		return nil
	}
	return fmt.Errorf("create channel error %d: %s", resp.StatusCode, string(body))
}

func (c *Client) DeleteChannel(teamThreadID, channelThreadID string) error {
	url := fmt.Sprintf("https://teams.microsoft.com/api/mt/part/amer-02/beta/teams/%s/channels/%s", teamThreadID, channelThreadID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.SpacesToken)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("delete channel error %d: %s", resp.StatusCode, string(body))
}

func (c *Client) DeleteTeam(teamThreadID string) error {
	url := fmt.Sprintf("https://teams.microsoft.com/api/mt/part/amer-02/beta/teams/%s/delete", teamThreadID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.SpacesToken)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("delete team error %d: %s", resp.StatusCode, string(body))
}

func (c *Client) GetTeamInfo(teamID string) (*Team, error) {
	body, err := c.doReq(fmt.Sprintf("/teams/%s?$select=displayName,description,visibility,isArchived,webUrl,summary,memberSettings,createdDateTime", teamID))
	if err != nil {
		return nil, err
	}
	var team Team
	if err := json.Unmarshal(body, &team); err != nil {
		return nil, fmt.Errorf("error parsing team info: %w", err)
	}
	return &team, nil
}

func (c *Client) GetChannelInfo(teamID, channelID string) (*Channel, error) {
	body, err := c.doReq(fmt.Sprintf("/teams/%s/channels/%s?$select=displayName,description,membershipType,email,createdDateTime", teamID, channelID))
	if err != nil {
		return nil, fmt.Errorf("error parsing channel info: %w", err)
	}
	var ch Channel
	if err := json.Unmarshal(body, &ch); err != nil {
		return nil, fmt.Errorf("error parsing channel info: %w", err)
	}
	return &ch, nil
}

func (c *Client) GetTeamMembers(teamGUID string) ([]TeamMember, error) {
	// Fetch members
	body, err := c.doReq(fmt.Sprintf("/groups/%s/members?$select=id,displayName,mail", teamGUID))
	if err != nil {
		return nil, err
	}
	var membersRes struct {
		Value []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Mail        string `json:"mail"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &membersRes); err != nil {
		return nil, fmt.Errorf("error parsing members: %w", err)
	}

	// Fetch owners
	body, err = c.doReq(fmt.Sprintf("/groups/%s/owners?$select=id", teamGUID))
	if err != nil {
		return nil, err
	}
	var ownersRes struct {
		Value []struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &ownersRes); err != nil {
		return nil, fmt.Errorf("error parsing owners: %w", err)
	}

	ownerSet := make(map[string]bool)
	for _, o := range ownersRes.Value {
		ownerSet[o.ID] = true
	}

	var result []TeamMember
	for _, m := range membersRes.Value {
		role := "Member"
		if ownerSet[m.ID] {
			role = "Owner"
		}
		result = append(result, TeamMember{
			ID:          m.ID,
			DisplayName: m.DisplayName,
			Mail:        m.Mail,
			Role:        role,
		})
	}
	return result, nil
}

func (c *Client) GetChannelMembers(channelThreadID string) ([]TeamMember, error) {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/threads/%s/members?view=msnp24Equivalent&pageSize=100&selectMemberRoles=Admin", channelThreadID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("get channel members error %d: %s", resp.StatusCode, string(body))
	}

	var res struct {
		Members []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"members"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parsing channel members: %w", err)
	}

	// Resolve names via Graph
	var result []TeamMember
	for _, m := range res.Members {
		// Extract GUID from "8:orgid:GUID"
		userID := m.ID
		if idx := strings.LastIndex(m.ID, ":"); idx != -1 {
			userID = m.ID[idx+1:]
		}
		userBody, err := c.doReq(fmt.Sprintf("/users/%s?$select=displayName,mail,id", userID))
		if err != nil {
			result = append(result, TeamMember{ID: userID, DisplayName: userID, Role: m.Role})
			continue
		}
		var user struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Mail        string `json:"mail"`
		}
		if err := json.Unmarshal(userBody, &user); err != nil {
			result = append(result, TeamMember{ID: userID, DisplayName: userID, Role: m.Role})
			continue
		}
		role := "Member"
		if m.Role == "Admin" {
			role = "Owner"
		}
		result = append(result, TeamMember{
			ID:          user.ID,
			DisplayName: user.DisplayName,
			Mail:        user.Mail,
			Role:        role,
		})
	}
	return result, nil
}

func (c *Client) AddTeamMember(teamThreadID, teamGUID, userMRI string) error {
	payload := fmt.Sprintf(`{"users":[{"mri":"%s","role":0}],"groupId":"%s"}`, userMRI, teamGUID)
	url := fmt.Sprintf("https://teams.microsoft.com/api/mt/part/amer-02/beta/teams/%s/bulkUpdateRoledMembers?allowAsyncAddition=true", teamThreadID)
	req, err := http.NewRequest("PUT", url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.SpacesToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 201 || resp.StatusCode == 202 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("add member error %d: %s", resp.StatusCode, string(body))
}

func (c *Client) AddChannelMember(teamThreadID, channelThreadID, userID string) error {
	payload := fmt.Sprintf(`{"value":[{"mri":"8:orgid:%s","role":0}]}`, userID)
	url := fmt.Sprintf("https://teams.microsoft.com/api/mt/part/amer-02/beta/teams/%s/channels/%s/members", teamThreadID, channelThreadID)
	req, err := http.NewRequest("PUT", url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.SpacesToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 201 || resp.StatusCode == 202 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("add channel member error %d: %s", resp.StatusCode, string(body))
}

func (c *Client) RemoveTeamMember(teamThreadID, teamGUID, userID string) error {
	payload := fmt.Sprintf(`{"userMri":"8:orgid:%s","updateType":"Left","groupId":"%s"}`, userID, teamGUID)
	url := fmt.Sprintf("https://teams.microsoft.com/api/mt/part/amer-02/beta/teams/%s/members", teamThreadID)
	req, err := http.NewRequest("PUT", url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.SpacesToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("remove member error %d: %s", resp.StatusCode, string(body))
}
