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

	"teamsTUI/internal/helpers"
)

var (
	stripTags       = regexp.MustCompile(`<[^>]*>`)
	multipleSpaces  = regexp.MustCompile(`[ \t]+`)
	MentionSpan     = regexp.MustCompile(`<span[^>]*itemtype="http://schema\.skype\.com/Mention"[^>]*>([^<]+)</span>`)
	boldRe          = regexp.MustCompile(`(?i)<(b|strong)(\s+[^>]*)?>`)
	boldCloseRe     = regexp.MustCompile(`(?i)</(b|strong)\s*>`)
	italicRe        = regexp.MustCompile(`(?i)<(i|em)(\s+[^>]*)?>`)
	italicCloseRe   = regexp.MustCompile(`(?i)</(i|em)\s*>`)
	strikeRe        = regexp.MustCompile(`(?i)<(s|strike|del)(\s+[^>]*)?>`)
	strikeCloseRe   = regexp.MustCompile(`(?i)</(s|strike|del)\s*>`)
	linkRe          = regexp.MustCompile(`(?i)<a\s+[^>]*href="([^"]+)"[^>]*>([^<]+)</a>`)
	codeRe          = regexp.MustCompile(`(?i)<code(\s+[^>]*)?>`)
	codeCloseRe     = regexp.MustCompile(`(?i)</code\s*>`)
	preRe           = regexp.MustCompile(`(?i)<pre(\s+[^>]*)?>`)
	preCloseRe      = regexp.MustCompile(`(?i)</pre\s*>`)
	ulRe            = regexp.MustCompile(`(?i)<ul(\s+[^>]*)?>`)
	ulCloseRe       = regexp.MustCompile(`(?i)</ul\s*>`)
	olRe            = regexp.MustCompile(`(?i)<ol(\s+[^>]*)?>`)
	olCloseRe       = regexp.MustCompile(`(?i)</ol\s*>`)
	liRe            = regexp.MustCompile(`(?i)<li(\s+[^>]*)?>`)
	liCloseRe       = regexp.MustCompile(`(?i)</li\s*>`)
	brRe            = regexp.MustCompile(`(?i)<br\s*/?>`)
	pCloseRe        = regexp.MustCompile(`(?i)</p\s*>`)
	divCloseRe      = regexp.MustCompile(`(?i)</div\s*>`)
	blockquoteRe    = regexp.MustCompile(`(?i)<blockquote(\s+[^>]*)?>`)
	blockquoteCloseRe = regexp.MustCompile(`(?i)</blockquote\s*>`)
	imgRe           = regexp.MustCompile(`(?i)<img[^>]*>`)
	attachmentRe    = regexp.MustCompile(`(?i)<attachment[^>]*>.*?</attachment>`)
	headingRe       [6]*regexp.Regexp
	headingCloseRe  [6]*regexp.Regexp
)

func init() {
	for i := 1; i <= 6; i++ {
		headingRe[i-1] = regexp.MustCompile(fmt.Sprintf(`(?i)<h%d(\s+[^>]*)?>`, i))
		headingCloseRe[i-1] = regexp.MustCompile(fmt.Sprintf(`(?i)</h%d\s*>`, i))
	}
}

type ChatSvcError struct {
	StatusCode int
	Message    string
}

func (e *ChatSvcError) Error() string {
	return fmt.Sprintf("chatsvc error %d: %s", e.StatusCode, e.Message)
}

func cleanHTML(s string) string {
	// Step 1: Mentions → sentinel
	s = MentionSpan.ReplaceAllStringFunc(s, func(m string) string {
		sub := MentionSpan.FindStringSubmatch(m)
		if len(sub) > 1 {
			return "\x1E@" + sub[1] + "\x1F"
		}
		return m
	})

	// Step 2: normalize whitespace
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)

	// Step 3: semantic HTML → Markdown
	// Headings
	for i := 1; i <= 6; i++ {
		prefix := "\n" + strings.Repeat("#", i) + " "
		s = headingRe[i-1].ReplaceAllString(s, prefix)
		s = headingCloseRe[i-1].ReplaceAllString(s, "\n")
	}

	// Bold, italic, strikethrough
	s = boldRe.ReplaceAllString(s, "**")
	s = boldCloseRe.ReplaceAllString(s, "**")
	s = italicRe.ReplaceAllString(s, "*")
	s = italicCloseRe.ReplaceAllString(s, "*")
	s = strikeRe.ReplaceAllString(s, "~~")
	s = strikeCloseRe.ReplaceAllString(s, "~~")

	// Links
	s = linkRe.ReplaceAllString(s, "[$2]($1)")

	// Code
	s = codeRe.ReplaceAllString(s, "`")
	s = codeCloseRe.ReplaceAllString(s, "`")
	s = preRe.ReplaceAllString(s, "\n```\n")
	s = preCloseRe.ReplaceAllString(s, "\n```\n")

	// Lists
	s = ulRe.ReplaceAllString(s, "\n")
	s = ulCloseRe.ReplaceAllString(s, "\n")
	s = olRe.ReplaceAllString(s, "\n")
	s = olCloseRe.ReplaceAllString(s, "\n")
	s = liRe.ReplaceAllString(s, "\n- ")
	s = liCloseRe.ReplaceAllString(s, "")

	// Blockquote
	s = blockquoteRe.ReplaceAllString(s, "\n> ")
	s = blockquoteCloseRe.ReplaceAllString(s, "\n")

	// Tables → bullet lists
	s = strings.ReplaceAll(s, "<table", "\n<table")
	s = strings.ReplaceAll(s, "</table>", "\n")
	s = strings.ReplaceAll(s, "<tbody>", "\n")
	s = strings.ReplaceAll(s, "</tbody>", "\n")
	s = strings.ReplaceAll(s, "<tr>", "\n- ")
	s = strings.ReplaceAll(s, "</tr>", "")
	s = strings.ReplaceAll(s, "<td>", "")
	s = strings.ReplaceAll(s, "</td>", "")
	s = strings.ReplaceAll(s, "<th>", "")
	s = strings.ReplaceAll(s, "</th>", "")

	// Images and Attachments
	s = imgRe.ReplaceAllString(s, "[Attached Image]")
	s = attachmentRe.ReplaceAllString(s, "[Attached File]")

	// Block separators
	s = brRe.ReplaceAllString(s, "\n")
	s = pCloseRe.ReplaceAllString(s, "\n")
	s = divCloseRe.ReplaceAllString(s, "\n")

	// Step 4: strip remaining tags
	s = stripTags.ReplaceAllString(s, "")

	// Fix Markdown spacing for bold, italic, strikethrough
	fixMarkdownSpacing := func(marker string, content string) string {
		re := regexp.MustCompile(regexp.QuoteMeta(marker) + `(.*?)` + regexp.QuoteMeta(marker))
		return re.ReplaceAllStringFunc(content, func(match string) string {
			inner := match[len(marker) : len(match)-len(marker)]
			leftSpace, rightSpace := "", ""
			if strings.HasPrefix(inner, " ") {
				leftSpace = " "
				inner = strings.TrimLeft(inner, " ")
			}
			if strings.HasSuffix(inner, " ") {
				rightSpace = " "
				inner = strings.TrimRight(inner, " ")
			}
			if inner == "" {
				return leftSpace + rightSpace
			}
			return leftSpace + marker + inner + marker + rightSpace
		})
	}
	s = fixMarkdownSpacing("**", s)
	s = fixMarkdownSpacing("*", s)
	s = fixMarkdownSpacing("~~", s)

	// Step 5: unescape HTML entities
	s = html.UnescapeString(s)

	// Step 6: collapse horizontal whitespace
	s = multipleSpaces.ReplaceAllString(s, " ")

	// Step 7: trim lines, preserve single blank lines for Markdown structure
	lines := strings.Split(s, "\n")
	var result []string
	prevBlank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !prevBlank {
				result = append(result, "")
			}
			prevBlank = true
		} else {
			result = append(result, line)
			prevBlank = false
		}
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
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
	SelfID      string
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
			Transport: &http.Transport{
				MaxConnsPerHost: 10,
				MaxIdleConns:    12,
				IdleConnTimeout: 90 * time.Second,
			},
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
	path := filepath.Join(helpers.ConfigDir(), "tokens.env")
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
