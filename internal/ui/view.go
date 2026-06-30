package ui

import (
	"fmt"
	"strings"
	"time"

	"teamsTUI/internal/graph"

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

	// Altura total disponible para los paneles
	// topBar(2) + footer(2) + borde panel superior+inferior(2) = 6
	if m.width == 0 || m.height == 0 {
		return ""
	}
	panelOuterHeight := m.height - 6

	// Width(n) suma 2 cols de borde por panel; 1 col de margen para Kitty.
	available       := m.width - 5
	leftOuterWidth  := available / 3
	rightOuterWidth := available - leftOuterWidth

	// Dimensiones INTERNAS
	leftInnerHeight := panelOuterHeight - 2

	rightInnerWidth := rightOuterWidth - 2
	rightInnerHeight := panelOuterHeight - 2

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
	} else if m.workspace == WorkspaceTeams {
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
			leftContent += fmt.Sprintf("%s%s\n", cursor, style.Render(truncateText(t.DisplayName, leftOuterWidth-4)))
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
			viewportH := leftInnerHeight
			if viewportH <= 0 {
				viewportH = 10
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
				leftContent += fmt.Sprintf("%s%s\n", cursor, style.Render(truncateText(c.DisplayName, leftOuterWidth-4)))
			}

			// Indicador de más canales abajo
			if windowEnd < totalChans {
				hidden := totalChans - windowEnd
				leftContent += metaStyle.Render(fmt.Sprintf("  ... (%d más)\n", hidden))
			}
		}
	} else if m.workspace == WorkspaceActivity {
		leftContent = renderNotifList(m)
	} else if m.workspace == WorkspaceAssignments {
		leftContent = renderAssignList(m)
	}

	// Le pasamos todo ese texto al viewport izquierdo
	if m.ready {
		m.leftVp.SetContent(leftContent)
		leftContent = m.leftVp.View()
	}

	// Aplicar estilos al panel izquierdo
	lStyle := paneStyle.Width(leftOuterWidth).Height(panelOuterHeight - 2)
	if m.focusLeft {
		lStyle = focusedPaneStyle.Width(leftOuterWidth).Height(panelOuterHeight - 2)
	}
	leftPanel := lStyle.Render(leftContent)

	// === Panel Derecho ===
	rightContent := ""

	// Estado de carga: solo mostrar "Cargando..." en el splash
	if m.loading && m.focusLeft && m.workspace != WorkspaceActivity && m.workspace != WorkspaceAssignments {
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
			rightContent = lipgloss.Place(rightInnerWidth, rightInnerHeight, lipgloss.Center, lipgloss.Center, splashContent)
		} else {
			rightContent = "Cargando..."
		}
	} else if m.focusLeft && m.loadedConvID == "" && m.workspace != WorkspaceActivity && m.workspace != WorkspaceAssignments {
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
			rightContent = lipgloss.Place(rightInnerWidth, rightInnerHeight, lipgloss.Center, lipgloss.Center, splashContent)
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
			rightContent = lipgloss.Place(rightInnerWidth, rightInnerHeight, lipgloss.Center, lipgloss.Center, emptyState)
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
	} else if m.workspace == WorkspaceTeams {
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
				rightContent = lipgloss.Place(rightInnerWidth, rightInnerHeight, lipgloss.Center, lipgloss.Center, emptyState)
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
	} else if m.workspace == WorkspaceActivity {
		rightContent = renderNotifDetail(m)
	} else if m.workspace == WorkspaceAssignments {
		rightContent = renderAssignDetail(m)
	}

	// Aplicar estilos al panel derecho — PRIMERO con el contenido normal
	rStyle := paneStyle.Width(rightOuterWidth).Height(panelOuterHeight - 2)
	if !m.focusLeft {
		rStyle = focusedPaneStyle.Width(rightOuterWidth).Height(panelOuterHeight - 2)
	}
	rightPanel := rStyle.Render(rightContent)

	// Popups: se centran usando las dimensiones REALES ya renderizadas del panel,
	// no vpWidth/vpHeight "crudos" — así nunca chocan con el border/padding de paneStyle.
	if m.confirmingDownload {
		names := make([]string, len(m.downloadTargets))
		for i, t := range m.downloadTargets {
			names[i] = t.Name
		}

		var dirLine string
		if m.editingDownloadDir {
			dirLine = fmt.Sprintf("Destino: %s", m.downloadDirInput.View())
		} else {
			dirLine = fmt.Sprintf("Destino: %s", m.prefs.DownloadDir)
		}

		var actions string
		if m.editingDownloadDir {
			actions = "[Enter] Confirmar ruta   [Esc] Cancelar"
		} else {
			actions = "[Enter/y] Descargar   [e] Editar ruta   [Esc/n] Cancelar"
		}

		question := fmt.Sprintf(
			"¿Descargar %d archivo(s)?\n\n%s\n\n%s\n\n%s",
			len(names),
			strings.Join(names, "\n  "),
			dirLine,
			actions,
		)
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
		topBar := lipgloss.NewStyle().Width(m.width - 1).Align(lipgloss.Right).PaddingRight(2).Render(userInfo)
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
	case m.confirmingDownload && m.editingDownloadDir:
		return " [Enter] Confirmar ruta   [Esc] Cancelar edición"
	case m.confirmingDownload:
		return " [Enter/y] Descargar   [e] Editar ruta   [Esc/n] Cancelar"
	case m.workspace == WorkspaceAssignments:
		return " [1] Equipos  [2] DMs  [3] Actividad  [4] Tareas  [←/→] Filtrar  [↑/↓] Navegar  [Enter] Ver  [q] Salir"
	case m.workspace == WorkspaceActivity:
		if !m.focusLeft {
			return " [o] Ir al canal  [Esc] Volver  [q] Salir"
		}
		return " [1-4] Workspace  [←/→] Filtrar  [↑/↓] Navegar  [Enter] Ver detalle  [q] Salir"
	case m.focusLeft && m.loadedConvID == "":
		return " [1] Equipos  [2] DMs  [3] Actividad  [4] Tareas  [↑/↓] Navegar  [Enter] Abrir  [p] Estado  [q] Salir"
	case m.previewing:
		return " [Esc] Volver a archivos  [↑/↓] Scroll"
	case !m.focusLeft && m.viewMode == ModeFiles:
		return " [↑/↓] Navegar  [Enter] Abrir  [Space] Seleccionar  [v] Preview  [o] Descargar  [p] Estado  [Esc/h] Volver"
	case !m.focusLeft && m.viewMode == ModeChat:
		return " [↑/↓] Scroll  [i] Escribir  [f] Archivos  [p] Estado  [Esc/h] Volver"
	default:
		return " [1] Equipos  [2] DMs  [3] Actividad  [4] Tareas  [↑/↓] Navegar  [Enter] Abrir  [p] Estado  [q] Salir"
	}
}

func renderNotifList(m Model) string {
	if m.notifErr != nil {
		errText := errorStyle.Render("⚠ " + m.notifErr.Error())
		hint := helpStyle.Render("  Actualizá el token en DevTools → Cookies → TEAMS_NOTIF_TOKEN")
		retry := helpStyle.Render("  [r] Reintentar")
		return errText + "\n\n" + hint + "\n" + retry
	}
	if !m.notifLoaded {
		return helpStyle.Render("Cargando actividad...")
	}

	// Filter tabs
	filterNames := []string{"Todas", "Próximas", "Vencidas"}
	var tabs []string
	for i, name := range filterNames {
		if ActivityFilter(i) == m.activityFilter {
			tabs = append(tabs, activeTabStyle.Render("[ "+name+" ]"))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render("[ "+name+" ]"))
		}
	}
	header := strings.Join(tabs, " ") + "\n"

	// Apply filter
	filtered := m.filteredNotifications()
	if len(filtered) == 0 {
		return header + "\n" + lipgloss.Place(0, 0, lipgloss.Center, lipgloss.Center,
			helpStyle.Render("Sin notificaciones en esta categoría."))
	}

	// Map filtered index back to original index for selection
	origIdx := make([]int, len(filtered))
	filteredMap := map[int]int{} // filtered index → original index
	for fi, n := range filtered {
		for oi, on := range m.notifications {
			if n.ID == on.ID {
				origIdx[fi] = oi
				filteredMap[fi] = oi
				break
			}
		}
	}

	lines := make([]string, 0, len(filtered))
	for fi, n := range filtered {
		cursor := "  "
		style := normalItemStyle
		if origIdx[fi] == m.selectedNotif {
			if m.focusLeft {
				cursor = "▶ "
				style = selectedItemStyle
			} else {
				style = style.Copy().Foreground(lipgloss.Color("245"))
			}
		}

		label := graph.ActivityTypeLabel(n.Subtype)
		age := formatAge(n.Timestamp)
		title := truncate(n.SenderName, 20)
		preview := truncate(n.Preview, 30)

		line := fmt.Sprintf("%s%s%s %s\n   %s", cursor, label, style.Render(title), metaStyle.Render(age), preview)
		lines = append(lines, line)
	}
	return header + strings.Join(lines, "\n")
}

func renderNotifDetail(m Model) string {
	if len(m.notifications) == 0 || m.selectedNotif >= len(m.notifications) {
		return helpStyle.Render("Seleccioná una notificación para ver detalles.")
	}

	n := m.notifications[m.selectedNotif]
	label := graph.ActivityTypeLabel(n.Subtype)

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("%s %s", label, n.SenderName)))
	b.WriteString("\n")
	b.WriteString(metaStyle.Render(formatAge(n.Timestamp)))
	b.WriteString("\n\n")
	b.WriteString(n.Preview)
	b.WriteString("\n\n")
	b.WriteString(metaStyle.Render("─────────────────────────────────"))
	b.WriteString("\n")
	if n.SourceThread != "" {
		b.WriteString(helpStyle.Render("[o] Ir al canal"))
	} else {
		b.WriteString(helpStyle.Render("[Esc] Volver"))
	}

	return b.String()
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "ahora"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "hace 1 min"
		}
		return fmt.Sprintf("hace %d min", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "hace 1 h"
		}
		return fmt.Sprintf("hace %d h", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "hace 1 día"
		}
		return fmt.Sprintf("hace %d días", days)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func (m Model) filteredNotifications() []graph.NotificationItem {
	if m.activityFilter == FilterAll {
		return m.notifications
	}
	now := time.Now()
	weekFromNow := now.Add(7 * 24 * time.Hour)
	var result []graph.NotificationItem
	for _, n := range m.notifications {
		switch m.activityFilter {
		case FilterUpcoming:
			if n.Subtype == "assignmentPublishedNotification" || n.Subtype == "assignmentDueDateNotification" {
				result = append(result, n)
			}
		case FilterOverdue:
			if (n.Subtype == "assignmentPublishedNotification" || n.Subtype == "assignmentDueDateNotification") && n.Timestamp.Before(now) && n.Timestamp.After(weekFromNow) == false {
				if containsPastDate(n.Preview, now) {
					result = append(result, n)
				}
			}
		}
	}
	return result
}

func containsPastDate(preview string, now time.Time) bool {
	lower := strings.ToLower(preview)
	months := map[string]time.Month{
		"enero": time.January, "febrero": time.February, "marzo": time.March,
		"abril": time.April, "mayo": time.May, "junio": time.June,
		"julio": time.July, "agosto": time.August, "septiembre": time.September,
		"octubre": time.October, "noviembre": time.November, "diciembre": time.December,
	}
	for keyword, month := range months {
		idx := strings.Index(lower, keyword)
		if idx > 0 {
			start := idx - 5
			if start < 0 {
				start = 0
			}
			prefix := preview[start:idx]
			for _, w := range strings.Fields(prefix) {
				day := 0
				fmt.Sscanf(w, "%d", &day)
				if day > 0 && day <= 31 {
					year := now.Year()
					date := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
					if date.Before(now) {
						return true
					}
				}
			}
		}
	}
	return false
}

func filteredAssignments(m Model) []graph.Assignment {
	if m.assignFilter == FilterAll {
		return m.assignments
	}
	now := time.Now()
	weekFromNow := now.Add(7 * 24 * time.Hour)
	var result []graph.Assignment
	for _, a := range m.assignments {
		switch m.assignFilter {
		case FilterUpcoming:
			if !a.DueDateTime.IsZero() && a.DueDateTime.After(now) && a.DueDateTime.Before(weekFromNow) {
				result = append(result, a)
			}
		case FilterOverdue:
			if !a.DueDateTime.IsZero() && a.DueDateTime.Before(now) && !a.IsCompleted {
				result = append(result, a)
			}
		case FilterCompleted:
			if a.IsCompleted {
				result = append(result, a)
			}
		}
	}
	return result
}

func renderAssignList(m Model) string {
	if m.assignErr != nil {
		var b strings.Builder
		b.WriteString(titleStyle.Render("Tareas") + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("API de Education Assignments bloqueada") + "\n\n")
		b.WriteString(helpStyle.Render("El WAF de Microsoft bloquea el acceso\ndesde clientes nativos sin sesión de browser.\n\nUsá la pestaña Assignments en teams.microsoft.com\npara ver tus tareas."))
		return b.String()
	}

	if !m.assignLoaded {
		return helpStyle.Render("Cargando tareas...")
	}

	filterNames := []string{"Todas", "Próximas", "Vencidas", "Completadas"}
	var tabs []string
	for i, name := range filterNames {
		if ActivityFilter(i) == m.assignFilter {
			tabs = append(tabs, activeTabStyle.Render("[ "+name+" ]"))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render("[ "+name+" ]"))
		}
	}
	header := strings.Join(tabs, " ") + "\n"

	filtered := filteredAssignments(m)
	if len(filtered) == 0 {
		return header + "\n" + helpStyle.Render("Sin tareas en esta categoría.")
	}

	var lines []string
	for i, a := range filtered {
		cursor := "  "
		style := normalItemStyle
		if i == m.selectedAssign {
			if m.focusLeft {
				cursor = "▶ "
				style = selectedItemStyle
			} else {
				style = style.Copy().Foreground(lipgloss.Color("245"))
			}
		}

		label := graph.AssignmentStatusLabel(a.Status)
		due := "Sin fecha"
		if !a.DueDateTime.IsZero() {
			due = a.DueDateTime.Local().Format("02/01 15:04")
		}
		line := fmt.Sprintf("%s%s %s\n   %s",
			cursor,
			label,
			style.Render(truncate(a.DisplayName, 28)),
			metaStyle.Render(due),
		)
		lines = append(lines, line)
	}
	return header + strings.Join(lines, "\n")
}

func renderAssignDetail(m Model) string {
	filtered := filteredAssignments(m)
	if len(filtered) == 0 || m.selectedAssign >= len(filtered) {
		return helpStyle.Render("Seleccioná una tarea para ver detalles.")
	}

	a := filtered[m.selectedAssign]

	due := "Sin fecha"
	if !a.DueDateTime.IsZero() {
		due = a.DueDateTime.Local().Format("02/01/2006 15:04")
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(graph.AssignmentStatusLabel(a.Status) + " Tarea"))
	b.WriteString("\n\n")
	b.WriteString(selectedItemStyle.Render("Nombre:   ") + a.DisplayName + "\n")
	b.WriteString(selectedItemStyle.Render("Clase:    ") + a.ClassID + "\n")
	b.WriteString(selectedItemStyle.Render("Vence:    ") + due + "\n")
	b.WriteString(selectedItemStyle.Render("Estado:   ") + a.Status + "\n")

	if !m.focusLeft {
		b.WriteString("\n")
		b.WriteString(metaStyle.Render("─────────────────────────────────"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("[Esc] Volver a la lista"))
	}

	return b.String()
}

func truncateText(text string, maxWidth int) string {
	if lipgloss.Width(text) <= maxWidth {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > maxWidth-1 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
