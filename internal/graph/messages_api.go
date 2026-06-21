package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Attachment struct {
	Name string
	URL  string
	Type string // "file" o "link"
}

type Message struct {
	ID          string       `json:"id"`
	Body        string       `json:"body"`
	FromName    string       `json:"fromName"`
	CreatedAt   time.Time    `json:"createdAt"`
	MessageType string       `json:"messageType"`
	Attachments []Attachment `json:"attachments"`
}

// Estructuras internas para la respuesta de ChatSvc (Teams Web)
type chatSvcResponse struct {
	Messages []struct {
		ID                  string                 `json:"id"`
		Type                string                 `json:"type"`
		MessageType         string                 `json:"messagetype"`
		Content             string                 `json:"content"`
		ImDisplayName       string                 `json:"imdisplayname"`
		OriginalArrivalTime string                 `json:"originalarrivaltime"`
		Properties          map[string]interface{} `json:"properties"`
	} `json:"messages"`
	Metadata struct {
		BackwardLink string `json:"backwardLink"`
	} `json:"_metadata"`
}

// GetMessages obtiene los mensajes de un canal usando la API interna (ChatSvc)
func (c *Client) GetMessages(teamID, channelID string, pageSize int) ([]Message, error) {
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

	for page := 0; page < maxPages && len(allMsgs) < pageSize; page++ {
		req, err := http.NewRequest(http.MethodGet, urlStr, nil)
		if err != nil {
			return allMsgs, err
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
			return allMsgs, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return allMsgs, &ChatSvcError{StatusCode: resp.StatusCode, Message: string(body)}
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return allMsgs, fmt.Errorf("error leyendo respuesta: %w", err)
		}

		var res chatSvcResponse
		if err := json.Unmarshal(body, &res); err != nil {
			return allMsgs, fmt.Errorf("error parseando mensajes: %w", err)
		}

		for _, m := range res.Messages {
			t, _ := time.Parse(time.RFC3339, m.OriginalArrivalTime)
			name := m.ImDisplayName
			if name == "" {
				name = "Usuario"
			}

			var attachments []Attachment

			// Extraer links de OneDrive/SharePoint u otros adjuntos
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

			// Extraer archivos directos
			if filesStr, ok := m.Properties["files"].(string); ok && filesStr != "[]" && filesStr != "" {
				var files []map[string]interface{}
				if json.Unmarshal([]byte(filesStr), &files) == nil {
					for _, f := range files {
						fname := "Archivo adjunto"
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

			allMsgs = append(allMsgs, Message{
				ID:          m.ID,
				Body:        cleanHTML(m.Content),
				FromName:    name,
				CreatedAt:   t,
				MessageType: m.MessageType,
				Attachments: attachments,
			})
		}

		if len(res.Messages) == 0 {
			break
		}
		if res.Metadata.BackwardLink == "" {
			break
		}
		urlStr = res.Metadata.BackwardLink
	}

	if len(allMsgs) > pageSize {
		allMsgs = allMsgs[:pageSize]
	}

	return allMsgs, nil
}

// SendMessage envía un mensaje de texto al canal especificado usando la API interna
func (c *Client) SendMessage(channelID, content string) error {
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages", channelID)

	payload := map[string]string{
		"content":     content,
		"messagetype": "RichText/Html",
		"contenttype": "text",
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
