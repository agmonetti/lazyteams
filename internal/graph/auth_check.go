package graph

import (
	"net/http"
	"sync"
)

type TokenCheckResult struct {
	TokenType string
	Expired   bool
}

// CheckAllTokens validates all tokens in parallel using lightweight requests.
// Returns a list of expired token types.
func (c *Client) CheckAllTokens() []string {
	checks := []struct {
		tokenType string
		check     func() bool
	}{
		{"graph", func() bool { return c.checkEndpoint("https://graph.microsoft.com/v1.0/me", c.GraphToken) }},
		{"web", func() bool {
			return c.checkEndpoint("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/properties", c.WebToken)
		}},
		{"notif", func() bool {
			return c.checkEndpoint("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/48:notifications/messages?startTime=0&pageSize=1&view=msnp24Equivalent&targetType=Passport", c.NotifToken)
		}},
		{"edu", func() bool {
			return c.checkEndpoint("https://assignments.edu.cloud.microsoft/api/v1.0/edu/me/work?$top=1", c.EduToken)
		}},
		{"fabric", func() bool {
			return c.checkEndpointFabric(c.FabricToken)
		}},
	}

	var mu sync.Mutex
	var expired []string
	var wg sync.WaitGroup

	for _, ch := range checks {
		wg.Add(1)
		go func(tokenType string, check func() bool) {
			defer wg.Done()
			if check() {
				mu.Lock()
				expired = append(expired, tokenType)
				mu.Unlock()
			}
		}(ch.tokenType, ch.check)
	}

	wg.Wait()
	
	return expired
}

// checkEndpoint returns true if the token is expired (401).
func (c *Client) checkEndpoint(url, token string) bool {
	if token == "" {
		return false // Skip if token not configured
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 401
}

// checkEndpointFabric returns true if the fabric token is expired.
// Note: The Teams Fabric API returns 403 Forbidden (not 401) when the
// TEAMS_FABRIC_TOKEN is expired — this is expected behavior, not a permissions error.
func (c *Client) checkEndpointFabric(token string) bool {
	if token == "" {
		return false
	}
	req, err := http.NewRequest("GET",
		"https://teams.microsoft.com/api/mt/amer/beta/teams/fabric/amer/templates?templateType=standard",
		nil,
	)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-ms-client-type", "web")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// 403 Forbidden = token expired (known Fabric API behavior)
	// 401 Unauthorized = also expired
	return resp.StatusCode == 401 || resp.StatusCode == 403
}
