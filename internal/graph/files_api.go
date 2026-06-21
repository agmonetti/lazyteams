package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type DriveItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	WebUrl      string `json:"webUrl"`
	DownloadUrl string `json:"@microsoft.graph.downloadUrl"`
	Folder      *struct {
		ChildCount int `json:"childCount"`
	} `json:"folder,omitempty"`
	// RemoteItem indica que este DriveItem es en realidad un "shortcut" (acceso directo)
	// a contenido que vive físicamente en otro Drive. Desde fines de 2024, Microsoft
	// cambió "Materiales de clase" (Class Materials) de ser una carpeta real a ser
	// un shortcut de este tipo en muchos tenants educativos.
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

	// Flag interno para la UI (no pertenece al JSON de Microsoft)
	IsExternalLink bool `json:"-"`
}

// GetChannelFiles obtiene la lista de archivos usando la API directa de SharePoint (Drive)
func (c *Client) GetChannelFiles(teamID, channelName string) ([]DriveItem, error) {
	// Bypass del bug "This API is not supported for AAD accounts".
	// En lugar de usar el endpoint de Teams, le pegamos directamente al disco (Drive)
	// del grupo de Office 365. La carpeta del canal tiene el mismo nombre que el canal.
	endpoint := fmt.Sprintf("/groups/%s/drive/root:/%s:/children", teamID, url.PathEscape(channelName))
	body, err := c.doReq(endpoint)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []DriveItem `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parseando archivos: %w", err)
	}

	// PARCHE EDUCACIÓN: En los tenants universitarios, la carpeta "Materiales de clase"
	// es una carpeta especial (hoy día, un shortcut/remoteItem) que Teams inyecta
	// visualmente adentro de "General".
	if strings.ToLower(channelName) == "general" {
		matFolder, err := c.findClassMaterialsFolder(teamID)
		if err == nil && matFolder != nil {
			res.Value = append([]DriveItem{*matFolder}, res.Value...)
		}
	}

	return res.Value, nil
}

// GetFolderChildren permite navegar de forma recursiva (tipo árbol)
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
		return nil, fmt.Errorf("error parseando subcarpeta: %w", err)
	}

	return res.Value, nil
}

// findClassMaterialsFolder localiza la carpeta/shortcut "Materiales de clase"
// (o "Class Materials") buscando el Drive independiente asociado a este Site.
func (c *Client) findClassMaterialsFolder(teamID string) (*DriveItem, error) {
	// 1. Obtener el Site asociado al grupo
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

	// 2. Obtener todas las librerías (drives) de ese Site
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

	// 3. Buscar la librería de Materiales de clase
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
		return nil, errors.New("no se encontró librería de Materiales de clase")
	}

	// 4. Crear un DriveItem sintético con la faceta RemoteItem.
	// La TUI está programada para navegar a otro drive si encuentra un RemoteItem.
	// Ponemos "root" como ID para que el endpoint pida la raíz de esa librería.
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

// ensureFolderFacet garantiza que el ícono de carpeta se muestre en la UI
// incluso si el item viene únicamente con faceta remoteItem.
func ensureFolderFacet(item *DriveItem) {
	if item.Folder == nil {
		item.Folder = &struct {
			ChildCount int `json:"childCount"`
		}{ChildCount: 1}
	}
}

// GetItemChildren lista los hijos de un item en un Drive arbitrario (por driveID).
// Es necesario para navegar dentro de "shortcuts" (remoteItem), como el nuevo
// formato de "Materiales de clase", cuyo contenido real vive en un Drive
// distinto al del grupo/equipo (Team).
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
		return nil, fmt.Errorf("error parseando hijos remotos: %w", err)
	}

	return res.Value, nil
}
