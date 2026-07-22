package ui

import (
	"fmt"
	"sort"
	"strings"
	"teamsTUI/internal/teams"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleMessagesMsg(msg messagesMsg) (tea.Model, tea.Cmd) {
	m.messages = msg.messages
	m.messagesBackwardLink = msg.backwardLink
	m.loadingMore = false
	m.loading = false

	var cmds []tea.Cmd

	if m.viewMode == ModeChat {
		var content string
		msgsToRender := m.messages
		if m.searchQuery != "" {
			msgsToRender = m.filterMessages(m.messages, m.searchQuery)
		}

		if m.workspace == WorkspaceDMs {
			content = formatMessagesDM(msgsToRender, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
		} else {
			content = formatMessagesWithCursor(msgsToRender, m.viewport.Width, m.messageCursor, m.cursorMode)
		}
		atBottom := m.viewport.AtBottom()
		m.viewport.SetContent(content)
		if atBottom {
			m.viewport.GotoBottom()
		}

		if m.showThread {
			replies := repliesFor(m.messages, m.threadParentID)
			threadContent := formatThread(m.threadParentMsg, replies, m.threadViewport.Width, m.userName, m.threadCursor, true)
			m.threadViewport.SetContent(threadContent)
		}

		// Mark conversation as read (fire-and-forget)
		if len(m.messages) > 0 {
			lastMsg := m.messages[0] // messages are newest-first
			cmds = append(cmds, markAsReadCmd(m.client, m.loadedConvID, lastMsg))
		}
	} else if m.viewMode == ModeFiles {
		m.files = teams.AggregateChatAttachments(m.messages)
		m.selectedFile = 0
		m.viewport.SetContent(renderFilesContent(&m))
		m.viewport.GotoTop()
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleChatsMsg(msg chatsMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	m.chats = msg.chats
	// Sort: personal notes first, then oneOnOne, then group, then meeting
	selfChatID := ""
	if m.selfID != "" {
		selfChatID = fmt.Sprintf("19:%s_%s@unq.gbl.spaces", m.selfID, m.selfID)
	}
	sort.SliceStable(m.chats, func(i, j int) bool {
		a, b := m.chats[i], m.chats[j]
		return chatPriority(a, selfChatID) < chatPriority(b, selfChatID)
	})
	m.chatsLoaded = true
	m.loading = false
	m.selectedChat = 0

	// Trigger presence immediately when loading chats
	seen := make(map[string]struct{})
	var ids []string
	if m.selfID != "" {
		seen[m.selfID] = struct{}{}
		ids = append(ids, m.selfID)
	}
	for _, ch := range m.chats {
		for _, u := range ch.Members {
			if u.UserID != "" {
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

	// Fire unread checks for all chats
	for _, ch := range m.chats {
		cmds = append(cmds, checkUnreadCmd(m.client, ch))
	}

	// Chain async self-discovery, passing the cache if available
	if m.selfID != "" {
		cachedID := m.prefs.SelfChatIDs[m.selfID]
		cmds = append(cmds, discoverSelfChatCmd(m.client, m.selfID, cachedID))
		return m, tea.Batch(cmds...)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleTeamsMsg(msg teamsMsg) (tea.Model, tea.Cmd) {
	m.teams = msg.teams
	m.teamsLoaded = true
	m.loading = false
	if len(m.teams) > 0 {
		if m.selectedTeam >= len(m.teams) {
			m.selectedTeam = len(m.teams) - 1
		}
		m.loading = true
		m.teamMembers = nil
		return m, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID)
	} else {
		m.selectedTeam = 0
		m.channels = nil
	}
	return m, nil
}

func (m Model) handleChannelsMsg(msg channelsMsg) (tea.Model, tea.Cmd) {
	if len(m.teams) == 0 || m.selectedTeam >= len(m.teams) || msg.teamID != m.teams[m.selectedTeam].ID {
		return m, nil // stale response, discard
	}
	m.channels = msg.channels
	m.selectedChan = 0
	// Skip to first visible channel if first is hidden
	teamID := m.teams[m.selectedTeam].ID
	hidden := m.prefs.HiddenChannels[teamID]
	for i, c := range m.channels {
		if !contains(hidden, c.ID) {
			m.selectedChan = i
			break
		}
	}
	m.messages = nil
	m.channelErr = nil
	m.loading = false
	// Populate the channelID → teamID map and find General threadId
	for _, ch := range msg.channels {
		m.channelToTeam[ch.ID] = msg.teamID
		if strings.EqualFold(ch.DisplayName, "General") {
			m.teamThreadID = ch.ID
		}
	}
	return m, nil
}

func (m Model) handleFilesMsg(msg filesMsg) (tea.Model, tea.Cmd) {
	// Discard stale responses from a previous folder level
	if msg.folderID != "" {
		expectedKey := ""
		rootKey := ""
		if len(m.folderStack) > 0 {
			expectedKey = m.folderStack[len(m.folderStack)-1].ID
		}
		if m.selectedChan < len(m.channels) {
			rootKey = "root:" + m.channels[m.selectedChan].ID
		}
		if msg.folderID != expectedKey && msg.folderID != rootKey {
			return m, nil
		}
		m.folderCache[msg.folderID] = msg.files
	}

	m.files = msg.files
	m.loading = false
	m.selectedFile = 0
	m.selectedFiles = make(map[int]bool)

	m.viewport.SetContent(renderFilesContent(&m))
	m.viewport.GotoTop()
	return m, nil
}

func (m Model) handleTickMsg(msg tickMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	// Poll: refresh chats if we're in DMs and the loaded conversation is still open
	cmds = append(cmds, pollChatsCmd(m.client))
	// Refresh messages if a conversation is open and the user isn't typing
	if m.loadedConvID != "" && m.viewMode == ModeChat && !m.isTyping && !m.focusLeft {
		m.messagesBackwardLink = ""
		m.loadingMore = false
		cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
	}
	// Re-schedule the next tick
	cmds = append(cmds, refreshTickCmd())
	return m, tea.Batch(cmds...)
}

func (m Model) handlePresenceMsg(msg presenceTickMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	// Presence poll: always include selfID, others only in DMs
	seen := make(map[string]struct{})
	var ids []string
	if m.selfID != "" {
		seen[m.selfID] = struct{}{}
		ids = append(ids, m.selfID)
	}
	if m.workspace == WorkspaceDMs && len(m.chats) > 0 {
		for _, ch := range m.chats {
			for _, u := range ch.Members {
				if u.UserID != "" {
					if _, ok := seen[u.UserID]; !ok {
						seen[u.UserID] = struct{}{}
						ids = append(ids, u.UserID)
					}
				}
			}
		}
	}
	if len(ids) > 0 {
		cmds = append(cmds, pollPresenceCmd(m.client, ids))
	}
	cmds = append(cmds, refreshPresenceTickCmd())
	return m, tea.Batch(cmds...)
}

func (m Model) handlePresenceResultMsg(msg presenceTickResultMsg) (tea.Model, tea.Cmd) {
	for k, v := range msg.presences {
		m.presence[k] = v
	}
	return m, nil
}

func (m Model) handleNotificationsMsg(msg notificationsMsg) (tea.Model, tea.Cmd) {
	m.notifications = msg.items
	m.notifLoaded = true
	m.notifErr = nil
	return m, nil
}

func (m Model) handlePollChatsMsg(msg pollChatsMsg) (tea.Model, tea.Cmd) {
	// Update the chat list with fresh data
	// (no badge: lastModifiedDateTime is not available on this tenant)
	return m, nil
}
