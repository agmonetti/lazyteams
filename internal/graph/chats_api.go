package graph

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Chat struct {
	ID                     string              `json:"id"`
	Topic                  string              `json:"topic"`
	ChatType               string              `json:"chatType"`
	Members                []ChatMember        `json:"members"`
	LastMessagePreview     *ChatMessagePreview `json:"lastMessagePreview,omitempty"`
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

// DiscoverSelfChatID realiza fuerza bruta contra ChatSvc para encontrar
// el identificador real (MRI) del chat "Notas personales" del usuario.
func (c *Client) DiscoverSelfChatID(selfID string) string {
	candidates := []string{
		fmt.Sprintf("8:orgid:%s", selfID),
		"48:notes",
		fmt.Sprintf("8:%s", selfID),
		fmt.Sprintf("19:%s_%s@unq.gbl.spaces", selfID, selfID),
	}

	for _, id := range candidates {
		endpoint := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/%s/messages?pageSize=1", id)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}

		req.Header.Set("Authorization", "Bearer "+c.WebToken)
		req.Header.Set("behavioroverride", "redirectAs404")

		resp, err := c.HTTPClient.Do(req)
		if err == nil {
			resp.Body.Close()
			// Si el servidor responde 200 OK, encontramos el ID correcto.
			if resp.StatusCode == http.StatusOK {
				return id
			}
		}
	}
	return ""
}
