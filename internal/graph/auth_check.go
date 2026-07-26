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
