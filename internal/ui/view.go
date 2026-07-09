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


func renderInfoContent(m *Model) string {
	if m.channelInfo == nil {
		return "Loading info..."
	}

	ch := m.channelInfo
	email := ch.Email
	if email == "" {
		email = "—"
	}
	created := ""
	if len(ch.CreatedDateTime) >= 10 {
		created = ch.CreatedDateTime[:10]
	}
	
	var content string
	content += fmt.Sprintf(
		"Name:    %s\n"+
		"Type:    %s\n"+
		"Email:   %s\n"+
		"Created: %s\n",
		ch.DisplayName, ch.MembershipType, email, created,
	)

	if len(m.channelMembers) > 0 {
		content += "\nMembers:\n"
		for _, member := range m.channelMembers {
			roleIcon := normalItemStyle.Render("Member   ")
			if member.Role == "Owner" {
			    roleIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("Owner ★  ")
			}
			content += fmt.Sprintf("%s%s  %s\n",
				roleIcon,
				member.DisplayName,
				metaStyle.Render(member.Mail),
			)
		}
	} else if m.channelMembers == nil {
		content += "\nMembers: Loading...\n"
	}

	if strings.ToLower(ch.MembershipType) == "private" {
		content += "\n\n" + helpStyle.Render("Press [a] to add a member to this private channel.")
	}

	return content
}

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
				lock := ""
				if strings.ToLower(c.MembershipType) == "private" {
					lock = lipgloss.NewStyle().Foreground(colorMuted).Render(" ")
				}
				leftContent += fmt.Sprintf("%s%s\n", cursor, style.Render(truncateText(c.DisplayName, leftOuterWidth-6))+lock)
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
		statusLine := ""
		if m.downloadStatus != "" {
			statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(m.downloadStatus)
		}
		splashContent := lipgloss.JoinVertical(lipgloss.Center,
			splashLogoStyle.Render(asciiLogo),
			"",
			splashTitleStyle.Render("Microsoft Teams Terminal UI"),
			splashSubStyle.Render("v1.0.0-beta"),
			"",
			splashHintStyle.Render("[↑/↓] Navigate teams  ·  [Enter] Open channel"),
			"",
			splashHintStyle.Render("Loading..."),
			"",
			statusLine,
		)
		if m.ready {
			rightContent = lipgloss.Place(rightInnerWidth, rightInnerHeight, lipgloss.Center, lipgloss.Center, splashContent)
		} else {
			rightContent = "Loading..."
		}
	} else if m.focusLeft && m.loadedConvID == "" && m.workspace != WorkspaceActivity && m.workspace != WorkspaceAssignments {
		// === SPLASH SCREEN ===
		statusLine := ""
		if m.downloadStatus != "" {
			statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(m.downloadStatus)
		}
		splashContent := lipgloss.JoinVertical(lipgloss.Center,
			splashLogoStyle.Render(asciiLogo),
			"",
			splashTitleStyle.Render("Microsoft Teams Terminal UI"),
			splashSubStyle.Render("v1.0.0-beta"),
			"",
			splashHintStyle.Render("[↑/↓] Navigate teams  ·  [Enter] Open channel"),
			"",
			statusLine,
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
			tabChat, tabFiles, tabInfo := renderThreeTabs(m.viewMode, "Posts", "Files", "Info")
			title := fmt.Sprintf("# %s", m.channels[m.selectedChan].DisplayName)
			if m.viewMode == ModeFiles && len(m.folderStack) > 0 {
				for _, node := range m.folderStack {
					title += fmt.Sprintf(" / %s", node.Name)
				}
			}
			header := titleStyle.Render(title)
			rightContent += fmt.Sprintf("%s\n%s %s %s %s %s\n\n", header, tabChat, tabDividerStyle.Render("·"), tabFiles, tabDividerStyle.Render("·"), tabInfo)
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
	if m.showDirPicker {
		return m.dirPicker.View()
	} else if m.confirmingDownload {
		names := make([]string, len(m.downloadTargets))
		for i, t := range m.downloadTargets {
			names[i] = t.Name
		}

		dirLine := fmt.Sprintf("Destination: %s", m.prefs.DownloadDir)
		actions := "[Enter/y] Download   [e] Change folder   [Esc/n] Cancel"

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
	} else if m.showNewDMPopup {
		var popupContent string
		popupContent += titleStyle.Render("New Direct Message") + "\n\n"
		popupContent += m.newDMQuery.View() + "\n\n"

		if m.newDMErr != "" {
			popupContent += errorStyle.Render(m.newDMErr) + "\n"
		} else if len(m.newDMResults) == 0 && len(m.newDMQuery.Value()) >= 2 {
			popupContent += helpStyle.Render("No results found.") + "\n"
		} else {
			for i, u := range m.newDMResults {
				cursor := "  "
				style := normalItemStyle
				if i == m.newDMCursor {
					cursor = "▶ "
					style = selectedItemStyle
				}
				line := fmt.Sprintf("%s%s\n   %s", cursor, style.Render(u.DisplayName), metaStyle.Render(u.Mail))
				popupContent += line + "\n"
			}
		}

		popupContent += "\n" + helpStyle.Render("[↑/↓] Navigate  [Enter] Open DM  [Esc] Cancel")

		popup := popupStyle.Render(popupContent)
		rightPanel = lipgloss.Place(lipgloss.Width(rightPanel), lipgloss.Height(rightPanel), lipgloss.Center, lipgloss.Center, popup)
	} else if m.showCreateTeamPopup {
		var content string
		content += titleStyle.Render("New Team") + "\n\n"
		content += m.createTeamInput.View() + "\n\n"
		if m.createTeamErr != "" {
			content += errorStyle.Render(m.createTeamErr) + "\n\n"
		}
		content += helpStyle.Render("[Enter] Create   [Esc] Cancel")
		popup := popupStyle.Render(content)
		rightPanel = lipgloss.Place(lipgloss.Width(rightPanel), lipgloss.Height(rightPanel), lipgloss.Center, lipgloss.Center, popup)
	} else if m.showCreateChannelPopup {
		var content string
		content += titleStyle.Render("New Channel") + "\n\n"

		if m.createChannelStep == 0 {
			content += helpStyle.Render("Channel name:") + "\n"
			content += m.createChannelInput.View() + "\n\n"
			content += helpStyle.Render("[Enter] Next   [Esc] Cancel")
		} else {
			content += helpStyle.Render("Name: ") + normalItemStyle.Render(m.createChannelInput.Value()) + "\n\n"
			content += helpStyle.Render("Channel type:") + "\n\n"

			types := []string{"Standard", "Private", "Shared"}
			for i, t := range types {
				cursor := "  "
				style := normalItemStyle
				if t == m.createChannelType {
					cursor = "▶ "
					style = selectedItemStyle
				}
				desc := map[string]string{
					"Standard": "Everyone in the team has access",
					"Private":  "Specific people on the team",
					"Shared":   "People inside or outside the org",
				}[t]
				content += fmt.Sprintf("%s[%d] %s\n    %s\n\n", cursor, i+1, style.Render(t), helpStyle.Render(desc))
			}
			content += helpStyle.Render("[1-3] Select type   [Enter] Create   [Esc] Cancel")
		}

		popup := popupStyle.Render(content)
		rightPanel = lipgloss.Place(lipgloss.Width(rightPanel), lipgloss.Height(rightPanel), lipgloss.Center, lipgloss.Center, popup)
	} else if m.showDeleteChannelPopup && m.selectedChan < len(m.channels) {
		ch := m.channels[m.selectedChan].DisplayName
		popupContent := fmt.Sprintf("Delete channel \"%s\"?\n\n[Enter/y] Confirm   [Esc/n] Cancel", ch)
		popup := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#E74C3C")).
			Padding(1, 3).
			Render(popupContent)
		rightPanel = lipgloss.Place(lipgloss.Width(rightPanel), lipgloss.Height(rightPanel), lipgloss.Center, lipgloss.Center, popup)
	} else if m.showDeleteTeamPopup && m.selectedTeam < len(m.teams) {
		team := m.teams[m.selectedTeam].DisplayName
		popupContent := fmt.Sprintf("Delete team \"%s\"?\n\n[Enter/y] Confirm   [Esc/n] Cancel", team)
		popup := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#E74C3C")).
			Padding(1, 3).
			Render(popupContent)
		rightPanel = lipgloss.Place(lipgloss.Width(rightPanel), lipgloss.Height(rightPanel), lipgloss.Center, lipgloss.Center, popup)
	}

	if m.showTeamInfo && m.teamInfo != nil {
		t := m.teamInfo
		archived := "No"
		if t.IsArchived {
			archived = "Yes"
		}
		content := fmt.Sprintf(
			"Team Info\n\n"+
				"Name:        %s\n"+
				"Description: %s\n"+
				"Visibility:  %s\n"+
				"Archived:    %s\n"+
				"Members:     %d  Owners: %d  Guests: %d\n\n"+
				"[Esc] Close",
			t.DisplayName, t.Description, t.Visibility,
			archived, t.Summary.MembersCount, t.Summary.OwnersCount, t.Summary.GuestsCount,
		)
		popup := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#0078D4")).
			Padding(1, 3).
			Width(60).
			Render(content)
		combined := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
		return lipgloss.Place(lipgloss.Width(combined), lipgloss.Height(combined), lipgloss.Center, lipgloss.Center, popup)
	}
	
	if m.showRemoveMemberPopup && m.membersCursor < len(m.teamMembers) {
		member := m.teamMembers[m.membersCursor]
		content := fmt.Sprintf("Remove \"%s\" from team?\n\n[Enter/y] Confirm   [Esc/n] Cancel", member.DisplayName)
		popup := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#E74C3C")).
			Padding(1, 3).
			Render(content)
		combined := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
		return lipgloss.Place(lipgloss.Width(combined), lipgloss.Height(combined), lipgloss.Center, lipgloss.Center, popup)
	}

	if m.showMembersPopup {
		var content string
		content += titleStyle.Render("Team Members") + "\n\n"
		if m.membersLoading {
			content += helpStyle.Render("Loading...")
		} else if len(m.teamMembers) == 0 {
			content += helpStyle.Render("No members found.")
		} else {
			maxVisible := 10
			total := len(m.teamMembers)
			windowStart := m.membersCursor - maxVisible/2
			if windowStart < 0 {
				windowStart = 0
			}
			windowEnd := windowStart + maxVisible
			if windowEnd > total {
				windowEnd = total
				windowStart = total - maxVisible
				if windowStart < 0 {
					windowStart = 0
				}
			}
			if windowStart > 0 {
				content += metaStyle.Render(fmt.Sprintf("  ↑ %d more above\n", windowStart))
			}
			for i := windowStart; i < windowEnd; i++ {
				member := m.teamMembers[i]
				cursor := "  "
				nameStyle := normalItemStyle
				if i == m.membersCursor {
					cursor = "▶ "
					nameStyle = selectedItemStyle
				}
				roleIcon := normalItemStyle.Render("Member   ")
				if member.Role == "Owner" {
				    roleIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("Owner ★  ")
				}
				line := fmt.Sprintf("%s%s%s\n   %s",
					cursor,
					roleIcon,
					nameStyle.Render(member.DisplayName),
					metaStyle.Render(member.Mail),
				)
				content += line + "\n"
			}
			if windowEnd < total {
				content += metaStyle.Render(fmt.Sprintf("  ↓ %d more below\n", total-windowEnd))
			}
		}
		content += "\n" + helpStyle.Render("[↑/↓] Navigate  [a] Add  [x] Remove  [Esc] Close")
		popup := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#0078D4")).
			Padding(1, 3).
			Width(55).
			Render(content)
		combined := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
		return lipgloss.Place(lipgloss.Width(combined), lipgloss.Height(combined), lipgloss.Center, lipgloss.Center, popup)
	}

	if m.showAddChannelMemberPopup {
		var content string
		content += titleStyle.Render("Add to Channel") + "\n\n"
		query := m.addChannelMemberInput.Value()
		content += helpStyle.Render("> ") + query + "█\n\n"
		if m.addChannelMemberErr != "" {
			content += errorStyle.Render(m.addChannelMemberErr) + "\n"
		} else if len(m.addChannelMemberResults) == 0 {
			content += helpStyle.Render("No team members available to add.") + "\n"
		} else {
			for i, u := range m.addChannelMemberResults {
				cursor := "  "
				style := normalItemStyle
				if i == m.addChannelMemberCursor {
					cursor = "▶ "
					style = selectedItemStyle
				}
				content += fmt.Sprintf("%s%s\n   %s\n", cursor, style.Render(u.DisplayName), metaStyle.Render(u.Mail))
			}
		}
		content += "\n" + helpStyle.Render("[↑/↓] Navigate  [Enter] Add  [Esc] Cancel")
		popup := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#0078D4")).
			Padding(1, 3).
			Width(50).
			Render(content)
		combined := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
		return lipgloss.Place(lipgloss.Width(combined), lipgloss.Height(combined), lipgloss.Center, lipgloss.Center, popup)
	}

	if m.showAddMemberPopup {
		var content string
		content += titleStyle.Render("Add Member") + "\n\n"
		content += m.addMemberInput.View() + "\n\n"
		if m.addMemberErr != "" {
			content += errorStyle.Render(m.addMemberErr) + "\n"
		} else if len(m.newDMResults) == 0 && len(m.addMemberInput.Value()) >= 2 {
			content += helpStyle.Render("No results found.") + "\n"
		} else {
			for i, u := range m.newDMResults {
				cursor := "  "
				style := normalItemStyle
				if i == m.newDMCursor {
					cursor = "▶ "
					style = selectedItemStyle
				}
				content += fmt.Sprintf("%s%s\n   %s\n", cursor, style.Render(u.DisplayName), metaStyle.Render(u.Mail))
			}
		}
		content += "\n" + helpStyle.Render("[↑/↓] Navigate  [Enter] Add  [Esc] Cancel")
		popup := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#0078D4")).
			Padding(1, 3).
			Width(50).
			Render(content)
		combined := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
		return lipgloss.Place(lipgloss.Width(combined), lipgloss.Height(combined), lipgloss.Center, lipgloss.Center, popup)
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
		if m.tokenRenewing {
			renewBanner := lipgloss.NewStyle().
				Foreground(colorYellow).Bold(true).
				Render("⟳ Renewing tokens...")
			topBar = renewBanner + "  " + topBar
		} else if m.tokenRenewErr != "" {
			errBanner := lipgloss.NewStyle().
				Foreground(colorRed).Bold(true).
				Render("⚠ Token renewal failed — run ./msTTui-auth")
			topBar = errBanner + "  " + topBar
		}
		if m.teamCreating {
			creatingBanner := lipgloss.NewStyle().
				Foreground(colorMuted).
				Render("⟳ Creating team...")
			topBar = creatingBanner + "  " + topBar
		}
		ui = lipgloss.JoinVertical(lipgloss.Left, topBar, ui)
	}

	// Contextual footer
	footerBorder := lipgloss.NewStyle().
	    BorderForeground(colorBorder).
	    Border(lipgloss.NormalBorder(), true, false, false, false).
	    PaddingTop(0).
	    PaddingLeft(1)
	
	footerLine := footerBorder.Render(m.footerText())
	
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

func renderThreeTabs(current ViewMode, nameA, nameB, nameC string) (string, string, string) {
	var tabA, tabB, tabC string
	
	if current == ModeChat {
		tabA = activeTabStyle.Render("[ " + nameA + " ]")
	} else {
		tabA = inactiveTabStyle.Render("[ " + nameA + " ]")
	}
	
	if current == ModeFiles {
		tabB = activeTabStyle.Render("[ " + nameB + " ]")
	} else {
		tabB = inactiveTabStyle.Render("[ " + nameB + " ]")
	}
	
	if current == ModeInfo {
		tabC = activeTabStyle.Render("[ " + nameC + " ]")
	} else {
		tabC = inactiveTabStyle.Render("[ " + nameC + " ]")
	}
	
	return tabA, tabB, tabC
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
	dim := lipgloss.NewStyle().Foreground(colorMuted)

	workspaceHint := dim.Render("[1-4] Workspace")
	if m.notifLoaded {
		if count := m.unreadCount(); count > 0 {
			badge := lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(fmt.Sprintf("(%d)", count))
			workspaceHint = dim.Render("[1-4] Workspace ") + badge
		}
	}
	switch {
	case m.showCreateChannelPopup && m.createChannelStep == 0:
		return dim.Render(" [Enter] Next   [Esc] Cancel")
	case m.showCreateChannelPopup && m.createChannelStep == 1:
		return dim.Render(" [1] Standard  [2] Private  [3] Shared  [Enter] Create  [Esc] Cancel")
	case m.showCreateTeamPopup:
		return dim.Render(" [Enter] Create team   [Esc] Cancel")
	case m.showNewDMPopup:
		return dim.Render(" [↑/↓] Navigate results  [Enter] Open DM  [Esc] Cancel")
	case m.showPresenceMenu:
		return dim.Render(" [↑/↓] Navigate   [Enter] Confirm   [Esc/q] Cancel")
	case m.showAddChannelMemberPopup:
		return dim.Render(" [↑/↓] Navigate  [Enter] Add  [Esc] Cancel")
	case m.confirmingDownload:
		return dim.Render(" [Enter/y] Download   [e] Change folder   [Esc/n] Cancel")
	case m.workspace == WorkspaceAssignments:
		return dim.Render(" ") + workspaceHint + dim.Render("  [←/→] Filter  [↑/↓] Navigate  [Enter] View  [q] Quit")
	case m.workspace == WorkspaceActivity:
		if !m.focusLeft {
			return dim.Render(" [o] Go to channel  [Esc] Back  [q] Quit")
		}
		return dim.Render(" ") + workspaceHint + dim.Render("  [←/→] Filter  [↑/↓] Navigate  [Enter] View details  [q] Quit")
	case m.focusLeft && m.workspace == WorkspaceDMs:
		return dim.Render(" ") + workspaceHint + dim.Render("  [↑/↓] Navigate  [Enter] Open  [n] New DM  [p] Status  [q] Quit")
	case m.focusLeft && m.workspace == WorkspaceTeams && m.focusList == 1:
		return dim.Render(" [↑/↓] Navigate  [Enter] Open  [C] New channel  [X] Delete channel  [←] Back to teams  [q] Quit")
	case m.focusLeft && m.workspace == WorkspaceTeams && m.focusList == 0:
		return dim.Render(" ") + workspaceHint + dim.Render("  [↑/↓] Nav  [Enter] Open  [I] Info  [M] Members  [L] Link  [N] New  [D] Delete  [p] Status  [q] Quit")
	case m.previewing:
		return dim.Render(" [Esc] Back to files  [↑/↓] Scroll")
	case !m.focusLeft && m.viewMode == ModeFiles:
		return dim.Render(" [↑/↓] Navigate  [Enter] Open  [Space] Select  [v] Preview  [o] Download  [u] Upload  [p] Status  [Esc/h] Back")
	case !m.focusLeft && m.viewMode == ModeChat:
		return dim.Render(" [↑/↓] Scroll  [i] Type  [u] Upload  [f] Files  [I] Info  [p] Status  [Esc/h] Back")
	case !m.focusLeft && m.viewMode == ModeInfo:
		if m.channelInfo != nil && strings.ToLower(m.channelInfo.MembershipType) == "private" {
			return dim.Render(" [↑/↓] Scroll  [a] Add member  [f] Files  [I] Chat  [p] Status  [Esc/h] Back")
		}
		return dim.Render(" [↑/↓] Scroll  [f] Files  [I] Chat  [p] Status  [Esc/h] Back")
	default:
		return dim.Render(" ") + workspaceHint + dim.Render("  [↑/↓] Navigate  [Enter] Open  [p] Status  [q] Quit")
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
	filterNames := []string{"All", "Unread", "Overdue", "Upcoming"}
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

		unreadDot := ""
		if !n.IsRead {
			unreadDot = lipgloss.NewStyle().Foreground(colorRed).Render("● ")
		}
		line := fmt.Sprintf("%s%s%s%s %s\n   %s", cursor, unreadDot, label, style.Render(title), metaStyle.Render(age), preview)
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
		case FilterUnread:
			if !n.IsRead {
				result = append(result, n)
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
