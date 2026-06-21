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

// GetJoinedTeams obtiene los equipos de los que el usuario es miembro.
func (c *Client) GetJoinedTeams() ([]Team, error) {
	body, err := c.doReq("/me/joinedTeams")
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []Team `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parseando equipos: %w", err)
	}

	return res.Value, nil
}

// GetJoinedTeamsRaw obtiene el JSON crudo (útil para la prueba rápida).
func (c *Client) GetJoinedTeamsRaw() ([]byte, error) {
	return c.doReq("/me/joinedTeams")
}

// GetChannels obtiene los canales de un equipo específico.
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
			return nil, fmt.Errorf("error parseando canales: %w", err)
		}

		allChannels = append(allChannels, res.Value...)

		if res.NextLink != "" {
			// NextLink suele ser absoluta, le quitamos la baseURL si la tiene
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
