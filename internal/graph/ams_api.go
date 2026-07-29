package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const asyncGWBase = "https://us-prod.asyncgw.teams.microsoft.com"

type AMSImage struct {
	ObjectID string
	URL      string
}

func setAMSHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:151.0) Gecko/20100101 Firefox/151.0")
	req.Header.Set("x-ms-client-version", "1415/26071616008")
	req.Header.Set("x-ms-migration", "True")
	req.Header.Set("x-ms-test-user", "False")
	req.Header.Set("Origin", "https://teams.microsoft.com")
	req.Header.Set("Referer", "https://teams.microsoft.com/")
}

func (c *Client) UploadInlineImage(
	ctx context.Context,
	conversationID string,
	data []byte,
	contentType string,
) (*AMSImage, error) {

	// Step 1: create object
	createBody := map[string]any{
		"type": "pish/image",
		"permissions": map[string][]string{
			conversationID: {"read"},
		},
		"sharingMode": "Inline",
		"filename":    "clipboard.png",
	}

	b, _ := json.Marshal(createBody)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		asyncGWBase+"/v1/objects/",
		bytes.NewReader(b),
	)
	if err != nil {
		return nil, err
	}

	setAMSHeaders(req, c.WebToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create object failed: %d %s", resp.StatusCode, string(body))
	}

	var createResp struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return nil, err
	}

	// Step 2: upload content
	uploadURL := fmt.Sprintf(
		"%s/v1/objects/%s/content/imgpsh",
		asyncGWBase,
		createResp.ID,
	)

	req, err = http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		uploadURL,
		bytes.NewReader(data),
	)
	if err != nil {
		return nil, err
	}

	setAMSHeaders(req, c.WebToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err = c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload failed: %d %s", resp.StatusCode, string(body))
	}

	publicURL := fmt.Sprintf(
		"https://us-api.asm.skype.com/v1/objects/%s/views/imgo",
		createResp.ID,
	)

	return &AMSImage{
		ObjectID: createResp.ID,
		URL:      publicURL,
	}, nil
}
