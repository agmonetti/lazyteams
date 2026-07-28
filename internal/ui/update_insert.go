package ui

import (
	"strings"
	"teamsTUI/internal/graph"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleInsertMode(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If the popup is open, arrows and enter go to the popup
		if m.showMentionPopup {
			switch msg.String() {
			case "up":
				if m.mentionCursor > 0 {
					m.mentionCursor--
				}
				return m, nil, true
			case "down":
				if m.mentionCursor < len(m.mentionSuggestions)-1 {
					m.mentionCursor++
				}
				return m, nil, true
			case "enter", "tab":
				if m.mentionCursor >= len(m.mentionSuggestions) {
					m.showMentionPopup = false
					m.mentionSuggestions = nil
					m.mentionCursor = 0
					return m, nil, true
				}
				selected := m.mentionSuggestions[m.mentionCursor]
				// Replace from the @ to the end with the full name
				v := m.input.Value()
				newVal := v[:m.mentionAtPos] + "@" + selected.DisplayName + " "
				m.input.SetValue(newVal)
				// Move cursor to the end
				m.input.CursorEnd()
				m.showMentionPopup = false
				m.mentionSuggestions = nil
				m.mentionCursor = 0
				if m.ready {
					rightInnerHeight := m.height - 6 - 2
					if rightInnerHeight < 1 {
						rightInnerHeight = 1
					}
					inputHeight := strings.Count(m.input.View(), "\n") + 1
					newVpHeight := rightInnerHeight - 4 - inputHeight
					if newVpHeight < 5 {
						newVpHeight = 5
					}
					m.viewport.Height = newVpHeight
				}
				return m, nil, true
			case "esc":
				m.showMentionPopup = false
				if m.ready {
					rightInnerHeight := m.height - 6 - 2
					inputHeight := strings.Count(m.input.View(), "\n") + 1
					newVpHeight := rightInnerHeight - 4 - inputHeight
					if newVpHeight < 5 {
						newVpHeight = 5
					}
					m.viewport.Height = newVpHeight
				}
				return m, nil, true
			}
		}
		switch msg.String() {
		case "ctrl+p": // Paste image from clipboard
			m.downloadStatus = "Uploading image from clipboard..."
			m.downloadStatusID++
			v := m.input.Value()
			return m, tea.Batch(
				pasteImageCmd(m.client, m.activeConversationID(), v),
				clearStatusAfter(m.downloadStatusID),
			), true
		case "esc": // Exit insert mode
			m.isTyping = false
			m.input.Blur()
			m.input.Reset()
			// Restore viewport height
			if m.ready {
				rightInnerHeight := m.height - 6 - 2
				m.viewport.Height = rightInnerHeight - 4 - 1
			}
			return m, nil, true
		case "enter": // Send message
			v := m.input.Value()
			if v != "" && m.activeConversationID() != "" {
				m.input.Reset()
				m.isTyping = false
				m.input.Blur()
				if m.ready {
					rightInnerHeight := m.height - 6 - 2
					m.viewport.Height = rightInnerHeight - 4 - 1
				}
				m.loading = true
				mentions := resolveMentions(v, m.buildMemberIndex())
				return m, sendMessageCmd(m.client, m.activeConversationID(), v, mentions), true
			}
		}
	}

	// Pass all other keys to the input
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	// Mention detection
	v := m.input.Value()
	m.showMentionPopup = false
	if idx := strings.LastIndex(v, "@"); idx != -1 {
		query := v[idx+1:]
		if !strings.Contains(query, " ") || m.showMentionPopup {
			q := strings.ToLower(query)
			var suggestions []graph.TeamMember
			for _, tm := range m.teamMembers {
				if strings.Contains(strings.ToLower(tm.DisplayName), q) {
					suggestions = append(suggestions, tm)
				}
			}
			if m.workspace == WorkspaceDMs && m.selectedChat < len(m.chats) {
				for _, member := range m.chats[m.selectedChat].Members {
					if member.UserID != m.selfID && strings.Contains(strings.ToLower(member.DisplayName), q) {
						suggestions = append(suggestions, graph.TeamMember{
							DisplayName: member.DisplayName,
							ID:          member.UserID,
						})
					}
				}
			}
			if len(suggestions) > 0 {
				m.showMentionPopup = true
				m.mentionQuery = query
				m.mentionAtPos = idx
				m.mentionSuggestions = suggestions
				if m.mentionCursor >= len(suggestions) {
					m.mentionCursor = 0
				}
			}
		}
	}

	// Adjust viewport height dynamically as textarea grows
	if m.ready {
		rightInnerHeight := m.height - 6 - 2
		inputHeight := strings.Count(m.input.View(), "\n") + 1

		popupHeight := 0
		if m.showMentionPopup && len(m.mentionSuggestions) > 0 {
			lines := len(m.mentionSuggestions)
			if lines > 5 {
				lines = 5
			}
			popupHeight = lines + 2 // borders
		}

		newVpHeight := rightInnerHeight - 4 - inputHeight - popupHeight
		if newVpHeight < 5 {
			newVpHeight = 5 // minimum safety height
		}
		m.viewport.Height = newVpHeight
	}

	return m, tea.Batch(cmds...), true
}
