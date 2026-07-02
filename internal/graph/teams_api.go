package graph

import (
	"encoding/json"
	"fmt"
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
