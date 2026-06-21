package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SetPresence actualiza el estado de presencia preferido del usuario.
// Posibles valores para availability y activity:
// "Available", "Busy", "DoNotDisturb", "BeRightBack", "Away", "Offline"
func (c *Client) SetPresence(userID, availability, activity string) error {
	endpoint := fmt.Sprintf("%s/users/%s/presence/setUserPreferredPresence", baseURL, userID)

	payload := map[string]interface{}{
		"availability":       availability,
		"activity":           activity,
		"expirationDuration": "PT1H", // 1 hora por defecto (Graph lo recomienda o lo exige a veces)
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("error de red: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ClearPresence borra la presencia preferida (vuelve a la presencia calculada automáticamente por Teams).
func (c *Client) ClearPresence(userID string) error {
	endpoint := fmt.Sprintf("%s/users/%s/presence/clearUserPreferredPresence", baseURL, userID)

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("error de red: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *Client) GetPresences(userIDs []string) (map[string]string, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(userIDs))
	type presResult struct {
		id    string
		avail string
		err   error
	}

	ch := make(chan presResult, len(userIDs))
	for _, uid := range userIDs {
		go func(id string) {
			avail, err := c.getPresence(id)
			ch <- presResult{id: id, avail: avail, err: err}
		}(uid)
	}

	for range userIDs {
		r := <-ch
		if r.err == nil && r.avail != "" {
			result[r.id] = r.avail
		}
	}

	return result, nil
}

func (c *Client) getPresence(userID string) (string, error) {
	url := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s/presence", userID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("presence %d", resp.StatusCode)
	}

	var res struct {
		Availability string `json:"availability"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}
	return res.Availability, nil
}
