package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"teamsTUI/internal/graph"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Comandos

type messageSentMsg struct{}
type messageSendErrMsg struct{ err error }
type filesMsg struct{ files []graph.DriveItem }
type filesErrMsg struct{ err error }

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

func loadMessagesCmd(client *graph.Client, teamID, channelID string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := client.GetMessages(teamID, channelID)
		if err != nil {
			return messagesErrMsg{err}
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

func getFileIcon(item graph.DriveItem) string {
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
				if v != "" && len(m.channels) > 0 {
					m.input.Reset()
					m.isTyping = false
					m.input.Blur()
					m.loading = true
					// Enviamos y agregamos el comando
					return m, sendMessageCmd(m.client, m.channels[m.selectedChan].ID, v)
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
		m.viewport.SetContent(fmt.Sprintf("Error cargando mensajes: %v", msg.err))
		m.loading = false
		return m, nil

	case messageSentMsg:
		// Mensaje enviado correctamente. Recargamos los mensajes del canal
		return m, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID)

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

	case messagesMsg:
		m.messages = msg.messages
		m.loading = false

		content := formatMessages(m.messages)
		m.viewport.SetContent(content)
		m.viewport.GotoBottom() // Chat: últimos mensajes abajo
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "tab":
			m.focusLeft = !m.focusLeft

		case "esc":
			if !m.focusLeft {
				m.focusLeft = true
			}

		case "up", "k":
			if m.focusLeft {
				if m.focusList == 0 && len(m.teams) > 0 {
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
				if m.focusList == 0 && len(m.teams) > 0 {
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
			if m.focusLeft {
				m.focusList = 1
			}

		case "f":
			if !m.isTyping && len(m.channels) > 0 {
				if m.viewMode == ModeChat {
					m.viewMode = ModeFiles
					m.loading = true
					m.folderStack = nil // Reiniciar historial de carpetas
					cmds = append(cmds, loadFilesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].DisplayName))
				} else {
					m.viewMode = ModeChat
					// Recargar los mensajes para limpiar el viewport de archivos
					m.loading = true
					cmds = append(cmds, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID))
				}
			}

		case "i":
			if !m.focusLeft && len(m.channels) > 0 {
				m.isTyping = true
				m.input.Focus()
			}

		case "enter":
			if m.focusLeft && m.focusList == 1 && len(m.channels) > 0 {
				m.loading = true
				m.focusLeft = false
				cmds = append(cmds, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID))
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
			// Calculamos en qué línea virtual está nuestro cursor
			cursorLine := 1 + m.selectedTeam // 1 por el título "Equipos"
			if m.focusList == 1 {
				cursorLine = len(m.teams) + 3 + m.selectedChan // Sumamos los equipos y los espacios
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
	return b.String()
}
