package graph

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (c *Client) DownloadItem(teamID, itemID string) (io.ReadCloser, error) {
	endpoint := fmt.Sprintf("https://graph.microsoft.com/v1.0/groups/%s/drive/items/%s/content", teamID, itemID)
	return c.downloadContent(endpoint)
}

func (c *Client) DownloadRemoteItem(driveID, itemID string) (io.ReadCloser, error) {
	endpoint := fmt.Sprintf("https://graph.microsoft.com/v1.0/drives/%s/items/%s/content", driveID, itemID)
	return c.downloadContent(endpoint)
}

// DownloadGraphItem downloads a file using a full Graph item URL.
func (c *Client) DownloadGraphItem(itemURL string) (io.ReadCloser, error) {
	return c.downloadContent(itemURL + "/content")
}

// encodeShareURL encodes a URL into the "shareId" format that Graph expects
// for the /shares/{id}/driveItem endpoint.
func encodeShareURL(rawURL string) string {
	lower := strings.ToLower(rawURL)
	encoded := base64.URLEncoding.EncodeToString([]byte(lower))
	encoded = strings.TrimRight(encoded, "=")
	return "u!" + encoded
}

// ResolveSharedItem resolves a shared link (WebUrl from a DM file)
// into the real DriveItem with ID and driveId. If the link doesn't point
// to a SharePoint/OneDrive file, it returns an error.
func (c *Client) ResolveSharedItem(shareURL string) (*DriveItem, error) {
	shareId := encodeShareURL(shareURL)
	endpoint := fmt.Sprintf("https://graph.microsoft.com/v1.0/shares/%s/driveItem", shareId)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not resolve link (status %d)", resp.StatusCode)
	}

	var item DriveItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (c *Client) downloadContent(endpoint string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error during download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error downloading file (status %d): %s", resp.StatusCode, string(body))
	}
	return resp.Body, nil
}
