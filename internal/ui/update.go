package ui

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"teamsTUI/internal/graph"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Comandos

const pollInterval = 15       // segundos entre polls de DMs
const presenceInterval = 60   // segundos entre polls de presencia

type tickMsg struct{}
type presenceTickMsg struct{}
type messageSentMsg struct{}
type messageSendErrMsg struct{ err error }
type filesMsg struct{ files []graph.DriveItem }
type filesErrMsg struct{ err error }
type chatsMsg struct{ chats []graph.Chat }
type chatsErrMsg struct{ err error }
type meMsg struct{ id string }
type meErrMsg struct{ err error }
type selfChatDiscoveredMsg struct {
	id              string
	newlyDiscovered bool
}

func refreshTickCmd() tea.Cmd {
	return tea.Tick(pollInterval*time.Second, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func refreshPresenceTickCmd() tea.Cmd {
	return tea.Tick(presenceInterval*time.Second, func(t time.Time) tea.Msg {
		return presenceTickMsg{}
	})
}

func initialPresenceTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return presenceTickMsg{}
	})
}

func loadMeCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		id, err := client.GetMe()
		if err != nil {
			return meErrMsg{err}
		}
		return meMsg{id}
	}
}

func loadChatsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		chats, err := client.GetChats()
		if err != nil {
			return chatsErrMsg{err}
		}
		return chatsMsg{chats}
	}
}

type pollChatsMsg struct {
	chats []graph.Chat
}

func pollChatsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		chats, err := client.GetChats()
		if err != nil {
			return nil // falla silenciosa en poll, no rompemos la UI
		}
		return pollChatsMsg{chats}
	}
}

type presenceTickResultMsg struct {
	presences map[string]string
}

func pollPresenceCmd(client *graph.Client, userIDs []string) tea.Cmd {
	return func() tea.Msg {
		if len(userIDs) == 0 {
			return nil
		}
		presences, err := client.GetPresences(userIDs)
		if err != nil {
			return nil // falla silenciosa
		}
		return presenceTickResultMsg{presences}
	}
}

func discoverSelfChatCmd(client *graph.Client, selfID, cachedID string) tea.Cmd {
	return func() tea.Msg {
		if selfID == "" {
			return nil
		}
		
		// 1. Si tenemos caché, bypass total de la red
		if cachedID != "" {
			return selfChatDiscoveredMsg{id: cachedID, newlyDiscovered: false}
		}

		// 2. Sin caché, toca hacer fuerza bruta a la API
		id := client.DiscoverSelfChatID(selfID)
		if id != "" {
			return selfChatDiscoveredMsg{id: id, newlyDiscovered: true}
		}
		
		return nil // Si fallan todos los formatos, fallamos silenciosamente
	}
}

func loadTeamsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		teams, err := client.GetJoinedTeams()
		if err != nil {
			return errMsg{err}
		}
		return teamsMsg{teams}
	}
}

func loadChannelsCmd(client *graph.Client, teamID string) tea.Cmd {
	return func() tea.Msg {
		channels, err := client.GetChannels(teamID)
		if err != nil {
			return channelsErrMsg{teamID: teamID, err: err}
		}
		return channelsMsg{teamID: teamID, channels: channels}
	}
}

func loadMessagesCmd(client *graph.Client, teamID, channelID string, pageSize int) tea.Cmd {
	return func() tea.Msg {
		msgs, err := client.GetMessages(teamID, channelID, pageSize)
		if err != nil {
			return messagesErrMsg{err: err, conversationID: channelID, partialMsgs: msgs}
		}
		
		return messagesMsg{msgs}
	}
}

func sendMessageCmd(client *graph.Client, channelID, content string) tea.Cmd {
	return func() tea.Msg {
		err := client.SendMessage(channelID, content)
		if err != nil {
			return messageSendErrMsg{err}
		}
		return messageSentMsg{}
	}
}

func loadFilesCmd(client *graph.Client, teamID, channelName string) tea.Cmd {
	return func() tea.Msg {
		files, err := client.GetChannelFiles(teamID, channelName)
		if err != nil {
			return filesErrMsg{err}
		}
		return filesMsg{files}
	}
}

func loadFolderCmd(client *graph.Client, teamID string, node FolderNode) tea.Cmd {
	return func() tea.Msg {
		var (
			files []graph.DriveItem
			err   error
		)
		if node.DriveID != "" {
			// El contenido vive en otro Drive (shortcut/remoteItem)
			files, err = client.GetItemChildren(node.DriveID, node.ID)
		} else {
			files, err = client.GetFolderChildren(teamID, node.ID)
		}
		if err != nil {
			return filesErrMsg{err}
		}
		return filesMsg{files}
	}
}

// formatMessages convierte la lista de mensajes en un string renderizable para el viewport
func formatMessages(messages []graph.Message) string {
	var content string
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]

		timeStr := msg.CreatedAt.Local().Format("02/01 15:04")
		formattedTime := metaStyle.Render(fmt.Sprintf("[%s]", timeStr))

		sender := msg.FromName
		if sender == "" {
			sender = "Usuario"
		}

		switch msg.MessageType {
		case "Text", "RichText/Html":
			var attachmentsStr string
			for _, att := range msg.Attachments {
				icon := "[Link]"
				if att.Type == "file" {
					icon = "[Archivo]"
				}
				linkStr := makeClickableLink(att.Name, att.URL)
				attachmentsStr += fmt.Sprintf("  %s %s\n", systemEventStyle.Render(icon), linkStr)
			}

			if msg.Body != "" || attachmentsStr != "" {
				body := msg.Body
				if body != "" && attachmentsStr != "" {
					body += "\n\n"
				}
				body += attachmentsStr

				content += fmt.Sprintf("%s %s:\n%s\n\n",
					formattedTime,
					selectedItemStyle.Render(sender),
					body)
			}

		case "Event/Call":
			content += fmt.Sprintf("%s %s\n\n",
				formattedTime,
				systemEventStyle.Render("[Reunion / Llamada del Sistema]"))

		case "ThreadActivity/AddMember", "ThreadActivity/MemberAdded", "ThreadActivity/DeleteMember", "ThreadActivity/MemberRemoved":
			continue

		default:
			continue
		}
	}
	return content
}

var urlRegex = regexp.MustCompile(`https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*)`)

// aggregateChatAttachments junta todos los adjuntos (archivos y links) de los
// mensajes ya cargados en memoria, deduplicados por URL, del más reciente al
// más viejo. No pega a la red: opera 100% sobre m.messages.
func aggregateChatAttachments(messages []graph.Message) []graph.DriveItem {
	type entry struct {
		item    graph.DriveItem
		created time.Time
	}

	seen := make(map[string]bool)
	var entries []entry

	for _, msg := range messages {
		// 1. Extraer archivos y links del array oficial de Attachments
		for _, att := range msg.Attachments {
			if att.URL == "" || seen[att.URL] {
				continue
			}
			seen[att.URL] = true
			entries = append(entries, entry{
				item: graph.DriveItem{
					Name:           att.Name,
					WebUrl:         att.URL,
					IsExternalLink: att.Type == "link",
				},
				created: msg.CreatedAt,
			})
		}

		// 2. Extraer URLs pegadas a mano en el cuerpo del texto
		urls := urlRegex.FindAllString(msg.Body, -1)
		for _, u := range urls {
			if seen[u] {
				continue
			}
			seen[u] = true

			// Intentar deducir un nombre amigable
			name := u
			if parsed, err := url.Parse(u); err == nil {
				parts := strings.Split(parsed.Path, "/")
				if len(parts) > 0 && parts[len(parts)-1] != "" {
					name = parts[len(parts)-1]
				} else {
					name = parsed.Host
				}
			}

			entries = append(entries, entry{
				item: graph.DriveItem{
					Name:           name,
					WebUrl:         u,
					IsExternalLink: true, // Lo forzamos como link estético
				},
				created: msg.CreatedAt,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].created.After(entries[j].created)
	})

	items := make([]graph.DriveItem, len(entries))
	for i, e := range entries {
		items[i] = e.item
	}
	return items
}

func getFileIcon(item graph.DriveItem) string {
	if item.IsExternalLink {
		return "[LINK]"
	}
	if item.Folder != nil {
		return "[DIR]"
	}
	name := strings.ToLower(item.Name)
	switch {
	case strings.HasSuffix(name, ".pdf"):
		return "[PDF]"
	case strings.HasSuffix(name, ".pptx") || strings.HasSuffix(name, ".ppt"):
		return "[PPT]"
	case strings.HasSuffix(name, ".docx") || strings.HasSuffix(name, ".doc"):
		return "[DOC]"
	case strings.HasSuffix(name, ".xlsx") || strings.HasSuffix(name, ".xls"):
		return "[XLS]"
	case strings.HasSuffix(name, ".mp4") || strings.HasSuffix(name, ".mkv"):
		return "[VID]"
	case strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".rar"):
		return "[ZIP]"
	default:
		return "[FILE]"
	}
}

// Update
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	// --- MODO INSERT (TEXT INPUT) ---
	if m.isTyping {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc": // Salir del modo insert
				m.isTyping = false
				m.input.Blur()
				return m, nil
			case "enter": // Enviar mensaje
				v := m.input.Value()
				if v != "" && m.activeConversationID() != "" {
					m.input.Reset()
					m.isTyping = false
					m.input.Blur()
					m.loading = true
					// Enviamos y agregamos el comando
					return m, sendMessageCmd(m.client, m.activeConversationID(), v)
				}
			}
		}
		
		// Pasamos todas las demás teclas al input
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}
	// --------------------------------

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Cálculo exacto para que NO desborde el panel derecho
		vpWidth := (m.width * 2 / 3) - 6
		vpHeight := (m.height - 4) - 5 // Restamos 1 extra para la caja de texto

		// Ajustar tamaño del textinput
		m.input.Width = vpWidth - 2

		// Cálculo para el panel izquierdo
		leftWidth := (m.width / 3) - 4
		leftHeight := m.height - 6 

		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.leftVp = viewport.New(leftWidth, leftHeight)
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
			m.leftVp.Width = leftWidth
			m.leftVp.Height = leftHeight
		}

	case errMsg:
		m.err = msg.err
		m.loading = false
		return m, nil

	case channelsErrMsg:
		if msg.teamID == m.teams[m.selectedTeam].ID {
			m.channelErr = msg.err
			m.channels = nil
			m.messages = nil
			m.loading = false
		}
		return m, nil

	case messagesErrMsg:
		var chatsvcErr *graph.ChatSvcError
		if m.selfID != "" && msg.conversationID == m.prefs.SelfChatIDs[m.selfID] && errors.As(msg.err, &chatsvcErr) && chatsvcErr.StatusCode == 404 {
			// El acceso directo a notas personales falló con 404. 
			// Probablemente el MRI cambió o el caché quedó sucio.
			// 1. Borramos el caché
			delete(m.prefs.SelfChatIDs, m.selfID)
			savePrefs(m.prefs)

			// 2. Avisamos en la UI
			m.viewport.SetContent("El identificador del chat expiró. Auto-reparando acceso...")
			
			// 3. Relanzamos el descubrimiento forzando red (pasando string vacío)
			return m, discoverSelfChatCmd(m.client, m.selfID, "")
		}

		// Carga parcial: si hay mensajes previos al fallo, los mostramos con aviso
		if len(msg.partialMsgs) > 0 {
			m.messages = msg.partialMsgs
			m.loading = false
			if m.viewMode == ModeFiles {
				m.files = aggregateChatAttachments(m.messages)
				m.selectedFile = 0
				m.viewport.SetContent(renderFilesContent(&m) + "\n\n(carga parcial por error de red)")
			} else {
				m.viewport.SetContent(formatMessages(m.messages) + "\n\n(carga parcial por error de red)")
			}
			return m, nil
		}

		m.viewport.SetContent(fmt.Sprintf("Error cargando mensajes: %v", msg.err))
		m.loading = false
		return m, nil

	case messageSentMsg:
		// Mensaje enviado correctamente. Recargamos los mensajes del canal/chat
		return m, loadMessagesCmd(m.client, "", m.activeConversationID(), 200)

	case messageSendErrMsg:
		m.viewport.SetContent(fmt.Sprintf("Error enviando mensaje: %v", msg.err))
		m.loading = false
		return m, nil

	case filesErrMsg:
		m.viewport.SetContent(fmt.Sprintf("Error cargando archivos: %v", msg.err))
		m.loading = false
		return m, nil

	case filesMsg:
		m.files = msg.files
		m.loading = false
		m.selectedFile = 0

		m.viewport.SetContent(renderFilesContent(&m))
		m.viewport.GotoTop()
		return m, nil

	case teamsMsg:
		m.teams = msg.teams
		m.teamsLoaded = true
		m.loading = false
		if len(m.teams) > 0 {
			m.loading = true
			return m, loadChannelsCmd(m.client, m.teams[0].ID)
		}
		return m, nil

	case channelsMsg:
		if msg.teamID != m.teams[m.selectedTeam].ID {
			return m, nil // stale response, discard
		}
		m.channels = msg.channels
		m.selectedChan = 0
		m.messages = nil
		m.channelErr = nil
		m.loading = false
		return m, nil

	case meMsg:
		m.selfID = msg.id
		return m, nil

	case meErrMsg:
		// No bloqueamos la app: sin selfID, los chats 1:1 simplemente
		// van a mostrar todos los participantes en vez de excluirme a mí.
		return m, nil

	case chatsErrMsg:
		m.err = msg.err
		m.loading = false
		return m, nil

	case chatsMsg:
		m.chats = msg.chats
		m.chatsLoaded = true
		m.loading = false
		m.selectedChat = 0
		
		// Encadenamos el autodescubrimiento asíncrono pasándole el caché si existe
		if m.selfID != "" {
			cachedID := m.prefs.SelfChatIDs[m.selfID]
			return m, discoverSelfChatCmd(m.client, m.selfID, cachedID)
		}
		return m, nil

	case selfChatDiscoveredMsg:
		// Si lo descubrió recién ahora por fuerza bruta, lo guardamos en disco
		if msg.newlyDiscovered {
			m.prefs.SelfChatIDs[m.selfID] = msg.id
			savePrefs(m.prefs)
		}

		// Buscar si ya existe el chat sintético en la lista
		found := false
		for i, ch := range m.chats {
			if ch.Topic == "Notas personales (Vos)" {
				oldID := ch.ID
				m.chats[i].ID = msg.id // Actualización en caliente
				found = true

				// Si el usuario estaba intentando leer este chat y falló, auto-recuperamos
				if m.loadedConvID == oldID {
					m.loadedConvID = msg.id
					m.loading = true
					return m, loadMessagesCmd(m.client, "", msg.id, 200)
				}
				break
			}
		}

		// Si no existía (primera carga), lo insertamos al principio
		if !found {
			selfChat := graph.Chat{
				ID:       msg.id,
				Topic:    "Notas personales (Vos)",
				ChatType: "oneOnOne",
			}
			m.chats = append([]graph.Chat{selfChat}, m.chats...)
			if m.selectedChat > 0 {
				m.selectedChat++ // Mantener la selección visual donde estaba
			}
		}
		return m, nil

	case messagesMsg:
		m.messages = msg.messages
		m.loading = false

		if m.viewMode == ModeChat {
			content := formatMessages(m.messages)
			m.viewport.SetContent(content)
			m.viewport.GotoBottom()
		} else if m.viewMode == ModeFiles {
			m.files = aggregateChatAttachments(m.messages)
			m.selectedFile = 0
			m.viewport.SetContent(renderFilesContent(&m))
			m.viewport.GotoTop()
		}
		return m, nil

	case tickMsg:
		// Poll: refrescar chats si estamos en DMs y la conversación cargada sigue abierta
		cmds = append(cmds, pollChatsCmd(m.client))
		// Refrescar mensajes si hay una conversación abierta y el usuario no está escribiendo
		if m.loadedConvID != "" && m.viewMode == ModeChat && !m.isTyping && !m.focusLeft {
			cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
		}
		// Re-programar el próximo tick
		cmds = append(cmds, refreshTickCmd())
		return m, tea.Batch(cmds...)

	case presenceTickMsg:
		// Poll presencia: recolectar userIds de chats visibles
		if m.workspace == WorkspaceDMs && len(m.chats) > 0 {
			seen := make(map[string]struct{})
			var ids []string
			for _, ch := range m.chats {
				for _, u := range ch.Members {
					if u.UserID != m.selfID && u.UserID != "" {
						if _, ok := seen[u.UserID]; !ok {
							seen[u.UserID] = struct{}{}
							ids = append(ids, u.UserID)
						}
					}
				}
			}
			if len(ids) > 0 {
				cmds = append(cmds, pollPresenceCmd(m.client, ids))
			}
		}
		cmds = append(cmds, refreshPresenceTickCmd())
		return m, tea.Batch(cmds...)

	case presenceTickResultMsg:
		for k, v := range msg.presences {
			m.presence[k] = v
		}
		return m, nil

	case pollChatsMsg:
		// Actualizar la lista de chats con los datos frescos
		// (sin badge: lastModifiedDateTime no está disponible en este tenant)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "tab":
			m.focusLeft = !m.focusLeft

		case "1":
			m.workspace = WorkspaceTeams
			m.focusLeft = true
			m.focusList = 0

		case "2":
			m.workspace = WorkspaceDMs
			m.focusLeft = true
			if !m.chatsLoaded {
				m.loading = true
				cmds = append(cmds, loadChatsCmd(m.client))
			}

		case "esc":
			if !m.focusLeft {
				m.focusLeft = true
			}

		case "up", "k":
			if m.focusLeft {
				if m.workspace == WorkspaceDMs {
					if len(m.chats) > 0 && m.selectedChat > 0 {
						m.selectedChat--
						if m.viewMode == ModeChat {
							m.loading = true
							m.loadedConvID = m.chats[m.selectedChat].ID
							cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
						} else if m.viewMode == ModeFiles {
							m.loadedConvID = m.chats[m.selectedChat].ID
							m.loading = true
							cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
						}
					}
				} else if m.focusList == 0 && len(m.teams) > 0 {
					if m.selectedTeam > 0 {
						m.selectedTeam--
						m.loading = true
						cmds = append(cmds, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID))
					}
				} else if m.focusList == 1 && len(m.channels) > 0 {
					if m.selectedChan > 0 {
						m.selectedChan--
					}
				}
			} else {
				if m.viewMode == ModeFiles {
					if m.selectedFile > 0 {
						m.selectedFile--
						m.viewport.SetContent(renderFilesContent(&m))
					}
				} else {
					m.viewport, cmd = m.viewport.Update(msg)
					cmds = append(cmds, cmd)
				}
			}

		case "down", "j":
			if m.focusLeft {
				if m.workspace == WorkspaceDMs {
					if len(m.chats) > 0 && m.selectedChat < len(m.chats)-1 {
						m.selectedChat++
						if m.viewMode == ModeChat {
							m.loading = true
							m.loadedConvID = m.chats[m.selectedChat].ID
							cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
						} else if m.viewMode == ModeFiles {
							m.loadedConvID = m.chats[m.selectedChat].ID
							m.loading = true
							cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
						}
					}
				} else if m.focusList == 0 && len(m.teams) > 0 {
					if m.selectedTeam < len(m.teams)-1 {
						m.selectedTeam++
						m.loading = true
						cmds = append(cmds, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID))
					}
				} else if m.focusList == 1 && len(m.channels) > 0 {
					if m.selectedChan < len(m.channels)-1 {
						m.selectedChan++
					}
				}
			} else {
				if m.viewMode == ModeFiles {
					if m.selectedFile < len(m.files)-1 {
						m.selectedFile++
						m.viewport.SetContent(renderFilesContent(&m))
					}
				} else {
					m.viewport, cmd = m.viewport.Update(msg)
					cmds = append(cmds, cmd)
				}
			}

		case "left", "h":
			if !m.focusLeft {
				if m.viewMode == ModeFiles && len(m.folderStack) > 0 {
					// Volver atrás en el árbol de directorios
					m.loading = true
					m.folderStack = m.folderStack[:len(m.folderStack)-1]
					if len(m.folderStack) == 0 {
						// Volvimos a la raíz del canal
						cmds = append(cmds, loadFilesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].DisplayName))
					} else {
						// Volvimos a una subcarpeta
						parent := m.folderStack[len(m.folderStack)-1]
						cmds = append(cmds, loadFolderCmd(m.client, m.teams[m.selectedTeam].ID, parent))
					}
				} else {
					m.focusLeft = true
				}
			} else {
				m.focusList = 0
			}

		case "right", "l":
			if m.focusLeft && m.workspace == WorkspaceTeams {
				m.focusList = 1
			}

		case "f":
			if !m.isTyping {
				m.isTyping = false // reseteo por seguridad
				if m.workspace == WorkspaceTeams && len(m.channels) > 0 {
					if m.viewMode == ModeChat {
						m.viewMode = ModeFiles
						m.loading = true
						m.folderStack = nil
						cmds = append(cmds, loadFilesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].DisplayName))
					} else {
						m.viewMode = ModeChat
						m.loading = true
						cmds = append(cmds, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID, 200))
					}
				} else if m.workspace == WorkspaceDMs && len(m.chats) > 0 {
					activeID := m.activeConversationID()
					if m.viewMode == ModeChat {
						// Si cambiamos de chat sin apretar Enter, igual funciona
						if m.loadedConvID != activeID {
							m.loading = true
							m.loadedConvID = activeID
							m.viewMode = ModeChat // Forzamos ModeChat porque estamos pidiendo los mensajes desde cero
							cmds = append(cmds, loadMessagesCmd(m.client, "", activeID, 200))
						} else {
							// Agregación local: cero red, usa lo que ya está en m.messages.
							m.viewMode = ModeFiles
							m.folderStack = nil
							m.files = aggregateChatAttachments(m.messages)
							m.selectedFile = 0
							m.viewport.SetContent(renderFilesContent(&m))
							m.viewport.GotoTop()
						}
					} else {
						m.viewMode = ModeChat
						if m.loadedConvID != activeID {
							m.loading = true
							m.loadedConvID = activeID
							cmds = append(cmds, loadMessagesCmd(m.client, "", activeID, 200))
						} else {
							m.viewport.SetContent(formatMessages(m.messages))
						}
					}
				}
			}

		case "c", "C":
			if !m.isTyping && m.workspace == WorkspaceDMs && m.viewMode == ModeFiles {
				m.loading = true
				m.folderStack = nil
				// Cargar historial completo (1000 mensajes es el límite práctico de un chunk sin colgar)
				cmds = append(cmds, loadMessagesCmd(m.client, "", m.activeConversationID(), 1000))
			}

		case "i":
			// Blindaje total de la UI: Solo funciona en ModeChat
			if !m.focusLeft && m.viewMode == ModeChat && m.activeConversationID() != "" {
				m.isTyping = true
				m.input.Focus()
			}

		case "enter":
			if m.focusLeft && m.workspace == WorkspaceDMs && len(m.chats) > 0 {
				m.loading = true
				m.focusLeft = false
				m.isTyping = false
				m.viewMode = ModeChat // OBLIGATORIO RESETEAR
				chatID := m.chats[m.selectedChat].ID
				m.loadedConvID = chatID
				delete(m.chatUnread, chatID) // Limpiar badge al abrir
				cmds = append(cmds, loadMessagesCmd(m.client, "", chatID, 200))
			} else if m.focusLeft && m.focusList == 1 && len(m.channels) > 0 {
				m.loading = true
				m.focusLeft = false
				m.isTyping = false
				m.viewMode = ModeChat // OBLIGATORIO RESETEAR
				chanID := m.channels[m.selectedChan].ID
				m.loadedConvID = chanID
				cmds = append(cmds, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, chanID, 200))
			} else if !m.focusLeft && m.viewMode == ModeFiles && len(m.files) > 0 {
				selected := m.files[m.selectedFile]
				if selected.RemoteItem != nil {
					// Shortcut (ej: "Materiales de clase" en tenants educativos nuevos):
					// el contenido real vive en otro Drive. Navegamos con Drive/Item remoto.
					m.loading = true
					node := FolderNode{
						ID:      selected.RemoteItem.ID,
						Name:    selected.Name,
						DriveID: selected.RemoteItem.ParentReference.DriveID,
					}
					m.folderStack = append(m.folderStack, node)
					cmds = append(cmds, loadFolderCmd(m.client, m.teams[m.selectedTeam].ID, node))
				} else if selected.Folder != nil {
					// Carpeta normal dentro del Drive del equipo
					m.loading = true
					node := FolderNode{ID: selected.ID, Name: selected.Name}
					m.folderStack = append(m.folderStack, node)
					cmds = append(cmds, loadFolderCmd(m.client, m.teams[m.selectedTeam].ID, node))
				} else {
					// Es un archivo, abrimos
					link := selected.DownloadUrl
					if link == "" {
						link = selected.WebUrl
					}
					if link != "" {
						openBrowser(link)
					}
				}
			}
		}

		// --- MOTOR DE CÁMARA PANEL IZQUIERDO ---
		if m.focusLeft && m.ready {
			var cursorLine int
			if m.workspace == WorkspaceDMs {
				cursorLine = 1 + m.selectedChat // 1 por el título "Chats"
			} else if m.focusList == 1 {
				cursorLine = len(m.teams) + 3 + m.selectedChan // Sumamos los equipos y los espacios
			} else {
				cursorLine = 1 + m.selectedTeam // 1 por el título "Equipos"
			}
			
			// Centramos la cámara en el cursor
			offset := cursorLine - (m.leftVp.Height / 2)
			if offset < 0 {
				offset = 0
			}
			m.leftVp.SetYOffset(offset)
		}
	}

	return m, tea.Batch(cmds...)
}

// makeClickableLink envuelve un texto con la secuencia ANSI OSC 8
// soportada por terminales modernas (como Kitty, Alacritty, GNOME Terminal)
func makeClickableLink(text, url string) string {
	// \x1b]8;;URL\x1b\ TEXTO \x1b]8;;\x1b\
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	}
	if err != nil {
		// Log error if needed, but for TUI we just silently fail or could send an error msg
	}
}

func renderFilesContent(m *Model) string {
	if len(m.files) == 0 {
		return "  Esta carpeta está vacía."
	}
	var b strings.Builder
	for i, f := range m.files {
		cursor := "  "
		style := normalItemStyle
		if i == m.selectedFile {
			cursor = "▶ "
			style = selectedItemStyle
		}
		
		icon := getFileIcon(f)
		link := f.DownloadUrl
		if link == "" {
			link = f.WebUrl
		}
		
		// Aplicamos el estilo de selección a toda la línea
		line := fmt.Sprintf("%s %s", icon, f.Name)
		line = style.Render(line)
		
		// Lo hacemos clickeable
		clickableLine := makeClickableLink(line, link)
		
		b.WriteString(cursor + clickableLine + "\n")
	}

	if m.workspace == WorkspaceDMs {
		b.WriteString("\n\n  " + helpStyle.Render("(Mostrando adjuntos recientes. Presioná 'C' para cargar el historial completo)"))
	}

	return b.String()
}
