package ui

import (
	"strings"
	"lazyteams/internal/graph"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleInsertMode(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case clipboardImageLoadedMsg:
		if len(m.pendingImages) > 0 {
			// Replace placeholder with real data
			m.pendingImages[len(m.pendingImages)-1] = PendingImage{
				Data:        msg.Data,
				ContentType: msg.ContentType,
			}
		} else {
			m.pendingImages = append(m.pendingImages, PendingImage{
				Data:        msg.Data,
				ContentType: msg.ContentType,
			})
		}
		m.downloadStatus = "Image staged successfully"
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID), true

	case clipboardImageErrMsg:
		// Remove placeholder on error
		if len(m.pendingImages) > 0 {
			m.pendingImages = m.pendingImages[:len(m.pendingImages)-1]
		}
		m.downloadStatus = "Clipboard image error: " + msg.err.Error()
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID), true

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
				m.recalculateViewportHeight()
				return m, nil, true
			case "esc":
				m.showMentionPopup = false
				m.recalculateViewportHeight()
				return m, nil, true
			}
		}
		switch msg.String() {
		case "ctrl+p": // Stage image from clipboard
			// Add a placeholder immediately so viewport height adjusts
			// before the async clipboard read completes
			m.pendingImages = append(m.pendingImages, PendingImage{
				Data:        nil,
				ContentType: "image/png",
			})
			return m, readClipboardImageCmd(), true
		case "ctrl+v": // Block paste to avoid breaking the textarea
			return m, nil, true
		case "esc":
			if m.replyToMsg != nil {
				m.replyToMsg = nil
				m.recalculateViewportHeight()
				return m, nil, true
			}
			// Exit insert mode
			m.isTyping = false
			m.input.Blur()
			m.input.Reset()
			m.pendingImages = nil
			m.recalculateViewportHeight()
			return m, nil, true
		case "enter": // Send message
			v := m.input.Value()

			if (v != "" || len(m.pendingImages) > 0) && m.activeConversationID() != "" {
				pending := m.pendingImages
				replyTo := m.replyToMsg

				m.input.Reset()
				m.pendingImages = nil
				m.replyToMsg = nil
				m.isTyping = false
				m.input.Blur()

				m.recalculateViewportHeight()

				m.loading = true
				mentions := resolveMentions(v, m.buildMemberIndex())

				if len(pending) > 0 {
					return m, sendPendingMessageCmd(
						m.client,
						m.activeConversationID(),
						v,
						pending,
						mentions,
						replyTo,
					), true
				}

				return m, sendMessageCmd(
					m.client,
					m.activeConversationID(),
					v,
					mentions,
					replyTo,
				), true
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
	m.recalculateViewportHeight()

	_, isKey := msg.(tea.KeyMsg)
	return m, tea.Batch(cmds...), isKey
}
