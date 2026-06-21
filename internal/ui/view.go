package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const asciiLogo = `
████████ ███████  █████  ███    ███ ███████       ████████ ██    ██ ██ 
   ██    ██      ██   ██ ████  ████ ██               ██    ██    ██ ██ 
   ██    █████   ███████ ██ ████ ██ ███████ █████    ██    ██    ██ ██ 
   ██    ██      ██   ██ ██  ██  ██      ██          ██    ██    ██ ██ 
   ██    ███████ ██   ██ ██      ██ ███████          ██     ██████  ██ 
                                                                       
                                                                       `


func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("\n[!] Error: %v\n\nPresiona 'q' para salir.\n", m.err)
	}

	if m.teamsLoaded && len(m.teams) == 0 {
		return "\nNo se encontraron equipos. Esta cuenta puede no tener Teams Enterprise o no pertenece a ningún equipo.\n\nPresiona 'q' para salir.\n"
	}

	// === Panel Izquierdo ===
	leftContent := ""

	if m.workspace == WorkspaceDMs {
		leftContent += titleStyle.Render("Chats") + "\n"
		if len(m.chats) == 0 {
			leftContent += "  (sin chats)\n"
		} else {
			for i, ch := range m.chats {
				cursor := "  "
				style := normalItemStyle
				if i == m.selectedChat {
					if m.focusLeft {
						cursor = "▶ "
						style = selectedItemStyle
					} else {
						style = style.Copy().Foreground(lipgloss.Color("245"))
					}
				}
				name := ch.DisplayName(m.selfID)

				// Presencia: buscar el miembro que no soy yo
				presenceDot := ""
				for _, u := range ch.Members {
					if u.UserID != m.selfID {
						if avail, ok := m.presence[u.UserID]; ok {
							presenceDot = " " + presenceSymbol(avail)
						}
						break
					}
				}

				if m.chatUnread[ch.ID] && i != m.selectedChat {
					name = "● " + name
					style = style.Copy().Foreground(lipgloss.Color("11")) // amarillo para unread
				}
				leftContent += fmt.Sprintf("%s%s%s\n", cursor, style.Render(name), presenceDot)
			}
		}
	} else {
		// Título Equipos
		leftContent += titleStyle.Render("Equipos") + "\n"
		for i, t := range m.teams {
			cursor := "  "
			style := normalItemStyle
			if i == m.selectedTeam {
				if m.focusLeft && m.focusList == 0 {
					cursor = "▶ "
					style = selectedItemStyle
				} else {
					style = style.Copy().Foreground(lipgloss.Color("245"))
				}
			}
			leftContent += fmt.Sprintf("%s%s\n", cursor, style.Render(t.DisplayName))
		}

		leftContent += "\n"

		// Título Canales
		leftContent += titleStyle.Render("Canales") + "\n"
		
		if m.channelErr != nil {
			leftContent += lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(fmt.Sprintf("  [Bloqueado: %v]\n", m.channelErr))
		} else if len(m.channels) == 0 {
			leftContent += "  (sin canales)\n"
		} else {
			// Calcular cuántos canales caben en el viewport
			teamsLines := len(m.teams) + 3
			viewportH := m.leftVp.Height
			if viewportH <= 0 {
				viewportH = m.height - 6
			}
			// Reservar 2 líneas para indicadores
			maxChannels := viewportH - teamsLines - 2
			if maxChannels < 5 {
				maxChannels = 5
			}

			totalChans := len(m.channels)

			// Sliding window
			windowStart := m.channelWindowStart
			if windowStart < 0 {
				windowStart = 0
			}
			windowEnd := windowStart + maxChannels
			if windowEnd > totalChans {
				windowEnd = totalChans
				// Ajustar windowStart si estamos al final y sobran espacios
				if totalChans >= maxChannels {
					windowStart = totalChans - maxChannels
				} else {
					windowStart = 0
				}
			}

			// Indicador de canales ocultos arriba
			if windowStart > 0 {
				leftContent += metaStyle.Render(fmt.Sprintf("  ... (%d arriba)\n", windowStart))
			}

			for i := windowStart; i < windowEnd; i++ {
				c := m.channels[i]
				cursor := "  "
				style := normalItemStyle
				if i == m.selectedChan {
					if m.focusLeft && m.focusList == 1 {
						cursor = "▶ "
						style = selectedItemStyle
					} else {
						style = style.Copy().Foreground(lipgloss.Color("245"))
					}
				}
				leftContent += fmt.Sprintf("%s%s\n", cursor, style.Render(c.DisplayName))
			}

			// Indicador de más canales abajo
			if windowEnd < totalChans {
				hidden := totalChans - windowEnd
				leftContent += metaStyle.Render(fmt.Sprintf("  ... (%d más)\n", hidden))
			}
		}
	}

	// Le pasamos todo ese texto al viewport izquierdo
	if m.ready {
		m.leftVp.SetContent(leftContent)
		leftContent = m.leftVp.View()
	}

	// Aplicar estilos al panel izquierdo
	// Reducimos la altura en 1 para evitar que la terminal scrollee y oculte la topBar
	panelOuterHeight := m.height - 5
	lStyle := paneStyle.Width((m.width / 3) - 2).Height(panelOuterHeight)
	if m.focusLeft {
		lStyle = focusedPaneStyle.Width((m.width / 3) - 2).Height(panelOuterHeight)
	}
	leftPanel := lStyle.Render(leftContent)

	// === Panel Derecho ===
	vpWidth := (m.width * 2 / 3) - 4
	vpHeight := panelOuterHeight
	rightContent := ""

	// Estado de carga: solo mostrar "Cargando..." en el splash
	if m.loading && m.focusLeft {
		splashContent := lipgloss.JoinVertical(lipgloss.Center,
			splashLogoStyle.Render(asciiLogo),
			"",
			splashTitleStyle.Render("Microsoft Teams Terminal UI"),
			splashSubStyle.Render("v1.0.0-beta"),
			"",
			splashHintStyle.Render("[↑/↓] Navegar equipos  ·  [Enter] Abrir canal"),
			"",
			splashHintStyle.Render("Cargando..."),
		)
		if m.ready {
			rightContent = lipgloss.Place(vpWidth, vpHeight, lipgloss.Center, lipgloss.Center, splashContent)
		} else {
			rightContent = "Cargando..."
		}
	} else if m.focusLeft && m.loadedConvID == "" {
		// === SPLASH SCREEN ===
		splashContent := lipgloss.JoinVertical(lipgloss.Center,
			splashLogoStyle.Render(asciiLogo),
			"",
			splashTitleStyle.Render("Microsoft Teams Terminal UI"),
			splashSubStyle.Render("v1.0.0-beta"),
			"",
			splashHintStyle.Render("[↑/↓] Navegar equipos  ·  [Enter] Abrir canal"),
		)
		if m.ready {
			rightContent = lipgloss.Place(vpWidth, vpHeight, lipgloss.Center, lipgloss.Center, splashContent)
		} else {
			rightContent = splashContent
		}
	} else if m.workspace == WorkspaceDMs {
		// Cabecera: solo si hay datos cargados
		if m.loadedConvID != "" && m.loadedConvID == m.activeConversationID() && len(m.chats) > 0 && m.selectedChat < len(m.chats) {
			tabChat, tabFiles := renderTabs(m.viewMode, ModeChat, "Mensajes", "Archivos")
			title := fmt.Sprintf("@ %s", m.chats[m.selectedChat].DisplayName(m.selfID))
			header := titleStyle.Render(title)
			rightContent += fmt.Sprintf("%s\n%s %s %s\n\n", header, tabChat, tabDividerStyle.Render("·"), tabFiles)
		}

		if m.loadedConvID != "" && m.loadedConvID == m.activeConversationID() {
			rightContent += m.viewport.View() + "\n"
		} else if !m.focusLeft {
			//Centrar texto de ayuda cuando no hay conversación cargada
			emptyState := helpStyle.Render("Presioná Enter para abrir este chat.")
			rightContent = lipgloss.Place(vpWidth, vpHeight, lipgloss.Center, lipgloss.Center, emptyState)
		}

		if !m.focusLeft && m.viewMode == ModeChat {
			if m.isTyping {
				m.input.PromptStyle = selectedItemStyle
				m.input.TextStyle = normalItemStyle
			} else {
				m.input.PromptStyle = normalItemStyle
				m.input.TextStyle = normalItemStyle
			}
			rightContent += m.input.View()
		}
	} else {
		// Cabecera: solo si hay datos cargados
		if m.loadedConvID != "" && m.loadedConvID == m.activeConversationID() && len(m.channels) > 0 && m.selectedChan < len(m.channels) {
			tabChat, tabFiles := renderTabs(m.viewMode, ModeChat, "Publicaciones", "Archivos")
			title := fmt.Sprintf("# %s", m.channels[m.selectedChan].DisplayName)
			if m.viewMode == ModeFiles && len(m.folderStack) > 0 {
				for _, node := range m.folderStack {
					title += fmt.Sprintf(" / %s", node.Name)
				}
			}
			header := titleStyle.Render(title)
			rightContent += fmt.Sprintf("%s\n%s %s %s\n\n", header, tabChat, tabDividerStyle.Render("·"), tabFiles)
		}

		if m.viewMode == ModeChat {
			if m.loadedConvID != "" && m.loadedConvID == m.activeConversationID() {
				rightContent += m.viewport.View() + "\n"
			} else if !m.focusLeft {
				emptyState := helpStyle.Render("Presioná Enter para abrir este canal.")
				rightContent = lipgloss.Place(vpWidth, vpHeight, lipgloss.Center, lipgloss.Center, emptyState)
			}
		} else {
			rightContent += m.viewport.View() + "\n"
		}

		if !m.focusLeft && m.viewMode == ModeChat && m.loadedConvID == m.activeConversationID() {
			if m.isTyping {
				m.input.PromptStyle = selectedItemStyle
				m.input.TextStyle = normalItemStyle
			} else {
				m.input.PromptStyle = normalItemStyle
				m.input.TextStyle = normalItemStyle
			}
			rightContent += m.input.View()
		}
	}

	// Aplicar estilos al panel derecho — PRIMERO con el contenido normal
	rStyle := paneStyle.Width(vpWidth).MaxWidth(vpWidth).Height(vpHeight).MaxHeight(vpHeight)
	if !m.focusLeft {
		rStyle = focusedPaneStyle.Width(vpWidth).MaxWidth(vpWidth).Height(vpHeight).MaxHeight(vpHeight)
	}
	rightPanel := rStyle.Render(rightContent)

	// Popups: se centran usando las dimensiones REALES ya renderizadas del panel,
	// no vpWidth/vpHeight "crudos" — así nunca chocan con el border/padding de paneStyle.
	if m.confirmingDownload {
		names := make([]string, len(m.downloadTargets))
		for i, t := range m.downloadTargets {
			names[i] = t.Name
		}
		question := fmt.Sprintf("¿Descargar %d archivo(s)?\n\n%s\n\n[y] Sí   [n] No", len(names), strings.Join(names, "\n  "))
		popup := popupStyle.Render(question)
		rightPanel = lipgloss.Place(lipgloss.Width(rightPanel), lipgloss.Height(rightPanel), lipgloss.Center, lipgloss.Center, popup)
	} else if m.showPresenceMenu {
		var menu string
		menu += titleStyle.Render("Establecer Estado") + "\n\n"
		for i, opt := range m.presenceOptions {
			cursor := "  "
			style := normalItemStyle
			if i == m.presenceCursor {
				cursor = "▶ "
				style = selectedItemStyle
			}
			menu += fmt.Sprintf("%s%s %s\n", cursor, presenceSymbol(opt), style.Render(opt))
		}
		popup := presencePopupStyle.Render(menu)
		rightPanel = lipgloss.Place(lipgloss.Width(rightPanel), lipgloss.Height(rightPanel), lipgloss.Center, lipgloss.Center, popup)
	}

	// Juntar paneles
	ui := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Status bar superior: nombre + presencia propia, siempre visible y alineado a la derecha
	if m.ready {
		name := m.userName
		if name == "" {
			name = "Yo"
		}
		myStatus := "Offline"
		if s, ok := m.presence[m.selfID]; ok && s != "" {
			myStatus = s
		}
		statusDot := presenceSymbol(myStatus)
		statusLabel := splashSubStyle.Render(fmt.Sprintf("(%s)", myStatus))
		userInfo := fmt.Sprintf("%s %s %s", splashSubStyle.Render(name), statusDot, statusLabel)
		topBar := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Right).PaddingRight(2).Render(userInfo)
		ui = lipgloss.JoinVertical(lipgloss.Left, topBar, ui)
	}

	// Footer contextual
	footerLine := footerStyle.Render(m.footerText())
	if m.presenceError != "" {
		footerLine += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("Error de presencia: "+m.presenceError)
	}
	ui = lipgloss.JoinVertical(lipgloss.Left, ui, footerLine)

	return ui
}

func presenceSymbol(avail string) string {
	switch avail {
	case "Available":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("●") // verde
	case "Busy", "InACall", "InAMeeting":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("●") // rojo
	case "Away", "BeRightBack":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("●") // amarillo
	case "DoNotDisturb":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("●") // rojo
	case "Offline", "PresenceUnknown":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("●") // gris
	case "Reset (Automático)":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render("○") // círculo hueco
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("●") // gris por defecto
	}
}

func renderTabs(current, active ViewMode, nameA, nameB string) (string, string) {
	var tabA, tabB string
	if current == active {
		tabA = activeTabStyle.Render("[ " + nameA + " ]")
		tabB = inactiveTabStyle.Render("[ " + nameB + " ]")
	} else {
		tabA = inactiveTabStyle.Render("[ " + nameA + " ]")
		tabB = activeTabStyle.Render("[ " + nameB + " ]")
	}
	return tabA, tabB
}

func (m Model) footerText() string {
	switch {
	case m.showPresenceMenu:
		return " [↑/↓] Navegar   [Enter] Confirmar   [Esc/q] Cancelar"
	case m.confirmingDownload:
		return " [y] Confirmar   [n] Cancelar"
	case m.focusLeft && m.loadedConvID == "":
		return " [1] Equipos  [2] DMs  [↑/↓] Navegar  [Enter] Abrir  [p] Estado  [q] Salir"
	case !m.focusLeft && m.viewMode == ModeFiles:
		return " [↑/↓] Navegar  [Enter] Abrir  [Space] Seleccionar  [o] Descargar  [p] Estado  [Esc/h] Volver"
	case !m.focusLeft && m.viewMode == ModeChat:
		return " [↑/↓] Scroll  [i] Escribir  [f] Archivos  [p] Estado  [Esc/h] Volver"
	default:
		return " [1] Equipos  [2] DMs  [↑/↓] Navegar  [Enter] Abrir  [p] Estado  [q] Salir"
	}
}
