package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// UploadFileToChannel uploads a file to a Teams channel's SharePoint folder.
func (c *Client) UploadFileToChannel(teamID, channelName, filePath string) (DriveItem, error) {
	return c.uploadFile(
		fmt.Sprintf("/groups/%s/drive/root:/%s/%s:/content",
			teamID,
			url.PathEscape(channelName),
			url.PathEscape(filepath.Base(filePath)),
		),
		filePath,
	)
}

// UploadFileToFolder uploads a file to a specific folder (by ID) in the team's drive.
func (c *Client) UploadFileToFolder(teamID, folderID, filePath string) (DriveItem, error) {
	return c.uploadFile(
		fmt.Sprintf("/groups/%s/drive/items/%s:/%s:/content",
			teamID,
			folderID,
			url.PathEscape(filepath.Base(filePath)),
		),
		filePath,
	)
}

// UploadFileToOneDrive uploads a file to the logged-in user's OneDrive.
func (c *Client) UploadFileToOneDrive(filePath string) (DriveItem, error) {
	return c.uploadFile(
		fmt.Sprintf("/me/drive/root:/Teams Uploads/%s:/content",
			url.PathEscape(filepath.Base(filePath)),
		),
		filePath,
	)
}

func (c *Client) uploadFile(endpoint, filePath string) (DriveItem, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return DriveItem{}, fmt.Errorf("cannot open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return DriveItem{}, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return DriveItem{}, err
	}

	if info.Size() > 4*1024*1024 {
		return c.uploadLargeFile(endpoint, data)
	}

	req, err := http.NewRequest("PUT", baseURL+endpoint, bytes.NewReader(data))
	if err != nil {
		return DriveItem{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DriveItem{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(resp.Body)
		return DriveItem{}, fmt.Errorf("upload error %d: %s", resp.StatusCode, string(body))
	}

	var item DriveItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return DriveItem{}, err
	}
	return item, nil
}

func (c *Client) uploadLargeFile(endpoint string, data []byte) (DriveItem, error) {
	sessionURL := baseURL + strings.Replace(endpoint, ":/content", ":/createUploadSession", 1)
	req, err := http.NewRequest("POST", sessionURL, strings.NewReader(`{"item":{"@microsoft.graph.conflictBehavior":"rename"}}`))
	if err != nil {
		return DriveItem{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DriveItem{}, err
	}
	defer resp.Body.Close()

	var session struct {
		UploadURL string `json:"uploadUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return DriveItem{}, err
	}

	chunkSize := 4 * 1024 * 1024
	total := len(data)
	var item DriveItem

	for offset := 0; offset < total; offset += chunkSize {
		end := offset + chunkSize
		if end > total {
			end = total
		}
		chunk := data[offset:end]

		req, err := http.NewRequest("PUT", session.UploadURL, bytes.NewReader(chunk))
		if err != nil {
			return DriveItem{}, err
		}
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end-1, total))
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return DriveItem{}, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 201 || resp.StatusCode == 200 {
			json.Unmarshal(body, &item)
		}
	}
	return item, nil
}
