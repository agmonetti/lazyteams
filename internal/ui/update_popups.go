package ui

import (
	"fmt"
	"github.com/agmonetti/lazyteams/internal/graph"
	"github.com/agmonetti/lazyteams/internal/ui/components/directorypicker"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) handleHelpPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc", "q", "?", "enter", "space":
		m.showHelp = false
	}
	return m, nil, true
}

func (m Model) handleDeleteMsgPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "y", "enter":
		return m, deleteMessageCmd(m.client, m.activeConversationID(), m.deleteMsgID), true
	case "n", "esc":
		m.showDeleteMsgPopup = false
	}
	return m, nil, true
}

func (m Model) handleEditPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.showEditPopup = false
		m.editInput.Reset()
	case "enter":
		content := strings.TrimSpace(m.editInput.Value())
		if content != "" {
			return m, editMessageCmd(m.client, m.activeConversationID(), m.editMessageID, content), true
		}
	default:
		var cmd tea.Cmd
		m.editInput, cmd = m.editInput.Update(msg)
		return m, cmd, true
	}
	return m, nil, true
}

func (m Model) handleReactionPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.showReactionPicker = false
		return m, nil, true
	case "up", "k":
		if m.reactionCursor > 0 {
			m.reactionCursor--
		}
	case "down", "j":
		if m.reactionCursor < len(m.reactionOptions)-1 {
			m.reactionCursor++
		}
	case "enter":
		m.showReactionPicker = false
		m.reactionPending = false
		key := m.reactionOptions[m.reactionCursor]
		var targetMsg *graph.Message
		for i := range m.messages {
			if m.messages[i].ID == m.reactionTargetID {
				targetMsg = &m.messages[i]
				break
			}
		}
		if targetMsg != nil {
			alreadyReacted := false
			for _, r := range targetMsg.Reactions {
				if r.Key == key && r.UserReacted {
					alreadyReacted = true
					break
				}
			}
			// Optimistic update
			for i := range m.messages {
				if m.messages[i].ID == m.reactionTargetID {
					for j := range m.messages[i].Reactions {
						if m.messages[i].Reactions[j].Key == key {
							if alreadyReacted {
								m.messages[i].Reactions[j].UserReacted = false
								m.messages[i].Reactions[j].Count--
							} else {
								m.messages[i].Reactions[j].UserReacted = true
								m.messages[i].Reactions[j].Count++
							}
							break
						}
					}
					break
				}
			}
			if alreadyReacted {
				return m, removeReactionCmd(m.client, m.activeConversationID(), m.reactionTargetID, key), true
			}
			return m, addReactionCmd(m.client, m.activeConversationID(), m.reactionTargetID, key), true
		}
	}
	return m, nil, true
}

func (m Model) handleThreadView(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	// If typing, pass FIRST to input
	if m.isReplyTyping {
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
				v := m.input.Value()
				newVal := v[:m.mentionAtPos] + "@" + selected.DisplayName + " "
				m.input.SetValue(newVal)
				m.input.CursorEnd()
				m.showMentionPopup = false
				m.mentionSuggestions = nil
				m.mentionCursor = 0
				if m.ready {
					rightInnerHeight := m.height - 6 - 2
					inputHeight := strings.Count(m.input.View(), "\n") + 1
					m.threadViewport.Height = rightInnerHeight - 10 - inputHeight
				}
				return m, nil, true
			case "esc":
				m.showMentionPopup = false
				if m.ready {
					rightInnerHeight := m.height - 6 - 2
					inputHeight := strings.Count(m.input.View(), "\n") + 1
					m.threadViewport.Height = rightInnerHeight - 10 - inputHeight
				}
				return m, nil, true
			}
		}

		switch msg.String() {
		case "esc":
			m.isReplyTyping = false
			m.input.Blur()
			m.input.Reset()
			// Restore threadViewport
			if m.ready {
				rightInnerHeight := m.height - 6 - 2
				m.threadViewport.Height = rightInnerHeight - 12
			}
			return m, nil, true
		case "enter":
			v := strings.TrimSpace(m.input.Value())
			if v != "" {
				m.input.Reset()
				m.isReplyTyping = false
				m.input.Blur()
				// Restore threadViewport
				if m.ready {
					rightInnerHeight := m.height - 6 - 2
					m.threadViewport.Height = rightInnerHeight - 12
				}
				mentions := resolveMentions(v, m.buildMemberIndex())
				return m, sendReplyCmd(m.client, m.activeConversationID(), m.threadParentID, v, mentions), true
			}
			return m, nil, true
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)

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

				m.threadViewport.Height = rightInnerHeight - 10 - inputHeight - popupHeight
			}
			return m, cmd, true
		}
	}
	// If not typing, handle thread navigation
	switch msg.String() {
	case "esc":
		m.showThread = false
		m.threadParentID = ""
		m.cursorMode = false
		return m, nil, true
	case "i", "r":
		m.isReplyTyping = true
		m.input.Focus()
		if m.ready {
			rightInnerHeight := m.height - 6 - 2
			inputHeight := strings.Count(m.input.View(), "\n") + 1
			m.threadViewport.Height = rightInnerHeight - 10 - inputHeight
		}
		return m, nil, true
	case "up", "k":
		if m.threadCursor > 0 {
			m.threadCursor--
			replies := repliesFor(m.messages, m.threadParentID)
			content := formatThread(m.threadParentMsg, replies, m.threadViewport.Width, m.userName, m.threadCursor, true)
			m.threadViewport.SetContent(content)
		} else {
			m.threadViewport.LineUp(1)
		}
		return m, nil, true
	case "down", "j":
		replies := repliesFor(m.messages, m.threadParentID)
		if m.threadCursor < len(replies) {
			m.threadCursor++
			content := formatThread(m.threadParentMsg, replies, m.threadViewport.Width, m.userName, m.threadCursor, true)
			m.threadViewport.SetContent(content)
		} else {
			m.threadViewport.LineDown(1)
		}
		return m, nil, true
	case "e":
		replies := repliesFor(m.messages, m.threadParentID)
		var targetID string
		if m.threadCursor == 0 {
			targetID = m.threadParentID
		} else if m.threadCursor-1 < len(replies) {
			targetID = replies[m.threadCursor-1].ID
		}
		if targetID != "" {
			m.reactionTargetID = targetID
			m.loading = true
			return m, getReactionsCmd(m.client, m.activeConversationID(), targetID, m.selfID), true
		}
		return m, nil, true
	case "E":
		replies := repliesFor(m.messages, m.threadParentID)
		var targetMsg *graph.Message
		if m.threadCursor == 0 {
			targetMsg = &m.threadParentMsg
		} else if m.threadCursor-1 < len(replies) {
			targetMsg = &replies[m.threadCursor-1]
		}
		if targetMsg != nil && ((m.selfID != "" && strings.HasSuffix(targetMsg.FromUserID, m.selfID)) ||
			(targetMsg.FromName == "User" || targetMsg.FromName == m.userName)) {
			m.editMessageID = targetMsg.ID
			m.editOriginalBody = targetMsg.RawBody
			m.editInput.SetValue(cleanHTMLForEdit(targetMsg.RawBody))
			m.editInput.Focus()
			m.showEditPopup = true
		}
		return m, nil, true
	case "backspace", "delete":
		replies := repliesFor(m.messages, m.threadParentID)
		var targetMsg *graph.Message
		if m.threadCursor == 0 {
			targetMsg = &m.threadParentMsg
		} else if m.threadCursor-1 < len(replies) {
			targetMsg = &replies[m.threadCursor-1]
		}
		if targetMsg != nil && ((m.selfID != "" && strings.HasSuffix(targetMsg.FromUserID, m.selfID)) ||
			(targetMsg.FromName == "User" || targetMsg.FromName == m.userName)) {
			m.deleteMsgID = targetMsg.ID
			m.showDeleteMsgPopup = true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) handleCursorModeTeams(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.cursorMode = false
		m.messageCursor = 0
		content := formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		m.viewport.SetContent(content)
		return m, nil, true
	case "e":
		if m.reactionPending {
			return m, nil, true
		}
		m.reactionPending = true
		rootMsgs := rootMessages(m.messages)
		if m.messageCursor < len(rootMsgs) {
			m.reactionTargetID = rootMsgs[m.messageCursor].ID
			m.loading = true
			return m, getReactionsCmd(m.client, m.activeConversationID(), m.reactionTargetID, m.selfID), true
		}
		return m, nil, true
	case "E":
		rootMsgs := rootMessages(m.messages)
		if m.messageCursor < len(rootMsgs) {
			selected := rootMsgs[m.messageCursor]
			if (m.selfID != "" && strings.HasSuffix(selected.FromUserID, m.selfID)) ||
				(selected.FromName == "User" || selected.FromName == m.userName) {
				m.editMessageID = selected.ID
				m.editOriginalBody = selected.RawBody
				m.editInput.SetValue(cleanHTMLForEdit(selected.RawBody))
				m.editInput.Focus()
				m.showEditPopup = true
			}
		}
		return m, nil, true
	case "backspace", "delete":
		rootMsgs := rootMessages(m.messages)
		if m.messageCursor < len(rootMsgs) {
			selected := rootMsgs[m.messageCursor]
			if (m.selfID != "" && strings.HasSuffix(selected.FromUserID, m.selfID)) ||
				(selected.FromName == "User" || selected.FromName == m.userName) {
				m.deleteMsgID = selected.ID
				m.showDeleteMsgPopup = true
			}
		}
		return m, nil, true
	case "down", "k":
		if m.messageCursor > 0 {
			m.messageCursor--
		}
		content := formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		// Scroll to cursor
		cursorLine := 0
		for i, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, symCursor) {
				cursorLine = i
				break
			}
		}
		m.viewport.SetContent(content)
		visibleTop := m.viewport.YOffset
		visibleBottom := m.viewport.YOffset + m.viewport.Height
		if cursorLine < visibleTop+2 {
			m.viewport.SetYOffset(cursorLine - 2)
		} else if cursorLine > visibleBottom-3 {
			m.viewport.SetYOffset(cursorLine - m.viewport.Height + 3)
		}
		return m, nil, true
	case "up", "j":
		rootMsgs := rootMessages(m.messages)
		if m.messageCursor < len(rootMsgs)-1 {
			m.messageCursor++
		}
		content := formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		// Scroll to cursor
		cursorLine := 0
		for i, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, symCursor) {
				cursorLine = i
				break
			}
		}
		m.viewport.SetContent(content)
		visibleTop := m.viewport.YOffset
		visibleBottom := m.viewport.YOffset + m.viewport.Height
		if cursorLine < visibleTop+2 {
			m.viewport.SetYOffset(cursorLine - 2)
		} else if cursorLine > visibleBottom-3 {
			m.viewport.SetYOffset(cursorLine - m.viewport.Height + 3)
		}
		return m, nil, true
	case "o":
		rootMsgs := rootMessages(m.messages)
		if m.messageCursor < len(rootMsgs) {
			link := messageLink(rootMsgs[m.messageCursor])
			if link != "" {
				openBrowser(link)
				m.downloadStatus = "✓ Opening link in browser"
			} else {
				m.downloadStatus = "✗ No link in this message"
			}
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID), true
		}
		return m, nil, true
	case "enter":
		rootMsgs := rootMessages(m.messages)
		if m.messageCursor < len(rootMsgs) {
			selected := rootMsgs[m.messageCursor]
			m.threadParentID = selected.ID
			m.threadParentMsg = selected
			m.showThread = true
			m.threadCursor = 0
			// Render thread content
			replies := repliesFor(m.messages, selected.ID)
			content := formatThread(selected, replies, m.threadViewport.Width, m.userName, m.threadCursor, true)
			m.threadViewport.SetContent(content)
			m.threadViewport.GotoTop()
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) handleCursorModeDMs(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "o":
		validMsgs := validDMMessages(m.messages)
		if m.messageCursor < len(validMsgs) {
			link := messageLink(validMsgs[m.messageCursor])
			if link != "" {
				openBrowser(link)
				m.downloadStatus = "✓ Opening link in browser"
			} else {
				m.downloadStatus = "✗ No link in this message"
			}
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID), true
		}
		return m, nil, true
	case "enter":
		validMsgs := validDMMessages(m.messages)
		if m.messageCursor < len(validMsgs) {
			target := validMsgs[m.messageCursor]
			m.replyToMsg = &target
			m.cursorMode = false
			m.isTyping = true
			m.input.Focus()
			m.recalculateViewportHeight()
			return m, nil, true
		}
		return m, nil, true
	case "esc":
		m.cursorMode = false
		m.messageCursor = 0
		content := formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		m.viewport.SetContent(content)
		return m, nil, true
	case "up", "k":
		validMsgs := validDMMessages(m.messages)
		if m.messageCursor < len(validMsgs)-1 {
			m.messageCursor++
		}
		content := formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		cursorLine := 0
		for i, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, symCursor) {
				cursorLine = i
				break
			}
		}
		m.viewport.SetContent(content)
		visibleTop := m.viewport.YOffset
		visibleBottom := m.viewport.YOffset + m.viewport.Height
		if cursorLine < visibleTop+2 {
			m.viewport.SetYOffset(cursorLine - 2)
		} else if cursorLine > visibleBottom-3 {
			m.viewport.SetYOffset(cursorLine - m.viewport.Height + 3)
		}
		return m, nil, true
	case "down", "j":
		if m.messageCursor > 0 {
			m.messageCursor--
		}
		content := formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		cursorLine := 0
		for i, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, symCursor) {
				cursorLine = i
				break
			}
		}
		m.viewport.SetContent(content)
		visibleTop := m.viewport.YOffset
		visibleBottom := m.viewport.YOffset + m.viewport.Height
		if cursorLine < visibleTop+2 {
			m.viewport.SetYOffset(cursorLine - 2)
		} else if cursorLine > visibleBottom-3 {
			m.viewport.SetYOffset(cursorLine - m.viewport.Height + 3)
		}
		return m, nil, true
	case "e":
		if m.reactionPending {
			return m, nil, true
		}
		m.reactionPending = true
		validMsgs := validDMMessages(m.messages)
		if m.messageCursor < len(validMsgs) {
			target := validMsgs[m.messageCursor]
			m.reactionTargetID = target.ID
			m.loading = true
			return m, getReactionsCmd(m.client, m.activeConversationID(), target.ID, m.selfID), true
		}
		return m, nil, true
	case "E":
		validMsgs := validDMMessages(m.messages)
		if m.messageCursor < len(validMsgs) {
			selected := validMsgs[m.messageCursor]
			if (m.selfID != "" && strings.HasSuffix(selected.FromUserID, m.selfID)) ||
				(selected.FromName == "User" || selected.FromName == m.userName) {
				m.editMessageID = selected.ID
				m.editOriginalBody = selected.RawBody
				m.editInput.SetValue(cleanHTMLForEdit(selected.RawBody))
				m.editInput.Focus()
				m.showEditPopup = true
			}
		}
		return m, nil, true
	case "backspace", "delete":
		validMsgs := validDMMessages(m.messages)
		if m.messageCursor < len(validMsgs) {
			selected := validMsgs[m.messageCursor]
			if (m.selfID != "" && strings.HasSuffix(selected.FromUserID, m.selfID)) ||
				(selected.FromName == "User" || selected.FromName == m.userName) {
				m.deleteMsgID = selected.ID
				m.showDeleteMsgPopup = true
			}
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) handleSearching(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.isSearching = false
		m.searchInput.Reset()
		m.searchQuery = ""
		m.searchCursor = 0
		m.searchMatchCount = 0

		// Re-render chat
		var content string
		if m.workspace == WorkspaceDMs {
			content = formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		} else {
			content = formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		}
		m.viewport.SetContent(content)
		return m, nil, true
	case "enter":
		m.isSearching = false
		return m, nil, true
	case "up":
		if m.searchCursor > 0 {
			m.searchCursor--
			m.viewport.SetContent(m.renderSearchResults())
			m.viewport.GotoTop()
		}
		return m, nil, true
	case "down":
		if m.searchCursor < m.searchMatchCount-1 {
			m.searchCursor++
			m.viewport.SetContent(m.renderSearchResults())
			m.viewport.GotoTop()
		}
		return m, nil, true
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd, true
	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)

		m.searchQuery = strings.TrimSpace(m.searchInput.Value())
		m.searchCursor = 0
		m.searchMatchCount = m.countSearchMatches(m.messages, m.searchQuery)

		m.viewport.SetContent(m.renderSearchResults())
		return m, cmd, true
	}
}

// renderSearchResults renders the filtered message list during a search,
// applying the search highlight and an empty state when nothing matches.
func (m Model) renderSearchResults() string {
	filtered := m.messages
	if m.searchQuery != "" {
		filtered = m.filterMessages(m.messages, m.searchQuery)
	}
	if len(filtered) == 0 && m.searchQuery != "" {
		empty := helpStyle.Render(fmt.Sprintf("No results found for %q", m.searchQuery))
		return lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center, empty)
	}
	if m.workspace == WorkspaceDMs {
		return formatMessagesDM(filtered, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode, m.activeSearchCursor())
	}
	return formatMessagesWithCursor(filtered, m.viewport.Width, m.messageCursor, m.cursorMode, m.activeSearchCursor())
}

func (m Model) handleDirPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	var cmd tea.Cmd
	m.dirPicker, cmd = m.dirPicker.Update(msg)
	if cmd != nil {
		return m, cmd, true
	}
	// Check if picker sent a result
	return m, nil, true
}

func (m Model) handleCreateTeamPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.showCreateTeamPopup = false
		m.createTeamInput.Reset()
		m.createTeamErr = ""
		return m, nil, true
	case "enter":
		name := strings.TrimSpace(m.createTeamInput.Value())
		if name == "" {
			return m, nil, true
		}
		m.showCreateTeamPopup = false
		m.createTeamInput.Reset()
		m.teamCreating = true
		return m, createTeamCmd(m.client, name), true
	default:
		var cmd tea.Cmd
		m.createTeamInput, cmd = m.createTeamInput.Update(msg)
		return m, cmd, true
	}
}

func (m Model) handleTeamInfo(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if msg.String() == "esc" || msg.String() == "enter" {
		m.showTeamInfo = false
		m.teamInfo = nil
	}
	return m, nil, true
}

func (m Model) handleAddChannelMemberPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.showAddChannelMemberPopup = false
		m.addChannelMemberInput.Reset()
		m.addChannelMemberResults = nil
		m.addChannelMemberErr = ""
	case "up":
		if m.addChannelMemberCursor > 0 {
			m.addChannelMemberCursor--
		}
	case "down":
		if m.addChannelMemberCursor < len(m.addChannelMemberResults)-1 {
			m.addChannelMemberCursor++
		}
	case "home":
		m.addChannelMemberCursor = 0
	case "end":
		if len(m.addChannelMemberResults) > 0 {
			m.addChannelMemberCursor = len(m.addChannelMemberResults) - 1
		}
	case "enter":
		if len(m.addChannelMemberResults) > 0 {
			target := m.addChannelMemberResults[m.addChannelMemberCursor]
			m.showAddChannelMemberPopup = false
			m.addChannelMemberInput.Reset()
			m.addChannelMemberResults = nil
			teamGUID := m.teams[m.selectedTeam].ID
			channelID := m.channels[m.selectedChan].ID
			return m, addChannelMemberCmd(m.client, teamGUID, channelID, target), true
		}
	default:
		// Handle text input manually (no Focus())
		s := msg.String()
		if s == "backspace" || s == "ctrl+h" {
			v := m.addChannelMemberInput.Value()
			if len(v) > 0 {
				m.addChannelMemberInput.SetValue(v[:len(v)-1])
			}
		} else if len(s) == 1 && s[0] >= 32 && s[0] < 127 {
			m.addChannelMemberInput.SetValue(m.addChannelMemberInput.Value() + s)
		}
		// Filter team members by query
		q := strings.ToLower(strings.TrimSpace(m.addChannelMemberInput.Value()))
		excluded := make(map[string]bool)
		for _, cm := range m.channelMembers {
			excluded[cm.ID] = true
		}
		var filtered []graph.TeamMember
		for _, tm := range m.teamMembers {
			if excluded[tm.ID] {
				continue
			}
			if q == "" || strings.Contains(strings.ToLower(tm.DisplayName), q) || strings.Contains(strings.ToLower(tm.Mail), q) {
				filtered = append(filtered, tm)
			}
		}
		m.addChannelMemberResults = filtered
		m.addChannelMemberCursor = 0
	}
	return m, nil, true
}

func (m Model) handleAddMemberPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg.String() {
	case "esc":
		m.showAddMemberPopup = false
		m.addMemberInput.Reset()
		m.newDMResults = nil
		m.addMemberErr = ""
	case "up":
		if m.newDMCursor > 0 {
			m.newDMCursor--
		}
	case "down":
		if m.newDMCursor < len(m.newDMResults)-1 {
			m.newDMCursor++
		}
	case "home":
		m.newDMCursor = 0
	case "end":
		if len(m.newDMResults) > 0 {
			m.newDMCursor = len(m.newDMResults) - 1
		}
	case "enter":
		if len(m.newDMResults) > 0 {
			target := m.newDMResults[m.newDMCursor]
			m.showAddMemberPopup = false
			m.addMemberInput.Reset()
			m.newDMResults = nil
			m.addMemberErr = ""
			teamGUID := m.teams[m.selectedTeam].ID
			mri := "8:orgid:" + target.ID
			return m, addMemberCmd(m.client, m.teamThreadID, teamGUID, mri), true
		}
	default:
		var cmd tea.Cmd
		m.addMemberInput, cmd = m.addMemberInput.Update(msg)
		cmds = append(cmds, cmd)
		q := strings.TrimSpace(m.addMemberInput.Value())
		if len(q) >= 2 {
			cmds = append(cmds, searchUsersCmd(m.client, q))
		} else {
			m.newDMResults = nil
		}
		return m, tea.Batch(cmds...), true
	}
	return m, nil, true
}

func (m Model) handleRemoveMemberPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "y", "enter":
		member := m.teamMembers[m.membersCursor]
		teamGUID := m.teams[m.selectedTeam].ID
		return m, removeMemberCmd(m.client, m.teamThreadID, teamGUID, member.ID), true
	case "n", "esc":
		m.showRemoveMemberPopup = false
	}
	return m, nil, true
}

func (m Model) handleRemoveChannelMemberPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "y", "enter":
		if m.channelMemberCursor < len(m.channelMembers) {
			member := m.channelMembers[m.channelMemberCursor]
			if member.ID != m.selfID {
				teamGUID := m.teams[m.selectedTeam].ID
				channelID := m.channels[m.selectedChan].ID
				return m, removeChannelMemberCmd(m.client, teamGUID, channelID, member.ID), true
			}
		}
	case "n", "esc":
		m.showRemoveChannelMemberPopup = false
	}
	return m, nil, true
}

func (m Model) handleChangeChannelRolePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if m.changeChannelRoleCursor > 0 {
			m.changeChannelRoleCursor--
		}
	case "down", "j":
		if m.changeChannelRoleCursor < 1 {
			m.changeChannelRoleCursor++
		}
	case "enter":
		if m.channelMemberCursor < len(m.channelMembers) {
			member := m.channelMembers[m.channelMemberCursor]
			role := "Owner"
			if m.changeChannelRoleCursor == 1 {
				role = "Member"
			}
			if member.Role == role {
				m.showChangeChannelRolePopup = false
				return m, nil, true
			}
			m.showChangeChannelRolePopup = false
			// The Teams Fabric API does not enforce the last-owner restriction,
			// so block demoting the only owner here instead of relying on it.
			if role == "Member" && isLastChannelOwner(m.channelMembers, member.ID) {
				m.channelRoleErr = "Cannot demote the only owner."
				if m.viewMode == ModeInfo {
					m.viewport.SetContent(renderInfoContent(&m))
				}
				return m, nil, true
			}
			m.channelRoleErr = ""
			teamGUID := m.teams[m.selectedTeam].ID
			channelID := m.channels[m.selectedChan].ID
			return m, updateChannelMemberRoleCmd(m.client, teamGUID, channelID, member, role), true
		}
	case "esc":
		m.showChangeChannelRolePopup = false
	}
	return m, nil, true
}

func (m Model) handleMembersPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.showMembersPopup = false
		m.teamMembers = nil
		m.membersCursor = 0
	case "up", "k":
		if m.membersCursor > 0 {
			m.membersCursor--
		}
	case "down", "j":
		if m.membersCursor < len(m.teamMembers)-1 {
			m.membersCursor++
		}
	case "g", "home":
		m.membersCursor = 0
	case "G", "end":
		if len(m.teamMembers) > 0 {
			m.membersCursor = len(m.teamMembers) - 1
		}
	case "a":
		m.showMembersPopup = false
		m.showAddMemberPopup = true
		m.addMemberInput.Reset()
		m.newDMResults = nil
		m.addMemberErr = ""
		m.newDMCursor = 0
		m.addMemberInput.Focus()
	case "x", "X":
		if len(m.teamMembers) > 0 {
			member := m.teamMembers[m.membersCursor]
			if member.ID != m.selfID {
				m.showRemoveMemberPopup = true
			}
		}
	}
	return m, nil, true
}

func (m Model) handleDeleteChannelPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "y", "enter":
		m.showDeleteChannelPopup = false
		channelID := m.channels[m.selectedChan].ID
		return m, deleteChannelCmd(m.client, m.teamThreadID, channelID), true
	case "n", "esc":
		m.showDeleteChannelPopup = false
	}
	return m, nil, true
}

func (m Model) handleDeleteTeamPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "y", "enter":
		m.showDeleteTeamPopup = false
		return m, deleteTeamCmd(m.client, m.teamThreadID), true
	case "n", "esc":
		m.showDeleteTeamPopup = false
	}
	return m, nil, true
}

func (m Model) handleCreateChannelPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.showCreateChannelPopup = false
		m.createChannelInput.Reset()
		m.createChannelErr = ""
		m.createChannelStep = 0
		return m, nil, true
	case "enter":
		if m.createChannelStep == 0 {
			name := strings.TrimSpace(m.createChannelInput.Value())
			if name == "" {
				return m, nil, true
			}
			m.createChannelStep = 1
			m.createChannelInput.Blur()
			return m, nil, true
		}
		// Step 1: type already selected, confirm
		name := strings.TrimSpace(m.createChannelInput.Value())
		m.showCreateChannelPopup = false
		m.createChannelInput.Reset()
		m.createChannelStep = 0
		teamGUID := m.teams[m.selectedTeam].ID
		return m, createChannelCmd(m.client, teamGUID, m.teamThreadID, name, m.createChannelType), true
	case "1":
		if m.createChannelStep == 1 {
			m.createChannelType = "Standard"
		}
	case "2":
		if m.createChannelStep == 1 {
			m.createChannelType = "Private"
		}
	case "3":
		if m.createChannelStep == 1 {
			m.createChannelType = "Shared"
		}
	default:
		if m.createChannelStep == 0 {
			var cmd tea.Cmd
			m.createChannelInput, cmd = m.createChannelInput.Update(msg)
			return m, cmd, true
		}
	}
	return m, nil, true
}

func (m Model) handleNewDMPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg.String() {
	case "esc":
		m.showNewDMPopup = false
		m.newDMQuery.Reset()
		m.newDMResults = nil
		m.newDMErr = ""
		return m, nil, true
	case "up":
		if m.newDMCursor > 0 {
			m.newDMCursor--
		}
	case "down":
		if m.newDMCursor < len(m.newDMResults)-1 {
			m.newDMCursor++
		}
	case "home":
		m.newDMCursor = 0
	case "end":
		if len(m.newDMResults) > 0 {
			m.newDMCursor = len(m.newDMResults) - 1
		}
	case "enter":
		if len(m.newDMResults) > 0 {
			target := m.newDMResults[m.newDMCursor]
			m.showNewDMPopup = false
			m.newDMQuery.Reset()
			m.newDMResults = nil
			return m, createDMCmd(m.client, m.selfID, target.ID, target.DisplayName), true
		}
	default:
		var cmd tea.Cmd
		m.newDMQuery, cmd = m.newDMQuery.Update(msg)
		cmds = append(cmds, cmd)
		q := strings.TrimSpace(m.newDMQuery.Value())
		if len(q) >= 2 {
			cmds = append(cmds, searchUsersCmd(m.client, q))
		} else {
			m.newDMResults = nil
		}
		return m, tea.Batch(cmds...), true
	}
	return m, tea.Batch(cmds...), true
}

func (m Model) handlePresenceMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg.String() {
	case "esc", "q":
		m.showPresenceMenu = false
	case "up", "k":
		if m.presenceCursor > 0 {
			m.presenceCursor--
		}
	case "down", "j":
		if m.presenceCursor < len(m.presenceOptions)-1 {
			m.presenceCursor++
		}
	case "g", "home":
		m.presenceCursor = 0
	case "G", "end":
		if len(m.presenceOptions) > 0 {
			m.presenceCursor = len(m.presenceOptions) - 1
		}
	case "enter":
		m.showPresenceMenu = false
		avail := m.presenceOptions[m.presenceCursor]
		if m.selfID != "" {
			cmds = append(cmds, setPresenceCmd(m.client, m.selfID, avail))
			// Optimistic update: reflect the choice immediately instead of
			// waiting for the Graph GET round-trip. "Reset (Automatic)" is not
			// a real presence state, so let the poll confirm the cleared one.
			if avail == "Reset (Automatic)" {
				m.preferredPresence = ""
			} else {
				m.presence[m.selfID] = avail
				m.preferredPresence = avail
				m.preferredPresenceSetAt = time.Now()
			}
		} else {
			m.presenceError = "Cannot set presence: user ID not loaded yet"
		}
	}
	return m, tea.Batch(cmds...), true
}

func (m Model) handleCreateFolderPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.showCreateFolderPopup = false
		m.createFolderErr = ""
		return m, nil, true
	case "enter":
		name := strings.TrimSpace(m.createFolderInput.Value())
		if name == "" {
			m.createFolderErr = "Folder name cannot be empty."
			return m, nil, true
		}
		if len(m.folderStack) == 0 {
			return m, nil, true
		}
		parentID := m.folderStack[len(m.folderStack)-1].ID
		m.showCreateFolderPopup = false
		m.createFolderErr = ""
		return m, func() tea.Msg {
			item, err := m.client.CreateFolder(m.teams[m.selectedTeam].ID, parentID, name)
			if err != nil {
				return errMsg{err}
			}
			return createFolderDoneMsg{item: item}
		}, true
	default:
		var cmd tea.Cmd
		m.createFolderInput, cmd = m.createFolderInput.Update(msg)
		return m, cmd, true
	}
}

func (m Model) handleDeleteFilePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc", "n":
		m.showDeleteFilePopup = false
		return m, nil, true
	case "enter", "y":
		if m.selectedFile >= len(m.files) {
			m.showDeleteFilePopup = false
			return m, nil, true
		}
		target := m.files[m.selectedFile]
		driveID := m.currentFilesDriveID
		m.showDeleteFilePopup = false
		return m, func() tea.Msg {
			var err error
			if driveID != "" {
				err = m.client.DeleteRemoteItem(driveID, target.ID)
			} else {
				err = m.client.DeleteItem(m.teams[m.selectedTeam].ID, target.ID)
			}
			if err != nil {
				return errMsg{err}
			}
			return deleteFileDoneMsg{}
		}, true
	}
	return m, nil, true
}

func (m Model) handleConfirmingDownload(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg.String() {
	case "y", "enter":
		m.confirmingDownload = false
		m.downloading = true
		targets := m.downloadTargets
		driveID := m.currentFilesDriveID
		teamID := m.teams[m.selectedTeam].ID
		cmds = append(cmds, downloadFilesCmd(m.client, teamID, driveID, targets, m.prefs.DownloadDir))
		m.selectedFiles = make(map[int]bool)
		return m, tea.Batch(cmds...), true
	case "e":
		m.showDirPicker = true
		m.pickerPurpose = "download"
		m.dirPicker = directorypicker.New(directorypicker.Options{
			Title:       "Select download folder",
			InitialPath: m.prefs.DownloadDir,
			Mode:        "dir",
			Width:       m.width,
			Height:      m.height,
		})
		m.dirPicker, _ = m.dirPicker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		return m, nil, true
	case "n", "esc":
		m.confirmingDownload = false
		m.downloadTargets = nil
		return m, nil, true
	}
	return m, nil, true // intercept all keys while the popup is open
}
