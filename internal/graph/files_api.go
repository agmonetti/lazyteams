package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type DriveItem struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	WebUrl          string `json:"webUrl"`
	DownloadUrl     string `json:"@microsoft.graph.downloadUrl"`
	ParentReference *struct {
		DriveID string `json:"driveId"`
		ID      string `json:"id"`
	} `json:"parentReference,omitempty"`
	Folder *struct {
		ChildCount int `json:"childCount"`
	} `json:"folder,omitempty"`
	// RemoteItem indicates that this DriveItem is actually a "shortcut"
	// to content that lives in a different Drive. Since late 2024, Microsoft
	// changed "Class Materials" from being a real folder to being
	// a shortcut of this type in many educational tenants.
	RemoteItem *struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		ParentReference struct {
			DriveID string `json:"driveId"`
		} `json:"parentReference"`
		Folder *struct {
			ChildCount int `json:"childCount"`
		} `json:"folder,omitempty"`
	} `json:"remoteItem,omitempty"`

	// Metadata
	LastModifiedDateTime string `json:"lastModifiedDateTime"`
	LastModifiedBy       struct {
		User struct {
			DisplayName string `json:"displayName"`
		} `json:"user"`
	} `json:"lastModifiedBy"`
	Size int64 `json:"size"`

	// Internal flag for the UI (not part of Microsoft's JSON)
	IsExternalLink bool `json:"-"`
}

// GetChannelFiles fetches the file list using the direct SharePoint Drive API
func (c *Client) GetChannelFiles(teamID, channelName string) ([]DriveItem, error) {
	// Workaround for the "This API is not supported for AAD accounts" bug.
	// Instead of using the Teams endpoint, we hit the Office 365 group's
	// Drive directly. The channel folder has the same name as the channel.
	endpoint := fmt.Sprintf("/groups/%s/drive/root:/%s:/children", teamID, url.PathEscape(channelName))
	body, err := c.doReq(endpoint)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []DriveItem `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parsing files: %w", err)
	}

	// EDUCATION PATCH: In university tenants, the "Class Materials" folder
	// is a special folder (currently a shortcut/remoteItem) that Teams injects
	// visually inside "General".
	if strings.ToLower(channelName) == "general" {
		matFolder, err := c.findClassMaterialsFolder(teamID)
		if err == nil && matFolder != nil {
			res.Value = append([]DriveItem{*matFolder}, res.Value...)
		}
	}

	return res.Value, nil
}

// GetFolderChildren enables recursive tree-style navigation
func (c *Client) GetFolderChildren(teamID, folderID string) ([]DriveItem, error) {
	endpoint := fmt.Sprintf("/groups/%s/drive/items/%s/children", teamID, folderID)
	body, err := c.doReq(endpoint)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []DriveItem `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parsing subfolder: %w", err)
	}

	return res.Value, nil
}

// findClassMaterialsFolder locates the "Class Materials" folder/shortcut
// by finding the independent Drive associated with this Site.
func (c *Client) findClassMaterialsFolder(teamID string) (*DriveItem, error) {
	// 1. Get the Site associated with the group
	siteEndpoint := fmt.Sprintf("/groups/%s/sites/root", teamID)
	siteBody, err := c.doReq(siteEndpoint)
	if err != nil {
		return nil, err
	}
	var siteRes struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(siteBody, &siteRes); err != nil {
		return nil, err
	}

	// 2. Get all libraries (drives) for that Site
	drivesEndpoint := fmt.Sprintf("/sites/%s/drives", siteRes.ID)
	drivesBody, err := c.doReq(drivesEndpoint)
	if err != nil {
		return nil, err
	}

	var drivesRes struct {
		Value []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := json.Unmarshal(drivesBody, &drivesRes); err != nil {
		return nil, err
	}

	// 3. Find the Class Materials library
	var matDriveID string
	var matName string
	for _, d := range drivesRes.Value {
		if strings.EqualFold(d.Name, "Materiales de clase") || strings.EqualFold(d.Name, "Class Materials") {
			matDriveID = d.ID
			matName = d.Name
			break
		}
	}

	if matDriveID == "" {
		return nil, errors.New("Class Materials library not found")
	}

	// 4. Create a synthetic DriveItem with the RemoteItem facet.
	// The TUI is programmed to navigate to another drive when it finds a RemoteItem.
	// We use "root" as the ID so the endpoint requests the root of that library.
	item := DriveItem{
		ID:   "root",
		Name: matName,
	}
	item.RemoteItem = &struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		ParentReference struct {
			DriveID string `json:"driveId"`
		} `json:"parentReference"`
		Folder *struct {
			ChildCount int `json:"childCount"`
		} `json:"folder,omitempty"`
	}{
		ID:   "root",
		Name: matName,
		ParentReference: struct {
			DriveID string `json:"driveId"`
		}{DriveID: matDriveID},
	}

	ensureFolderFacet(&item)
	return &item, nil
}

// ensureFolderFacet ensures the folder icon is displayed in the UI
// even if the item only comes with the remoteItem facet.
func ensureFolderFacet(item *DriveItem) {
	if item.Folder == nil {
		item.Folder = &struct {
			ChildCount int `json:"childCount"`
		}{ChildCount: 1}
	}
}

// GetItemChildren lists children of an item in an arbitrary Drive (by driveID).
// This is needed to navigate inside "shortcuts" (remoteItems), like the new
// "Class Materials" format whose actual content lives in a Drive
// different from the group/team's Drive.
func (c *Client) GetItemChildren(driveID, itemID string) ([]DriveItem, error) {
	endpoint := fmt.Sprintf("/drives/%s/items/%s/children", driveID, itemID)
	body, err := c.doReq(endpoint)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []DriveItem `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parsing remote children: %w", err)
	}

	return res.Value, nil
}

// DeleteItem deletes a file or folder from a team's drive.
func (c *Client) DeleteItem(teamID, itemID string) error {
	endpoint := fmt.Sprintf("%s/groups/%s/drive/items/%s", baseURL, teamID, itemID)
	req, err := http.NewRequest("DELETE", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// DeleteRemoteItem deletes a file or folder from an arbitrary drive (for remoteItems).
func (c *Client) DeleteRemoteItem(driveID, itemID string) error {
	endpoint := fmt.Sprintf("%s/drives/%s/items/%s", baseURL, driveID, itemID)
	req, err := http.NewRequest("DELETE", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetChannelFilesFolder returns the channel's shared folder (filesFolder) via
// the Graph endpoint. For private channels this resolves to the channel's own
// SharePoint drive (parentReference.driveId), which is where its files live.
func (c *Client) GetChannelFilesFolder(teamID, channelID string) (*DriveItem, error) {
	endpoint := fmt.Sprintf("/teams/%s/channels/%s/filesFolder", teamID, channelID)
	body, err := c.doReq(endpoint)
	if err != nil {
		return nil, err
	}
	var item DriveItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("error parsing channel files folder: %w", err)
	}
	return &item, nil
}

// GetChannelFolder gets the channel folder metadata (ID, name) from the drive.
// GET /groups/{teamID}/drive/root:/{channelName}
func (c *Client) GetChannelFolder(teamID, channelID, channelName string, isPrivate bool) (*DriveItem, error) {
	if isPrivate {
		item, err := c.GetChannelFilesFolder(teamID, channelID)
		if err != nil {
			return nil, err
		}
		if item.Name == "" {
			item.Name = channelName
		}
		return item, nil
	}
	endpoint := fmt.Sprintf("/groups/%s/drive/root:/%s", teamID, url.PathEscape(channelName))
	body, err := c.doReq(endpoint)
	if err != nil {
		return nil, err
	}
	var item DriveItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("error parsing channel folder: %w", err)
	}
	return &item, nil
}

// CreateFolder creates a new folder inside a team's drive channel folder.
func (c *Client) CreateFolder(teamID, parentID, folderName string) (DriveItem, error) {
	endpoint := fmt.Sprintf("%s/groups/%s/drive/items/%s/children", baseURL, teamID, parentID)
	payload := map[string]any{
		"name":                              folderName,
		"folder":                            map[string]any{},
		"@microsoft.graph.conflictBehavior": "rename",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return DriveItem{}, err
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
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
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return DriveItem{}, fmt.Errorf("create folder failed (%d): %s", resp.StatusCode, string(b))
	}
	var item DriveItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return DriveItem{}, err
	}
	return item, nil
}
