package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var stripTags = regexp.MustCompile(`<[^>]*>`)
var multipleSpaces = regexp.MustCompile(`[ \t]+`)

func cleanHTML(content string) string {
	// 1. Quitar saltos de línea y tabulaciones literales del código HTML
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\r", " ")
	content = strings.ReplaceAll(content, "\t", " ")

	// 2. Definir puntos de quiebre (bloques visuales)
	content = strings.ReplaceAll(content, "</tr>", "\n")
	content = strings.ReplaceAll(content, "</td>", " ")
	content = strings.ReplaceAll(content, "</li>", "\n")
	content = strings.ReplaceAll(content, "<li>", "• ")
	content = strings.ReplaceAll(content, "<br>", "\n")
	content = strings.ReplaceAll(content, "<br/>", "\n")
	content = strings.ReplaceAll(content, "<br />", "\n")
	content = strings.ReplaceAll(content, "</p>", "\n")
	content = strings.ReplaceAll(content, "</div>", "\n")

	// 3. Borrar tags restantes
	content = stripTags.ReplaceAllString(content, "")

	// 4. Decodificar entidades HTML (convierte &quot; en ")
	content = html.UnescapeString(content)

	// 5. Apretar espacios vacíos horizontales
	content = multipleSpaces.ReplaceAllString(content, " ")

	// 6. Limpiar renglones vacíos y espacios al inicio/fin de cada línea
	lines := strings.Split(content, "\n")
	var out []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			// Evitamos inyectar renglones en blanco que generan grandes gaps
			out = append(out, trimmed)
		}
	}

	// Al unir con \n, garantizamos que quede un solo salto de línea (todos seguidos)
	// Si queremos mantener párrafos dobles, esto lo junta todo. 
	// Para listas y tablas es perfecto.
	return strings.Join(out, "\n")
}

// Structs de datos
type Team struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Channel struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Chat struct {
	ID                 string              `json:"id"`
	Topic              string              `json:"topic"`
	ChatType           string              `json:"chatType"`
	Members            []ChatMember        `json:"members"`
	LastMessagePreview *ChatMessagePreview `json:"lastMessagePreview,omitempty"`
}

type ChatMessagePreview struct {
	From *struct {
		User *struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	} `json:"from"`
}

type ChatMember struct {
	DisplayName string `json:"displayName"`
	UserID      string `json:"userId"`
}

// DisplayName resuelve el nombre a mostrar de un chat. Los chats grupales
// suelen tener "topic"; los 1:1 no, así que ahí usamos el nombre del otro
// participante (excluyendo al usuario logueado, identificado por selfID).
func (ch Chat) DisplayName(selfID string) string {
	if strings.TrimSpace(ch.Topic) != "" {
		return ch.Topic
	}

	// Detección exacta e infalible del chat "Notas personales (Vos)".
	// En Teams, este chat siempre tiene un ID formado por el ID del usuario repetido.
	expectedSelfChatID := fmt.Sprintf("19:%s_%s@unq.gbl.spaces", selfID, selfID)
	if ch.ID == expectedSelfChatID {
		return "Notas personales (Vos)"
	}

	var names []string
	for _, m := range ch.Members {
		if m.UserID != selfID && strings.TrimSpace(m.DisplayName) != "" {
			names = append(names, m.DisplayName)
		}
	}
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}

	// Fallback para chats históricos o huerfanos donde Graph API omite
	// al otro miembro en la respuesta. Intentamos extraer el nombre
	// de la persona que envió el último mensaje (si no fuimos nosotros).
	if ch.LastMessagePreview != nil && ch.LastMessagePreview.From != nil && ch.LastMessagePreview.From.User != nil {
		u := ch.LastMessagePreview.From.User
		if u.ID != selfID && strings.TrimSpace(u.DisplayName) != "" {
			return strings.TrimSpace(u.DisplayName) + " (Histórico)"
		}
	}

	if ch.ChatType == "oneOnOne" {
		return "Chat 1:1 (Histórico)"
	}
	if ch.ChatType != "" {
		return fmt.Sprintf("Chat sin nombre (%s)", ch.ChatType)
	}
	return "Chat sin nombre"
}

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
}

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
}

const baseURL = "https://graph.microsoft.com/v1.0"

type Client struct {
	GraphToken string
	WebToken   string
	HTTPClient *http.Client
}

func NewClient(graphToken, webToken string) *Client {
	return &Client{
		GraphToken: graphToken,
		WebToken:   webToken,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) doReq(endpoint string) ([]byte, error) {
	req, err := http.NewRequest("GET", baseURL+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creando request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.GraphToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de red: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo respuesta: %w", err)
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

// GetJoinedTeams obtiene los equipos de los que el usuario es miembro.
func (c *Client) GetJoinedTeams() ([]Team, error) {
	body, err := c.doReq("/me/joinedTeams")
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []Team `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parseando equipos: %w", err)
	}

	return res.Value, nil
}

// GetMe obtiene el ID del usuario logueado, necesario para resolver
// el nombre del interlocutor en chats 1:1.
func (c *Client) GetMe() (string, error) {
	body, err := c.doReq("/me")
	if err != nil {
		return "", err
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		return "", fmt.Errorf("error parseando /me: %w", err)
	}
	return me.ID, nil
}

// GetChats lista los chats personales (1:1 y grupales) del usuario.
func (c *Client) GetChats() ([]Chat, error) {
	body, err := c.doReq("/me/chats?$expand=members&$top=50")
	if err != nil {
		return nil, err
	}
	var res struct {
		Value []Chat `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parseando chats: %w", err)
	}
	return res.Value, nil
}

// GetJoinedTeamsRaw obtiene el JSON crudo (útil para la prueba rápida).
func (c *Client) GetJoinedTeamsRaw() ([]byte, error) {
	return c.doReq("/me/joinedTeams")
}

// GetChannels obtiene los canales de un equipo específico.
func (c *Client) GetChannels(teamID string) ([]Channel, error) {
	endpoint := fmt.Sprintf("/teams/%s/channels", teamID)
	body, err := c.doReq(endpoint)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []Channel `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parseando canales: %w", err)
	}

	return res.Value, nil
}

// GetMessages obtiene los mensajes de un canal usando la API interna (ChatSvc)
func (c *Client) GetMessages(teamID, channelID string) ([]Message, error) {
	// El channelID que nos da Graph ya tiene el formato interno de Skype (ej: 19:xxx@thread.tacv2)
	url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages?view=msnp24Equivalent|supportsMessageProperties&pageSize=200&startTime=1", channelID)
	
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chatsvc error %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo respuesta: %w", err)
	}

	var res chatSvcResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("error parseando mensajes: %w", err)
	}

	var msgs []Message
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
					url, _ := l["url"].(string)
					if url != "" {
						title := url
						if preview, ok := l["preview"].(map[string]interface{}); ok {
							if t, ok := preview["title"].(string); ok && t != "" {
								title = t
							}
						}
						// Limpiar saltos de línea basura en el título del link
						title = strings.ReplaceAll(title, "\n", "")
						title = strings.ReplaceAll(title, "\r", "")
						title = strings.TrimSpace(title)

						attachments = append(attachments, Attachment{
							Name: title,
							URL:  url,
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
					name := "Archivo adjunto"
					if n, ok := f["fileName"].(string); ok && n != "" {
						name = n
					} else if n, ok := f["name"].(string); ok && n != "" {
						name = n
					} else if n, ok := f["title"].(string); ok && n != "" {
						name = n
					}

					url := ""
					if u, ok := f["fileUrl"].(string); ok {
						url = u
					} else if u, ok := f["url"].(string); ok {
						url = u
					}

					if url != "" {
						attachments = append(attachments, Attachment{
							Name: name,
							URL:  url,
							Type: "file",
						})
					}
				}
			}
		}
		
		msgs = append(msgs, Message{
			ID:          m.ID,
			Body:        cleanHTML(m.Content),
			FromName:    name,
			CreatedAt:   t,
			MessageType: m.MessageType,
			Attachments: attachments,
		})
	}

	return msgs, nil
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
