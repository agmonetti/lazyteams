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

// encodeShareURL codifica una URL en el formato "shareId" que Graph espera
// para el endpoint /shares/{id}/driveItem.
func encodeShareURL(rawURL string) string {
	lower := strings.ToLower(rawURL)
	encoded := base64.URLEncoding.EncodeToString([]byte(lower))
	encoded = strings.TrimRight(encoded, "=")
	return "u!" + encoded
}

// ResolveSharedItem resuelve un link de compartido (WebUrl de un archivo de DM)
// en el DriveItem real con ID y driveId. Si el link no apunta a un archivo
// de SharePoint/OneDrive, devuelve error.
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
		return nil, fmt.Errorf("no se pudo resolver el link (status %d)", resp.StatusCode)
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
		return nil, fmt.Errorf("error de red al descargar: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error descargando archivo (status %d): %s", resp.StatusCode, string(body))
	}
	return resp.Body, nil
}
