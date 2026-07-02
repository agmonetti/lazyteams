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
		return fmt.Sprintf("\n[!] Error: %v\n\nPress 'q' to quit.\n", m.err)
	}

	if m.teamsLoaded && len(m.teams) == 0 {
		return "\nNo teams found. This account may not have Teams Enterprise or may not belong to any team.\n\nPress 'q' to quit.\n"
	}

	// Total height available for panels
	// topBar(2) + footer(2) + top+bottom panel border(2) = 6
	if m.width == 0 || m.height == 0 {
		return ""
	}
	panelOuterHeight := m.height - 6

	// Width(n) adds 2 cols of border per panel; 1 col of margin for Kitty.
	available       := m.width - 5
	leftOuterWidth  := available / 3
	rightOuterWidth := available - leftOuterWidth

	// INNER dimensions
	leftInnerHeight := panelOuterHeight - 2

	rightInnerWidth := rightOuterWidth - 2
	rightInnerHeight := panelOuterHeight - 2

	// === Left Panel ===
	leftContent := ""

	if m.workspace == WorkspaceDMs {
		leftContent += titleStyle.Render("Chats") + "\n"
		if len(m.chats) == 0 {
			leftContent += "  (no chats)\n"
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

				// Presence: find the member that isn't me
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
					style = style.Copy().Foreground(lipgloss.Color("11")) // yellow for unread
				}
				leftContent += fmt.Sprintf("%s%s%s\n", cursor, style.Render(name), presenceDot)
			}
		}
	} else if m.workspace == WorkspaceTeams {
		leftContent += titleStyle.Render("Teams") + "\n"
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

		// Channels title
		leftContent += titleStyle.Render("Channels") + "\n"

		if m.channelErr != nil {
			leftContent += lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(fmt.Sprintf("  [Blocked: %v]\n", m.channelErr))
		} else if len(m.channels) == 0 {
			leftContent += "  (no channels)\n"
		} else {
			// Calculate how many channels fit in the viewport
			teamsLines := len(m.teams) + 3
			viewportH := leftInnerHeight
			if viewportH <= 0 {
				viewportH = 10
			}
			// Reserve 2 lines for indicators
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
				// Adjust windowStart if we're at the end and there's extra space
				if totalChans >= maxChannels {
					windowStart = totalChans - maxChannels
				} else {
					windowStart = 0
				}
			}

			// Hidden channels indicator above
			if windowStart > 0 {
				leftContent += metaStyle.Render(fmt.Sprintf("  ... (%d above)\n", windowStart))
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

			// More channels indicator below
			if windowEnd < totalChans {
				hidden := totalChans - windowEnd
				leftContent += metaStyle.Render(fmt.Sprintf("  ... (%d more)\n", hidden))
			}
		}
	} else if m.workspace == WorkspaceActivity {
		leftContent = renderNotifList(m)
	} else if m.workspace == WorkspaceAssignments {
		leftContent = renderAssignList(m)
	}

	// Pass all that text to the left viewport
	if m.ready {
		m.leftVp.SetContent(leftContent)
		leftContent = m.leftVp.View()
	}

	// Apply styles to the left panel
	lStyle := paneStyle.Width(leftOuterWidth).Height(panelOuterHeight - 2)
	if m.focusLeft {
		lStyle = focusedPaneStyle.Width(leftOuterWidth).Height(panelOuterHeight - 2)
	}
	leftPanel := lStyle.Render(leftContent)

	// === Right Panel ===
	rightContent := ""

	// Loading state: only show "Loading..." on the splash
	if m.loading && m.focusLeft && m.workspace != WorkspaceActivity && m.workspace != WorkspaceAssignments {
		splashContent := lipgloss.JoinVertical(lipgloss.Center,
			splashLogoStyle.Render(asciiLogo),
			"",
			splashTitleStyle.Render("Microsoft Teams Terminal UI"),
			splashSubStyle.Render("v1.0.0-beta"),
			"",
			splashHintStyle.Render("[↑/↓] Navigate teams  ·  [Enter] Open channel"),
			"",
			splashHintStyle.Render("Loading..."),
		)
		if m.ready {
			rightContent = lipgloss.Place(rightInnerWidth, rightInnerHeight, lipgloss.Center, lipgloss.Center, splashContent)
		} else {
			rightContent = "Loading..."
		}
	} else if m.focusLeft && m.loadedConvID == "" && m.workspace != WorkspaceActivity && m.workspace != WorkspaceAssignments {
		// === SPLASH SCREEN ===
		splashContent := lipgloss.JoinVertical(lipgloss.Center,
			splashLogoStyle.Render(asciiLogo),
			"",
			splashTitleStyle.Render("Microsoft Teams Terminal UI"),
			splashSubStyle.Render("v1.0.0-beta"),
			"",
			splashHintStyle.Render("[↑/↓] Navigate teams  ·  [Enter] Open channel"),
		)
		if m.ready {
			rightContent = lipgloss.Place(rightInnerWidth, rightInnerHeight, lipgloss.Center, lipgloss.Center, splashContent)
		} else {
			rightContent = splashContent
		}
	} else if m.workspace == WorkspaceDMs {
		// Header: only if data is loaded
		if m.loadedConvID != "" && m.loadedConvID == m.activeConversationID() && len(m.chats) > 0 && m.selectedChat < len(m.chats) {
			tabChat, tabFiles := renderTabs(m.viewMode, ModeChat, "Messages", "Files")
			title := fmt.Sprintf("@ %s", m.chats[m.selectedChat].DisplayName(m.selfID))
			header := titleStyle.Render(title)
			rightContent += fmt.Sprintf("%s\n%s %s %s\n\n", header, tabChat, tabDividerStyle.Render("·"), tabFiles)
		}

		if m.loadedConvID != "" && m.loadedConvID == m.activeConversationID() {
			rightContent += m.viewport.View() + "\n"
		} else if !m.focusLeft {
			// Center help text when no conversation is loaded
			emptyState := helpStyle.Render("Press Enter to open this chat.")
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
			inputView := m.input.View()
			inputLines := strings.Count(inputView, "\n") +1
			m.viewport.Height = rightInnerHeight - 4 - inputLines -1
			rightContent += inputView
		}
	} else if m.workspace == WorkspaceTeams {
		// Header: only if data is loaded
		if m.loadedConvID != "" && m.loadedConvID == m.activeConversationID() && len(m.channels) > 0 && m.selectedChan < len(m.channels) {
			tabChat, tabFiles := renderTabs(m.viewMode, ModeChat, "Posts", "Files")
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
				emptyState := helpStyle.Render("Press Enter to open this channel.")
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
			inputView := m.input.View()
			inputLines := strings.Count(inputView, "\n") +1
			m.viewport.Height = rightInnerHeight - 4 - inputLines -1
			rightContent += inputView
		}
	} else if m.workspace == WorkspaceActivity {
		rightContent = renderNotifDetail(m)
	} else if m.workspace == WorkspaceAssignments {
		rightContent = renderAssignDetail(m)
	}

	// Apply styles to the right panel — no fixed Height, content defines the height
	rStyle := paneStyle.Width(rightOuterWidth)
	if !m.focusLeft {
		rStyle = focusedPaneStyle.Width(rightOuterWidth)
	}
	rightPanel := rStyle.Render(rightContent)

	// Popups: centered using the REAL rendered dimensions of the panel,
	// not "raw" vpWidth/vpHeight — so they never collide with paneStyle's border/padding.
	if m.confirmingDownload {
		names := make([]string, len(m.downloadTargets))
		for i, t := range m.downloadTargets {
			names[i] = t.Name
		}

		var dirLine string
		if m.editingDownloadDir {
			dirLine = fmt.Sprintf("Destination: %s", m.downloadDirInput.View())
		} else {
			dirLine = fmt.Sprintf("Destination: %s", m.prefs.DownloadDir)
		}

		var actions string
		if m.editingDownloadDir {
			actions = "[Enter] Confirm path   [Esc] Cancel"
		} else {
			actions = "[Enter/y] Download   [e] Edit path   [Esc/n] Cancel"
		}

		question := fmt.Sprintf(
			"Download %d file(s)?\n\n%s\n\n%s\n\n%s",
			len(names),
			strings.Join(names, "\n  "),
			dirLine,
			actions,
		)
		popup := popupStyle.Render(question)
		rightPanel = lipgloss.Place(lipgloss.Width(rightPanel), lipgloss.Height(rightPanel), lipgloss.Center, lipgloss.Center, popup)
	} else if m.showPresenceMenu {
		var menu string
		menu += titleStyle.Render("Set Status") + "\n\n"
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

	// Combine panels
	ui := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Top status bar: name + own presence, always visible and right-aligned
	if m.ready {
		name := m.userName
		if name == "" {
			name = "Me"
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

	// Contextual footer
	footerLine := footerStyle.Render(m.footerText())
	if m.presenceError != "" {
		footerLine += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("Presence error: " + m.presenceError)
	}
	ui = lipgloss.JoinVertical(lipgloss.Left, ui, footerLine)

	return ui
}

func presenceSymbol(avail string) string {
	switch avail {
	case "Available":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("●") // green
	case "Busy", "InACall", "InAMeeting":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("●") // red
	case "Away", "BeRightBack":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("●") // yellow
	case "DoNotDisturb":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("●") // red
	case "Offline", "PresenceUnknown":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("●") // gray
	case "Reset (Automatic)":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render("○") // hollow circle
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("●") // default gray
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

func (m Model) unreadCount() int {
	count := 0
	for _, n := range m.notifications {
		if !n.IsRead {
			count++
		}
	}
	return count
}

func (m Model) footerText() string {
	activityTab := "[3] Activity"
	if m.notifLoaded && m.unreadCount() > 0 {
		activityTab = fmt.Sprintf("[3] Activity %s", selectedItemStyle.Render("●"))
	}
	switch {
	case m.showPresenceMenu:
		return " [↑/↓] Navigate   [Enter] Confirm   [Esc/q] Cancel"
	case m.confirmingDownload && m.editingDownloadDir:
		return " [Enter] Confirm path   [Esc] Cancel editing"
	case m.confirmingDownload:
		return " [Enter/y] Download   [e] Edit path   [Esc/n] Cancel"
	case m.workspace == WorkspaceAssignments:
		return fmt.Sprintf(" [1] Teams  [2] DMs  %s  [4] Assignments  [←/→] Filter  [↑/↓] Navigate  [Enter] View  [q] Quit", activityTab)
	case m.workspace == WorkspaceActivity:
		if !m.focusLeft {
			return " [o] Go to channel  [Esc] Back  [q] Quit"
		}
		return fmt.Sprintf(" [1-4] Workspace  [←/→] Filter  [↑/↓] Navigate  [Enter] View details  [q] Quit")
	case m.focusLeft && m.loadedConvID == "":
		return fmt.Sprintf(" [1] Teams  [2] DMs  %s  [4] Assignments  [↑/↓] Navigate  [Enter] Open  [p] Status  [q] Quit", activityTab)
	case m.previewing:
		return " [Esc] Back to files  [↑/↓] Scroll"
	case !m.focusLeft && m.viewMode == ModeFiles:
		return " [↑/↓] Navigate  [Enter] Open  [Space] Select  [v] Preview  [o] Download  [p] Status  [Esc/h] Back"
	case !m.focusLeft && m.viewMode == ModeChat:
		return " [↑/↓] Scroll  [i] Type  [f] Files  [p] Status  [Esc/h] Back"
	default:
		return fmt.Sprintf(" [1] Teams  [2] DMs  %s  [4] Assignments  [↑/↓] Navigate  [Enter] Open  [p] Status  [q] Quit", activityTab)
	}
}

func renderNotifList(m Model) string {
	if m.notifErr != nil {
		errText := errorStyle.Render("⚠ " + m.notifErr.Error())
		hint := helpStyle.Render("  Update the token in DevTools → Cookies → TEAMS_NOTIF_TOKEN")
		retry := helpStyle.Render("  [r] Retry")
		return errText + "\n\n" + hint + "\n" + retry
	}
	if !m.notifLoaded {
		return helpStyle.Render("Loading activity...")
	}

	// Filter tabs
	filterNames := []string{"All", "Upcoming", "Overdue"}
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
			helpStyle.Render("No notifications in this category."))
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
		return helpStyle.Render("Select a notification to view details.")
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
		b.WriteString(helpStyle.Render("[o] Go to channel"))
	} else {
		b.WriteString(helpStyle.Render("[Esc] Back"))
	}

	return b.String()
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 h ago"
		}
		return fmt.Sprintf("%d h ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
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
		"january": time.January, "february": time.February, "march": time.March,
		"april": time.April, "may": time.May, "june": time.June,
		"july": time.July, "august": time.August, "september": time.September,
		"october": time.October, "november": time.November, "december": time.December,
		// Spanish month names for backwards compatibility with existing data
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
		b.WriteString(titleStyle.Render("Assignments") + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("Education Assignments API blocked") + "\n\n")
		b.WriteString(helpStyle.Render("Microsoft WAF blocks access\nfrom native clients without a browser session.\n\nUse the Assignments tab in teams.microsoft.com\nto view your assignments."))
		return b.String()
	}

	if !m.assignLoaded {
		return helpStyle.Render("Loading assignments...")
	}

	filterNames := []string{"All", "Upcoming", "Overdue", "Completed"}
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
		return header + "\n" + helpStyle.Render("No assignments in this category.")
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
		due := "No due date"
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
		return helpStyle.Render("Select an assignment to view details.")
	}

	a := filtered[m.selectedAssign]

	due := "No due date"
	if !a.DueDateTime.IsZero() {
		due = a.DueDateTime.Local().Format("02/01/2006 15:04")
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(graph.AssignmentStatusLabel(a.Status) + " Assignment"))
	b.WriteString("\n\n")
	b.WriteString(selectedItemStyle.Render("Name:     ") + a.DisplayName + "\n")
	b.WriteString(selectedItemStyle.Render("Class:    ") + a.ClassID + "\n")
	b.WriteString(selectedItemStyle.Render("Due:      ") + due + "\n")
	b.WriteString(selectedItemStyle.Render("Status:   ") + a.Status + "\n")

	if !m.focusLeft {
		b.WriteString("\n")
		b.WriteString(metaStyle.Render("─────────────────────────────────"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("[Esc] Back to list"))
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
