package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Attachment struct {
	Name string
	URL  string
	Type string // "file" or "link"
}

type Reaction struct {
	Key         string
	Count       int
	UserReacted bool
}

type Message struct {
	ID            string
	RootMessageID string // if == ID, it is a root message; if != ID, it is a reply
	Body          string
	RawBody       string // original unprocessed content, for editing
	FromName      string
	FromUserID    string // "8:orgid:GUID" to compare authorship
	CreatedAt     time.Time
	MessageType   string
	Deleted       bool
	Attachments   []Attachment
	Reactions     []Reaction
}

// Internal structures for ChatSvc (Teams Web) response
type chatSvcResponse struct {
	Messages []struct {
		ID                  string                 `json:"id"`
		RootMessageID       string                 `json:"rootMessageId"`
		Type                string                 `json:"type"`
		MessageType         string                 `json:"messagetype"`
		Content             string                 `json:"content"`
		ImDisplayName       string                 `json:"imdisplayname"`
		FromUserId          string                 `json:"fromUserId"`
		OriginalArrivalTime string                 `json:"originalarrivaltime"`
		Properties          map[string]interface{} `json:"properties"`
		AnnotationsSummary  *struct {
			Emotions map[string]int `json:"emotions"`
		} `json:"annotationsSummary"`
	} `json:"messages"`
	Metadata struct {
		BackwardLink string `json:"backwardLink"`
	} `json:"_metadata"`
}

// parseReactions extracts reactions from the "emotions" key in properties.
// The emotions can be either an []interface{} or a JSON string.
func parseReactions(properties map[string]interface{}) []Reaction {
	if properties == nil || properties["emotions"] == nil {
		return nil
	}

	var reactions []Reaction

	if emoList, ok := properties["emotions"].([]interface{}); ok {
		for _, e := range emoList {
			if emoMap, ok := e.(map[string]interface{}); ok {
				if key, ok := emoMap["key"].(string); ok {
					if users, ok := emoMap["users"].([]interface{}); ok {
						if len(users) > 0 {
							reactions = append(reactions, Reaction{Key: key, Count: len(users)})
						}
					}
				}
			}
		}
	} else if emoStr, ok := properties["emotions"].(string); ok {
		var emoList []struct {
			Key   string        `json:"key"`
			Users []interface{} `json:"users"`
		}
		if json.Unmarshal([]byte(emoStr), &emoList) == nil {
			for _, e := range emoList {
				if len(e.Users) > 0 {
					reactions = append(reactions, Reaction{Key: e.Key, Count: len(e.Users)})
				}
			}
		}
	}

	sort.Slice(reactions, func(i, j int) bool {
		return reactions[i].Count > reactions[j].Count
	})
	return reactions
}

// GetMessages fetches messages from a channel using the internal API (ChatSvc)
func (c *Client) GetMessagesWithLink(teamID, channelID string, pageSize int) ([]Message, string, error) {
	var allMsgs []Message
	batchSize := pageSize
	if batchSize > 200 {
		batchSize = 200
	}

	maxPages := (pageSize + batchSize - 1) / batchSize
	if maxPages > 10 {
		maxPages = 10
	}

	urlStr := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages?view=msnp24Equivalent|supportsMessageProperties&pageSize=%d&startTime=1", channelID, batchSize)

	var lastBackwardLink string

	for page := 0; page < maxPages && len(allMsgs) < pageSize; page++ {
		// urlStr comes from res.Metadata.BackwardLink after the first page;
		// never attach the WebToken to a non-Microsoft host.
		if err := validateMicrosoftURL(urlStr); err != nil {
			return allMsgs, lastBackwardLink, err
		}
		req, err := http.NewRequest(http.MethodGet, urlStr, nil)
		if err != nil {
			return allMsgs, lastBackwardLink, err
		}

		req.Header.Set("Authorization", "Bearer "+c.WebToken)
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("behavioroverride", "redirectAs404")
		req.Header.Set("x-ms-migration", "True")
		req.Header.Set("x-ms-request-priority", "0")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
		req.Header.Set("Referer", "https://teams.microsoft.com/")
		req.Header.Set("Origin", "https://teams.microsoft.com")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return allMsgs, lastBackwardLink, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return allMsgs, lastBackwardLink, &ChatSvcError{StatusCode: resp.StatusCode, Message: string(body)}
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return allMsgs, lastBackwardLink, fmt.Errorf("error reading response: %w", err)
		}

		var res chatSvcResponse
		if err := json.Unmarshal(body, &res); err != nil {
			return allMsgs, lastBackwardLink, fmt.Errorf("error parsing messages: %w", err)
		}

		for _, m := range res.Messages {
			t, _ := time.Parse(time.RFC3339, m.OriginalArrivalTime)
			name := m.ImDisplayName
			if name == "" {
				name = "User"
			}

			var attachments []Attachment

			// Extract OneDrive/SharePoint links or other attachments
			if linksStr, ok := m.Properties["links"].(string); ok && linksStr != "[]" && linksStr != "" {
				var links []map[string]interface{}
				if json.Unmarshal([]byte(linksStr), &links) == nil {
					for _, l := range links {
						u, _ := l["url"].(string)
						if u != "" {
							title := u
							if preview, ok := l["preview"].(map[string]interface{}); ok {
								if pt, ok := preview["title"].(string); ok && pt != "" {
									title = pt
								}
							}
							title = strings.ReplaceAll(title, "\n", "")
							title = strings.ReplaceAll(title, "\r", "")
							title = strings.TrimSpace(title)

							attachments = append(attachments, Attachment{
								Name: title,
								URL:  u,
								Type: "link",
							})
						}
					}
				}
			}

			// Extract direct files
			if filesStr, ok := m.Properties["files"].(string); ok && filesStr != "[]" && filesStr != "" {
				var files []map[string]interface{}
				if json.Unmarshal([]byte(filesStr), &files) == nil {
					for _, f := range files {
						fname := "Attached file"
						if n, ok := f["fileName"].(string); ok && n != "" {
							fname = n
						} else if n, ok := f["name"].(string); ok && n != "" {
							fname = n
						} else if n, ok := f["title"].(string); ok && n != "" {
							fname = n
						}

						furl := ""
						if u, ok := f["fileUrl"].(string); ok && u != "" {
							furl = u
						} else if u, ok := f["url"].(string); ok && u != "" {
							furl = u
						} else if fi, ok := f["fileInfo"].(map[string]interface{}); ok {
							if u, ok := fi["fileUrl"].(string); ok && u != "" {
								furl = u
							} else if u, ok := fi["serverRelativeUrl"].(string); ok && u != "" {
								furl = u
							}
						}

						if furl != "" {
							attachments = append(attachments, Attachment{
								Name: fname,
								URL:  furl,
								Type: "file",
							})
						}
					}
				}
			}

			// Extract AMS images from HTML
			imgSrcRe := regexp.MustCompile(`(?i)<img[^>]*src="([^"]+)"`)
			matches := imgSrcRe.FindAllStringSubmatch(m.Content, -1)
			for i, match := range matches {
				if len(match) > 1 {
					attachments = append(attachments, Attachment{
						Name: fmt.Sprintf("Image_%d.png", i+1),
						URL:  match[1],
						Type: "ams_image",
					})
				}
			}

			rootID := m.RootMessageID
			if rootID == "" {
				rootID = m.ID
			}

			var reactions []Reaction
			if m.AnnotationsSummary != nil {
				for key, count := range m.AnnotationsSummary.Emotions {
					if count > 0 {
						reactions = append(reactions, Reaction{Key: key, Count: count})
					}
				}
				sort.Slice(reactions, func(i, j int) bool {
					return reactions[i].Count > reactions[j].Count
				})
			} else {
				reactions = parseReactions(m.Properties)
			}

			isDeleted := false
			if m.Properties != nil {
				if _, ok := m.Properties["deletetime"]; ok {
					isDeleted = true
				}
			}

			allMsgs = append(allMsgs, Message{
				ID:            m.ID,
				RootMessageID: rootID,
				Body:          cleanHTML(m.Content),
				RawBody:       m.Content,
				FromName:      name,
				FromUserID:    m.FromUserId,
				CreatedAt:     t,
				MessageType:   m.MessageType,
				Deleted:       isDeleted,
				Attachments:   attachments,
				Reactions:     reactions,
			})
		}

		if len(res.Messages) == 0 {
			break
		}
		if res.Metadata.BackwardLink != "" {
			lastBackwardLink = res.Metadata.BackwardLink
		}
		if res.Metadata.BackwardLink == "" {
			break
		}
		urlStr = res.Metadata.BackwardLink
	}

	if len(allMsgs) > pageSize {
		allMsgs = allMsgs[:pageSize]
	}

	sortMessagesNewestFirst(allMsgs)

	return allMsgs, lastBackwardLink, nil
}

type MessagePage struct {
	Messages     []Message
	BackwardLink string
}

// sortMessagesNewestFirst orders messages newest-first so the UI's assumption
// that messages[0] is the latest holds regardless of the order the chatsvc
// returns (it can be non-chronological for some channels). Stable for equal
// timestamps, so channels already delivered newest-first are unchanged.
func sortMessagesNewestFirst(msgs []Message) {
	sort.SliceStable(msgs, func(i, j int) bool {
		return msgs[i].CreatedAt.After(msgs[j].CreatedAt)
	})
}

// GetMessagesFromLink fetches a page of messages using a backwardLink URL directly.
func (c *Client) GetMessagesFromLink(link string) (MessagePage, error) {
	// The link comes from a server response; never attach the WebToken to a
	// non-Microsoft host.
	if err := validateMicrosoftURL(link); err != nil {
		return MessagePage{}, err
	}
	req, err := http.NewRequest(http.MethodGet, link, nil)
	if err != nil {
		return MessagePage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	req.Header.Set("x-ms-request-priority", "0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	req.Header.Set("Referer", "https://teams.microsoft.com/")
	req.Header.Set("Origin", "https://teams.microsoft.com")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return MessagePage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return MessagePage{}, fmt.Errorf("loadMore error %d: %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MessagePage{}, err
	}
	var res chatSvcResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return MessagePage{}, err
	}

	var msgs []Message
	for _, m := range res.Messages {
		t, _ := time.Parse(time.RFC3339, m.OriginalArrivalTime)
		name := m.ImDisplayName
		if name == "" {
			name = "User"
		}
		rootID := m.RootMessageID
		if rootID == "" {
			rootID = m.ID
		}
		isDeleted := false
		if m.Properties != nil {
			if _, ok := m.Properties["deletetime"]; ok {
				isDeleted = true
			}
		}

		var attachments []Attachment
		if m.Properties != nil && m.Properties["files"] != nil {
			if fStr, ok := m.Properties["files"].(string); ok {
				var files []map[string]interface{}
				if json.Unmarshal([]byte(fStr), &files) == nil {
					for _, f := range files {
						fname, _ := f["name"].(string)
						var furl string
						if u, ok := f["fileUrl"].(string); ok && u != "" {
							furl = u
						}
						if furl != "" {
							attachments = append(attachments, Attachment{
								Name: fname,
								URL:  furl,
								Type: "file",
							})
						}
					}
				}
			}
		}

		var reactions []Reaction
		if m.AnnotationsSummary != nil {
			for key, count := range m.AnnotationsSummary.Emotions {
				if count > 0 {
					reactions = append(reactions, Reaction{Key: key, Count: count})
				}
			}
			sort.Slice(reactions, func(i, j int) bool {
				return reactions[i].Count > reactions[j].Count
			})
		} else {
			reactions = parseReactions(m.Properties)
		}

		msgs = append(msgs, Message{
			ID:            m.ID,
			RootMessageID: rootID,
			Body:          cleanHTML(m.Content),
			RawBody:       m.Content,
			FromName:      name,
			FromUserID:    m.FromUserId,
			CreatedAt:     t,
			MessageType:   m.MessageType,
			Deleted:       isDeleted,
			Attachments:   attachments,
			Reactions:     reactions,
		})
	}
	sortMessagesNewestFirst(msgs)
	return MessagePage{Messages: msgs, BackwardLink: res.Metadata.BackwardLink}, nil
}

// SendMessage sends a text message to the specified channel using the internal API.
// If replyTo is non-nil, the message includes a quoted reply blockquote.
func (c *Client) SendMessage(channelID, content string, mentions []MentionedUser, replyTo *Message) error {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages", channelID)

	qtdMsgs := "[]"

	properties := map[string]string{
		"importance": "",
		"subject":    "",
		"title":      "",
		"cards":      "[]",
		"links":      "[]",
		"files":      "[]",
		"mentions":   "[]",
	}

	var htmlContent string
	if len(mentions) > 0 {
		var mentionsJSON string
		htmlContent, mentionsJSON = BuildMentionContent(content, mentions)
		properties["mentions"] = mentionsJSON
	} else {
		htmlContent = "<p>" + html.EscapeString(content) + "</p>"
	}

	if replyTo != nil {
		preview := replyTo.Body
		if len([]rune(preview)) > 100 {
			preview = string([]rune(preview)[:100]) + "..."
		}
		ts := replyTo.CreatedAt.UnixMilli()
		senderMRI := "8:orgid:" + replyTo.FromUserID
		if strings.HasPrefix(replyTo.FromUserID, "8:") {
			senderMRI = replyTo.FromUserID
		}

		blockquote := fmt.Sprintf(
			`<blockquote itemscope itemtype="http://schema.skype.com/Reply" itemid="%d">`+
				`<strong itemprop="mri" itemid="%s">%s</strong>`+
				`<span itemprop="time" itemid="%d"></span>`+
				`<p itemprop="preview"> %s</p>`+
				`</blockquote>`,
			ts, senderMRI, replyTo.FromName, ts, preview,
		)
		htmlContent = blockquote + htmlContent

		qtdMsgs = fmt.Sprintf(
			`[{"messageId":"%d","sender":"%s","time":%d}]`,
			ts, senderMRI, ts,
		)
		properties["qtdMsgs"] = qtdMsgs
	}

	payload := map[string]interface{}{
		"content":     htmlContent,
		"messagetype": "RichText/Html",
		"contenttype": "Text",
		"properties":  properties,
	}
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	req.Header.Set("x-ms-request-priority", "0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	req.Header.Set("Referer", "https://teams.microsoft.com/")
	req.Header.Set("Origin", "https://teams.microsoft.com")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chatsvc send error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func buildInlineImageHTML(text string, img *AMSImage, dataLen int) string {
	imgTag := fmt.Sprintf(
		`<p><img src="%s" itemtype="http://schema.skype.com/AMSImage" itemscope="png" data-loading-state="success" data-inline-image="true" data-file-size="%d" alt="image" id="%s" itemid="%s" href="%s" target-src="%s"></p>`,
		img.URL, dataLen, img.ObjectID, img.ObjectID, img.URL, img.URL,
	)

	if text == "" || strings.TrimSpace(text) == "" {
		return imgTag
	}

	return imgTag + "<p>" + html.EscapeString(text) + "</p>"
}

// SendMessageWithInlineImage sends a message with inline AMS image references.
// If replyTo is non-nil, the message includes a quoted reply blockquote.
func (c *Client) SendMessageWithInlineImage(
	channelID string,
	text string,
	img *AMSImage,
	dataLen int,
	replyTo *Message,
) error {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages", channelID)

	htmlContent := buildInlineImageHTML(text, img, dataLen)

	payload := map[string]interface{}{
		"content":       htmlContent,
		"messagetype":   "RichText/Html",
		"contenttype":   "Text",
		"amsreferences": []string{img.ObjectID},
	}

	if replyTo != nil {
		preview := replyTo.Body
		if len([]rune(preview)) > 100 {
			preview = string([]rune(preview)[:100]) + "..."
		}
		ts := replyTo.CreatedAt.UnixMilli()
		senderMRI := "8:orgid:" + replyTo.FromUserID
		if strings.HasPrefix(replyTo.FromUserID, "8:") {
			senderMRI = replyTo.FromUserID
		}

		blockquote := fmt.Sprintf(
			`<blockquote itemscope itemtype="http://schema.skype.com/Reply" itemid="%d">`+
				`<strong itemprop="mri" itemid="%s">%s</strong>`+
				`<span itemprop="time" itemid="%d"></span>`+
				`<p itemprop="preview"> %s</p>`+
				`</blockquote>`,
			ts, senderMRI, replyTo.FromName, ts, preview,
		)
		htmlContent = blockquote + htmlContent

		qtdMsgs := fmt.Sprintf(
			`[{"messageId":"%d","sender":"%s","time":%d}]`,
			ts, senderMRI, ts,
		)
		payload["properties"] = map[string]string{
			"qtdMsgs": qtdMsgs,
		}
	}
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	req.Header.Set("x-ms-request-priority", "0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	req.Header.Set("Referer", "https://teams.microsoft.com/")
	req.Header.Set("Origin", "https://teams.microsoft.com")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chatsvc send error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) SendReply(channelID, parentMessageID, content string, mentions []MentionedUser) error {
	url := fmt.Sprintf(
		"https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s%%3Bmessageid%%3D%s/messages",
		channelID, parentMessageID,
	)

	htmlContent := "<p>" + html.EscapeString(content) + "</p>"
	properties := map[string]string{
		"importance": "",
		"subject":    "",
		"title":      "",
		"cards":      "[]",
		"links":      "[]",
		"files":      "[]",
		"mentions":   "[]",
	}

	if len(mentions) > 0 {
		var mentionsJSON string
		htmlContent, mentionsJSON = BuildMentionContent(content, mentions)
		properties["mentions"] = mentionsJSON
	}

	payload := map[string]interface{}{
		"content":     htmlContent,
		"messagetype": "RichText/Html",
		"contenttype": "Text",
		"properties":  properties,
	}
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	req.Header.Set("x-ms-request-priority", "0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	req.Header.Set("Referer", "https://teams.microsoft.com/")
	req.Header.Set("Origin", "https://teams.microsoft.com")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chatsvc reply error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
func (c *Client) AddReaction(channelID, messageID, key string) error {
	url := fmt.Sprintf(
		"https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages/%s/properties?name=emotions",
		channelID, messageID,
	)
	payload := map[string]any{
		"emotions": map[string]any{
			"key":   key,
			"value": time.Now().UnixMilli(),
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reaction error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) RemoveReaction(channelID, messageID, key string) error {
	url := fmt.Sprintf(
		"https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages/%s/properties?name=emotions",
		channelID, messageID,
	)
	payload := map[string]any{
		"emotions": map[string]any{
			"key": key,
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("DELETE", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remove reaction error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) GetMessageReactions(channelID, messageID, selfID string) (map[string]bool, error) {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages/%s?view=msnp24Equivalent|supportsMessageProperties", channelID, messageID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var res struct {
		Properties struct {
			Emotions []struct {
				Key   string `json:"key"`
				Users []struct {
					MRI string `json:"mri"`
				} `json:"users"`
			} `json:"emotions"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	// map[reactionKey] = userAlreadyReacted
	result := make(map[string]bool)
	for _, e := range res.Properties.Emotions {
		for _, u := range e.Users {
			// Extract GUID from "8:orgid:GUID"
			if idx := strings.LastIndex(u.MRI, ":"); idx != -1 {
				if u.MRI[idx+1:] == selfID {
					result[e.Key] = true
				}
			}
		}
	}
	return result, nil
}

func (c *Client) MarkConversationAsRead(conversationID string, lastMsg Message) error {
	ts := lastMsg.CreatedAt.UnixMilli()
	nowTs := time.Now().UnixMilli()
	horizon := fmt.Sprintf("%d;%d;%d", ts, nowTs, ts)

	payload := struct {
		ConsumptionHorizon string `json:"consumptionhorizon"`
	}{ConsumptionHorizon: horizon}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/properties?name=consumptionhorizon",
		conversationID)

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
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
	return fmt.Errorf("mark as read error %d: %s", resp.StatusCode, string(body))
}

func (c *Client) EditMessage(channelID, messageID, content string) error {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages/%s", channelID, messageID)
	payload := map[string]interface{}{
		"content":     fmt.Sprintf("<p>%s</p>", html.EscapeString(content)),
		"messagetype": "RichText/Html",
		"contenttype": "Text",
	}
	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("edit message error %d: %s", resp.StatusCode, string(b))
}

func (c *Client) DeleteMessage(channelID, messageID string) error {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages/%s?behavior=softDelete", channelID, messageID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.WebToken)
	req.Header.Set("behavioroverride", "redirectAs404")
	req.Header.Set("x-ms-migration", "True")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("delete message error %d: %s", resp.StatusCode, string(b))
}

// MentionedUser represents a resolved @mention ready to embed in a message.
type MentionedUser struct {
	ItemID      int    // incremental index (0, 1, 2...)
	MRI         string // "8:orgid:GUID"
	DisplayName string // shown inside the <span>
}

// BuildMentionContent converts plain text with @mentions into the Teams HTML
// format and returns the content string + the mentions JSON string for properties.
//
// mentions must be pre-resolved (MRI known) and ItemID set sequentially.
// The text must contain the display names exactly as stored in MentionedUser.DisplayName.
func BuildMentionContent(text string, mentions []MentionedUser) (content, mentionsJSON string) {
	// 1. Escape the entire user text first to prevent HTML injection
	result := html.EscapeString(text)

	// Replace each @DisplayName occurrence with the Teams readonly span.
	// We iterate by ItemID order so indices are injected correctly.
	for _, m := range mentions {
		// 2. Escape the display name in case a user has a malicious HTML name
		safeName := html.EscapeString(m.DisplayName)

		tag := fmt.Sprintf(
			`<readonly class="skipProofing" itemtype="http://schema.skype.com/Mention" spellcheck="false" contenteditable="false"><span itemtype="http://schema.skype.com/Mention" itemscope itemid="%d">%s</span></readonly>`,
			m.ItemID, safeName,
		)
		// 3. Replace the escaped mention with our safe HTML tag
		result = strings.ReplaceAll(result, "@"+safeName, tag)
	}
	content = "<p>" + result + "</p>"

	// Build mentions JSON array (stored as a string in properties)
	type mentionEntry struct {
		Type        string `json:"@type"`
		ItemID      string `json:"itemid"`
		MRI         string `json:"mri"`
		MentionType string `json:"mentionType"`
		DisplayName string `json:"displayName"`
	}
	entries := make([]mentionEntry, len(mentions))
	for i, m := range mentions {
		entries[i] = mentionEntry{
			Type:        "http://schema.skype.com/Mention",
			ItemID:      fmt.Sprintf("%d", m.ItemID),
			MRI:         m.MRI,
			MentionType: "person",
			DisplayName: m.DisplayName,
		}
	}
	b, _ := json.Marshal(entries)
	mentionsJSON = string(b)
	return
}
