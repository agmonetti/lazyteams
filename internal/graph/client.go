package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
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

	// --- PARCHE DE DEBUG ---
	// Volcamos el JSON crudo al disco duro para analizarlo
	os.WriteFile("debug_mensajes.json", body, 0644)
	// -----------------------

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
	// es una carpeta especial de solo lectura que Teams inyecta visualmente adentro de "General".
	if strings.ToLower(channelName) == "general" {
		// Fix: Usamos url.PathEscape para los espacios
		matEndpoint := fmt.Sprintf("/groups/%s/drive/root:/%s", teamID, url.PathEscape("Materiales de clase"))
		matBody, err := c.doReq(matEndpoint)
		if err == nil {
			var matFolder DriveItem
			if json.Unmarshal(matBody, &matFolder) == nil && matFolder.ID != "" {
				if matFolder.Folder == nil {
					matFolder.Folder = &struct {
						ChildCount int `json:"childCount"`
					}{ChildCount: 1}
				}
				res.Value = append([]DriveItem{matFolder}, res.Value...)
			}
		} else {
			// Fallback: inglés
			matEndpointEN := fmt.Sprintf("/groups/%s/drive/root:/%s", teamID, url.PathEscape("Class Materials"))
			matBodyEN, err := c.doReq(matEndpointEN)
			if err == nil {
				var matFolder DriveItem
				if json.Unmarshal(matBodyEN, &matFolder) == nil && matFolder.ID != "" {
					if matFolder.Folder == nil {
						matFolder.Folder = &struct {
							ChildCount int `json:"childCount"`
						}{ChildCount: 1}
					}
					res.Value = append([]DriveItem{matFolder}, res.Value...)
				}
			}
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
