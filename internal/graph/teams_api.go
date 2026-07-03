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
}

type Channel struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
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

// GetJoinedTeamsRaw fetches the raw JSON (useful for quick debugging).
func (c *Client) GetJoinedTeamsRaw() ([]byte, error) {
	return c.doReq("/me/joinedTeams")
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

	return allChannels, nil
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
