package graph

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var stripTags = regexp.MustCompile(`<[^>]*>`)
var multipleSpaces = regexp.MustCompile(`[ \t]+`)
var MentionSpan = regexp.MustCompile(`<span[^>]*itemtype="http://schema\.skype\.com/Mention"[^>]*>([^<]+)</span>`)

type ChatSvcError struct {
	StatusCode int
	Message    string
}

func (e *ChatSvcError) Error() string {
	return fmt.Sprintf("chatsvc error %d: %s", e.StatusCode, e.Message)
}

func cleanHTML(content string) string {
	// 0. Re-inject the @ symbol and markers for mentions before stripping tags
	content = MentionSpan.ReplaceAllStringFunc(content, func(match string) string {
		sub := MentionSpan.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		// Use control characters to delimit the exact mention
		return "\x1E@" + sub[1] + "\x1F"
	})

	// 1. Remove literal newlines and tabs from HTML code
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\r", " ")
	content = strings.ReplaceAll(content, "\t", " ")

	// 2. Define breakpoints (visual blocks)
	content = strings.ReplaceAll(content, "</tr>", "\n")
	content = strings.ReplaceAll(content, "</td>", " ")
	content = strings.ReplaceAll(content, "</li>", "\n")
	content = strings.ReplaceAll(content, "<li>", "• ")
	content = strings.ReplaceAll(content, "<br>", "\n")
	content = strings.ReplaceAll(content, "<br/>", "\n")
	content = strings.ReplaceAll(content, "<br />", "\n")
	content = strings.ReplaceAll(content, "</p>", "\n")
	content = strings.ReplaceAll(content, "</div>", "\n")

	// 3. Strip remaining tags
	content = stripTags.ReplaceAllString(content, "")

	// 4. Decode HTML entities (converts &quot; to ")
	content = html.UnescapeString(content)

	// 5. Collapse horizontal whitespace
	content = multipleSpaces.ReplaceAllString(content, " ")

	// 6. Trim empty lines and leading/trailing whitespace from each line
	lines := strings.Split(content, "\n")
	var out []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return strings.Join(out, "\n")
}

const baseURL = "https://graph.microsoft.com/v1.0"

type Client struct {
	GraphToken  string
	WebToken    string
	NotifToken  string
	EduToken    string
	Cookie      string
	EduCookie   string
	SpacesToken string
	FabricToken string
	HTTPClient  *http.Client
}

func NewClient(graphToken, webToken, notifToken, eduToken, cookie, eduCookie, spacesToken, fabricToken string) *Client {
	return &Client{
		GraphToken:  graphToken,
		WebToken:    webToken,
		NotifToken:  notifToken,
		EduToken:    eduToken,
		Cookie:      cookie,
		EduCookie:   eduCookie,
		SpacesToken: spacesToken,
		FabricToken: fabricToken,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) doReq(endpoint string) ([]byte, error) {
	req, err := http.NewRequest("GET", baseURL+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("Graph API error (%d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *Client) ReloadTokens() error {
	path := filepath.Join(os.Getenv("HOME"), ".config", "teamstui", "tokens.env")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		tokens[key] = val
	}
	if v := tokens["MS_GRAPH_TOKEN"]; v != "" {
		c.GraphToken = strings.TrimPrefix(v, "Bearer ")
	}
	if v := tokens["TEAMS_WEB_TOKEN"]; v != "" {
		c.WebToken = strings.TrimPrefix(v, "Bearer ")
	}
	if v := tokens["TEAMS_NOTIF_TOKEN"]; v != "" {
		c.NotifToken = strings.TrimPrefix(v, "Bearer ")
	}
	if v := tokens["EDU_TOKEN"]; v != "" {
		c.EduToken = strings.TrimPrefix(v, "Bearer ")
	}
	if v := tokens["TEAMS_COOKIE"]; v != "" {
		c.Cookie = v
	}
	if v := tokens["EDU_COOKIE"]; v != "" {
		c.EduCookie = v
	}
	if v := tokens["TEAMS_SPACES_TOKEN"]; v != "" {
		c.SpacesToken = strings.TrimPrefix(v, "Bearer ")
	}
	if v := tokens["TEAMS_FABRIC_TOKEN"]; v != "" {
		c.FabricToken = strings.TrimPrefix(v, "Bearer ")
	}
	return nil
}

func (c *Client) GetTenantID() string {
	parts := strings.Split(c.WebToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	payload = strings.NewReplacer("-", "+", "_", "/").Replace(payload)
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	if tid, ok := claims["tid"].(string); ok {
		return tid
	}
	return ""
}
