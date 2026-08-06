package ui

import (
	"fmt"
	"lazyteams/internal/graph"
	"lazyteams/internal/teams"
	"strings"

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
			content = formatMessagesDM(msgsToRender, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		} else {
			content = formatMessagesWithCursor(msgsToRender, m.viewport.Width, m.messageCursor, m.cursorMode, m.activeSearchCursor())
		}
		atBottom := m.viewport.AtBottom()
		m.viewport.SetContent(content)
		if atBottom || m.forceScrollBottom {
			m.viewport.GotoBottom()
			m.forceScrollBottom = false
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
		atBottom := m.viewport.AtBottom()
		curID := ""
		if m.selectedFile >= 0 && m.selectedFile < len(m.files) {
			curID = m.files[m.selectedFile].ID
		}
		m.files = teams.AggregateChatAttachments(m.messages)
		m.selectedFile = 0
		if curID != "" {
			for i, f := range m.files {
				if f.ID == curID {
					m.selectedFile = i
					break
				}
			}
		}
		m.viewport.SetContent(renderFilesContent(&m))
		if atBottom {
			m.viewport.GotoBottom()
		}
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
	sortChats(m.chats, m.chatUnread, selfChatID)
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
	// Keep the current selection when this is a background auto-refresh
	var prevID, prevRemoteID string
	preserving := msg.preserve && m.selectedFile >= 0 && m.selectedFile < len(m.files)
	if preserving {
		prevID = m.files[m.selectedFile].ID
		if m.files[m.selectedFile].RemoteItem != nil {
			prevRemoteID = m.files[m.selectedFile].RemoteItem.ID
		}
	}

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
	m.filesRefreshing = false
	m.selectedFile = 0
	if preserving && prevID != "" {
		for i, f := range msg.files {
			match := f.ID == prevID
			if prevRemoteID != "" && f.RemoteItem != nil && f.RemoteItem.ID == prevRemoteID {
				match = true
			}
			if match {
				m.selectedFile = i
				break
			}
		}
	}
	m.selectedFiles = make(map[int]bool)

	m.viewport.SetContent(renderFilesContent(&m))
	m.viewport.GotoTop()
	return m, nil
}

func (m Model) handleTickMsg(msg tickMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	// Poll: refresh chats if we're in DMs and the loaded conversation is still open
	cmds = append(cmds, pollChatsCmd(m.client))
	if !m.notificationsRefreshing {
		m.notificationsRefreshing = true
		cmds = append(cmds, pollNotificationsCmd(m.client))
	}
	// Refresh messages if a conversation is open and the user isn't typing.
	// DM files are aggregated from messages, so they refresh together.
	if m.loadedConvID != "" && !m.isTyping && !m.focusLeft &&
		(m.viewMode == ModeChat || (m.viewMode == ModeFiles && m.workspace == WorkspaceDMs)) {
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
	updateNotifications(&m, msg.items)
	m.notificationsRefreshing = false
	return m, nil
}

func (m Model) handlePollNotificationsMsg(msg pollNotificationsMsg) (tea.Model, tea.Cmd) {
	m.notificationsRefreshing = false
	if msg.err != nil {
		if is401(msg.err) {
			return m, queueTokenRenewal(&m, "notif")
		}
		return m, nil
	}
	updateNotifications(&m, msg.items)
	return m, nil
}

func updateNotifications(m *Model, items []graph.NotificationItem) {
	selectedID := ""
	if m.selectedNotif >= 0 && m.selectedNotif < len(m.notifications) {
		selectedID = m.notifications[m.selectedNotif].ID
	}

	m.notifications = items
	m.notifLoaded = true
	m.notifErr = nil
	m.notificationsRefreshing = false
	if selectedID != "" {
		for i, item := range items {
			if item.ID == selectedID {
				m.selectedNotif = i
				return
			}
		}
	}
	if m.selectedNotif >= len(items) {
		m.selectedNotif = len(items) - 1
	}
	if m.selectedNotif < 0 {
		m.selectedNotif = 0
	}
}

func (m Model) handlePollChatsMsg(msg pollChatsMsg) (tea.Model, tea.Cmd) {
	if len(msg.chats) == 0 {
		return m, nil
	}

	// Preserve the currently selected chat across the refresh
	var currID string
	if m.selectedChat >= 0 && m.selectedChat < len(m.chats) {
		currID = m.chats[m.selectedChat].ID
	}

	// Detect chats that did not exist in the previous poll (new DMs)
	oldChats := m.chats
	existing := make(map[string]struct{}, len(oldChats))
	for _, c := range oldChats {
		existing[c.ID] = struct{}{}
	}
	var newChats []graph.Chat
	for _, c := range msg.chats {
		if _, ok := existing[c.ID]; !ok {
			newChats = append(newChats, c)
		}
	}

	m.chats = msg.chats
	selfChatID := ""
	if m.selfID != "" {
		selfChatID = fmt.Sprintf("19:%s_%s@unq.gbl.spaces", m.selfID, m.selfID)

		// Re-apply the discovered "Personal notes" chat ID: GetChats returns the
		// placeholder id, and the hot-update from selfChatDiscoveredMsg would
		// otherwise be lost on every poll.
		if cachedID := m.prefs.SelfChatIDs[m.selfID]; cachedID != "" {
			for i := range m.chats {
				if m.chats[i].Topic == "Personal notes (You)" && m.chats[i].ID != cachedID {
					m.chats[i].ID = cachedID
				}
			}
		}
	}

	// Merge back chats that exist locally but not yet in the Graph response
	// (e.g. a DM just created by the app) so they don't blink out of the list.
	inFresh := make(map[string]struct{}, len(m.chats))
	for _, c := range m.chats {
		inFresh[c.ID] = struct{}{}
	}
	for _, c := range oldChats {
		if _, ok := inFresh[c.ID]; !ok {
			m.chats = append(m.chats, c)
			inFresh[c.ID] = struct{}{}
		}
	}

	sortChats(m.chats, m.chatUnread, selfChatID)
	if currID != "" {
		for i, c := range m.chats {
			if c.ID == currID {
				m.selectedChat = i
				break
			}
		}
	}

	var cmds []tea.Cmd
	if m.selfID != "" {
		for _, ch := range newChats {
			cmds = append(cmds, checkUnreadCmd(m.client, ch))
		}
	}
	return m, tea.Batch(cmds...)
}
