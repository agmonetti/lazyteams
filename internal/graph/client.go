package graph

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var stripTags = regexp.MustCompile(`<[^>]*>`)
var multipleSpaces = regexp.MustCompile(`[ \t]+`)

type ChatSvcError struct {
	StatusCode int
	Message    string
}

func (e *ChatSvcError) Error() string {
	return fmt.Sprintf("chatsvc error %d: %s", e.StatusCode, e.Message)
}

func cleanHTML(content string) string {
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
	GraphToken string
	WebToken   string
	NotifToken string
	EduToken   string
	Cookie     string
	EduCookie  string
	HTTPClient *http.Client
}

func NewClient(graphToken, webToken, notifToken, eduToken, cookie, eduCookie string) *Client {
	return &Client{
		GraphToken: graphToken,
		WebToken:   webToken,
		NotifToken: notifToken,
		EduToken:   eduToken,
		Cookie:     cookie,
		EduCookie:  eduCookie,
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
