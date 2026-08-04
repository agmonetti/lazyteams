package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"teamsTUI/internal/graph"
	"teamsTUI/internal/teams"
	"teamsTUI/internal/ui/components/directorypicker"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	// Insert mode — intercepts first
	if m.isTyping {
		newModel, keyCmd, consumed := m.handleInsertMode(msg)
		if consumed {
			modelWithHeight := newModel.(Model)
			modelWithHeight.recalculateViewportHeight()
			return modelWithHeight, keyCmd
		}
		m = newModel.(Model)
		cmds = append(cmds, keyCmd)
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// After setting m.width and m.height:
		if m.width < 120 {
			m.mobileMode = true
		} else {
			m.mobileMode = false
		}
		// Width: real overhead = 2 per panel (border) × 2 panels = 4
		// + 1 col margin for Kitty = 5 total
		available := m.width - 5
		leftOuterWidth := available / 3
		rightOuterWidth := available - leftOuterWidth
		leftInnerWidth := leftOuterWidth - 2
		rightInnerWidth := rightOuterWidth - 2
		panelOuterHeight := m.height - 6
		if m.mobileMode {
			panelOuterHeight = m.height - 2
		}
		if panelOuterHeight < 2 {
			panelOuterHeight = 2
		}
		leftInnerHeight := panelOuterHeight - 2
		rightInnerHeight := panelOuterHeight - 2

		var mobileInnerWidth int
		if m.mobileMode {
			mobileWidth := m.width - 2
			if mobileWidth < 20 {
				mobileWidth = 20
			}
			mobileInnerWidth = mobileWidth - 2
			m.input.SetWidth(mobileInnerWidth - 2)
		} else {
			m.input.SetWidth(rightInnerWidth - 2)
		}

		// Max height for textarea to prevent it from covering the whole screen
		m.input.MaxHeight = rightInnerHeight / 3

		if !m.ready {
			if m.mobileMode {
				m.viewport = viewport.New(mobileInnerWidth, 5)
				m.leftVp = viewport.New(mobileInnerWidth, leftInnerHeight)
				m.threadViewport = viewport.New(mobileInnerWidth, 5)
			} else {
				m.viewport = viewport.New(rightInnerWidth, 5)
				m.leftVp = viewport.New(leftInnerWidth, leftInnerHeight)
				m.threadViewport = viewport.New(rightInnerWidth, 5)
			}
			m.ready = true
		} else {
			if m.mobileMode {
				m.viewport.Width = mobileInnerWidth
				m.threadViewport.Width = mobileInnerWidth
				m.leftVp.Width = mobileInnerWidth
			} else {
				m.viewport.Width = rightInnerWidth
				m.threadViewport.Width = rightInnerWidth
				m.leftVp.Width = leftInnerWidth
			}
			m.leftVp.Height = leftInnerHeight
		}

		m.recalculateViewportHeight()

		// Re-wrap existing content with the new width
		if m.ready && len(m.messages) > 0 {
			if m.viewMode == ModeChat {
				var content string
				msgsToRender := m.messages
				if m.searchQuery != "" {
					msgsToRender = m.filterMessages(m.messages, m.searchQuery)
				}
				if m.workspace == WorkspaceDMs {
					content = formatMessagesDM(msgsToRender, rightInnerWidth, m.userName, m.selfID, m.messageCursor, m.cursorMode)
				} else {
					content = formatMessagesWithCursor(msgsToRender, rightInnerWidth, m.messageCursor, m.cursorMode)
				}
				m.viewport.SetContent(content)
			} else if m.viewMode == ModeFiles {
				m.viewport.SetContent(renderFilesContent(&m))
			}
		}

	case previewResultMsg:
		m.loading = false
		if msg.status != "" {
			m.downloadStatus = msg.status
			m.downloadStatusID++
			if m.viewMode == ModeFiles {
				m.viewport.SetContent(renderFilesContent(&m))
			}
		}
		if msg.openBrowser {
			return m, clearStatusAfter(m.downloadStatusID)
		}
		if msg.err != nil {
			m.previewContent = fmt.Sprintf("Error loading preview: %v", msg.err)
		} else {
			// Add line numbers
			lines := strings.Split(msg.content, "\n")
			var numbered strings.Builder
			width := len(fmt.Sprintf("%d", len(lines)))
			for i, line := range lines {
				numbered.WriteString(fmt.Sprintf("%*d │ %s\n", width, i+1, line))
			}
			m.previewContent = numbered.String()
		}
		m.previewFileName = msg.fileName
		m.previewing = true
		m.viewport.SetContent(m.previewContent)
		m.viewport.GotoTop()
		return m, nil

	case errMsg:
		if is401(msg.err) {
			which := detectExpiredToken(msg.err)
			if !m.tokenRenewing {
				m.tokenRenewing = true
				m.tokenRenewingType = which
				return m, authHelperRenewal(&m, which)
			}
			return m, nil
		}
		m.err = msg.err
		m.loading = false
		return m, nil

	case tokenCheckDoneMsg:
		for _, tokenType := range msg.expired {
			if !m.tokenRenewing {
				m.tokenRenewing = true
				m.tokenRenewingType = tokenType
				return m, authHelperRenewal(&m, tokenType)
			}
		}
		return m, nil

	case tokenRenewingMsg:
		m.tokenRenewing = true
		m.tokenRenewingType = "web"
		return m, authHelperRenewal(&m, "web")

	case tokenRenewedMsg:
		m.renewalProc = nil
		if msg.err != nil {
			m.tokenRenewErr = msg.err.Error()
			m.tokenRenewing = false
			m.tokenRenewingType = ""
		} else {
			m.tokenRenewErr = ""
			return m, reloadTokensCmd(m.client, msg.tokenType)
		}
		return m, nil

	case tokensReloadedMsg:
		m.tokenRenewing = false
		m.tokenRenewingType = ""
		if msg.err != nil {
			m.tokenRenewErr = msg.err.Error()
		} else {
			m.tokenRenewErr = ""
			if msg.tokenType == "edu" {
				return m, loadAssignmentsCmd(m.client)
			}
		}
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
		var chatsvcErr *graph.ChatSvcError
		if m.selfID != "" && msg.conversationID == m.prefs.SelfChatIDs[m.selfID] && errors.As(msg.err, &chatsvcErr) && chatsvcErr.StatusCode == 404 {
			// Direct access to personal notes failed with 404.
			// The MRI probably changed or the cache is stale.
			// 1. Clear the cache
			delete(m.prefs.SelfChatIDs, m.selfID)
			savePrefs(m.prefs)

			// 2. Show a message in the UI
			m.viewport.SetContent("Chat identifier expired. Auto-repairing access...")

			// 3. Re-trigger discovery forcing network (passing empty string)
			return m, discoverSelfChatCmd(m.client, m.selfID, "")
		}

		// Partial load: if there are messages before the failure, show them with a warning
		if len(msg.partialMsgs) > 0 {
			m.messages = msg.partialMsgs
			m.loading = false
			if m.viewMode == ModeFiles {
				m.files = teams.AggregateChatAttachments(m.messages)
				m.selectedFile = 0
				m.viewport.SetContent(renderFilesContent(&m) + "\n\n(partial load due to network error)")
			} else {
				var partial string
				msgsToRender := m.messages
				if m.searchQuery != "" {
					msgsToRender = m.filterMessages(m.messages, m.searchQuery)
				}
				if m.workspace == WorkspaceDMs {
					partial = formatMessagesDM(msgsToRender, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
				} else {
					partial = formatMessagesWithCursor(msgsToRender, m.viewport.Width, m.messageCursor, m.cursorMode)
				}
				m.viewport.SetContent(partial + "\n\n(partial load due to network error)")
			}
			return m, nil
		}

		if is401(msg.err) {
			which := detectExpiredToken(msg.err)
			if !m.tokenRenewing {
				m.tokenRenewing = true
				m.tokenRenewingType = which
				return m, authHelperRenewal(&m, which)
			}
		} else {
			m.viewport.SetContent(fmt.Sprintf("Error loading messages: %v", msg.err))
		}
		m.loading = false
		return m, nil

	case messageSentMsg:
		// Message sent successfully. Reload messages for the channel/chat
		m.messagesBackwardLink = ""
		m.loadingMore = false
		m.forceScrollBottom = true
		return m, loadMessagesCmd(m.client, "", m.activeConversationID(), 200)

	case messageSendErrMsg:
		m.viewport.SetContent(fmt.Sprintf("Error sending message: %v", msg.err))
		m.loading = false
		return m, nil

	case threadReplySentMsg:
		m.isReplyTyping = false
		m.input.Reset()
		// Reload messages to get the new reply
		m.messagesBackwardLink = ""
		m.loadingMore = false
		m.forceScrollBottom = true
		return m, loadMessagesCmd(m.client, "", m.activeConversationID(), 200)

	case threadReplySendErrMsg:
		m.isReplyTyping = false
		m.threadViewport.SetContent(fmt.Sprintf("Error sending reply: %v", msg.err))
		return m, nil

	case editMessageMsg:
		m.showEditPopup = false
		m.editInput.Reset()
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}
		m.messagesBackwardLink = ""
		m.loadingMore = false
		return m, loadMessagesCmd(m.client, "", m.activeConversationID(), 200)

	case deleteMessageMsg:
		m.showDeleteMsgPopup = false
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}
		m.messagesBackwardLink = ""
		m.loadingMore = false
		return m, loadMessagesCmd(m.client, "", m.activeConversationID(), 200)

	case addReactionMsg:
		m.reactionPending = false
		teamID := ""
		if m.workspace == WorkspaceTeams && len(m.teams) > 0 {
			teamID = m.teams[m.selectedTeam].ID
		}
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, tea.Batch(
				loadMessagesCmd(m.client, teamID, m.activeConversationID(), 200),
				clearStatusAfter(m.downloadStatusID),
			)
		}
		return m, loadMessagesCmd(m.client, teamID, m.activeConversationID(), 200)

	case removeReactionMsg:
		m.reactionPending = false
		teamID := ""
		if m.workspace == WorkspaceTeams && len(m.teams) > 0 {
			teamID = m.teams[m.selectedTeam].ID
		}
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, tea.Batch(
				loadMessagesCmd(m.client, teamID, m.activeConversationID(), 200),
				clearStatusAfter(m.downloadStatusID),
			)
		}
		return m, loadMessagesCmd(m.client, teamID, m.activeConversationID(), 200)

	case reactionsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}
		for i, message := range m.messages {
			if message.ID == msg.messageID {
				// Reset all first, then apply fresh state
				for j := range m.messages[i].Reactions {
					m.messages[i].Reactions[j].UserReacted = false
				}
				for j, r := range m.messages[i].Reactions {
					if msg.reactions[r.Key] {
						m.messages[i].Reactions[j].UserReacted = true
					}
				}
				break
			}
		}
		m.showReactionPicker = true
		m.reactionCursor = 0
		return m, nil

	case loadMoreMessagesMsg:
		m.loadingMore = false
		if msg.err != nil {
			return m, nil
		}
		// Prepend older messages (append them because index 0 is newest)
		m.messages = append(m.messages, msg.messages...)
		m.messagesBackwardLink = msg.backwardLink

		// Re-render preserving scroll position
		yBefore := m.viewport.YOffset
		var content string
		if m.workspace == WorkspaceDMs {
			content = formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
		} else {
			content = formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode)
		}
		m.viewport.SetContent(content)
		m.viewport.SetYOffset(yBefore)
		return m, nil

	case filesErrMsg:
		m.viewport.SetContent(fmt.Sprintf("Error loading files: %v", msg.err))
		m.loading = false
		return m, nil

	case channelRootMsg:
		if msg.err != nil {
			m.err = msg.err
			m.loading = false
			return m, nil
		}
		m.folderStack = append(m.folderStack, msg.node)
		m.currentFilesDriveID = msg.node.DriveID
		// Check cache for root
		cacheKey := "root:" + m.channels[m.selectedChan].ID
		if cached, ok := m.folderCache[cacheKey]; ok {
			m.files = cached
			m.selectedFile = 0
			m.selectedFiles = make(map[int]bool)
			m.loading = false
			m.viewport.SetContent(renderFilesContent(&m))
			m.viewport.GotoTop()
		} else {
			cmds = append(cmds, loadFilesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].DisplayName, m.channels[m.selectedChan].ID))
		}
		return m, tea.Batch(cmds...)

	case filesMsg:
		return m.handleFilesMsg(msg)

	case teamsMsg:
		return m.handleTeamsMsg(msg)

	case channelsMsg:
		return m.handleChannelsMsg(msg)

	case meMsg:
		m.selfID = msg.id
		m.client.SelfID = msg.id
		var cmds []tea.Cmd
		for _, ch := range m.chats {
			cmds = append(cmds, checkUnreadCmd(m.client, ch))
		}
		return m, tea.Batch(cmds...)

	case meErrMsg:
		// Don't block the app: without selfID, 1:1 chats will simply
		// show all participants instead of excluding me.
		return m, nil

	case chatsErrMsg:
		if is401(msg.err) {
			if !m.tokenRenewing {
				m.tokenRenewing = true
				m.tokenRenewingType = detectExpiredToken(msg.err)
				return m, authHelperRenewal(&m, detectExpiredToken(msg.err))
			}
		} else {
			m.err = msg.err
		}
		m.loading = false
		return m, nil

	case chatsMsg:
		return m.handleChatsMsg(msg)

	case selfChatDiscoveredMsg:
		// If it was just discovered via brute-force, save it to disk
		if msg.newlyDiscovered {
			m.prefs.SelfChatIDs[m.selfID] = msg.id
			savePrefs(m.prefs)
		}

		// Check if the synthetic chat already exists in the list
		found := false
		for i, ch := range m.chats {
			if ch.Topic == "Personal notes (You)" {
				oldID := ch.ID
				m.chats[i].ID = msg.id // Hot update
				found = true

				// If the user was trying to read this chat and it failed, auto-recover
				if m.loadedConvID == oldID {
					m.loadedConvID = msg.id
					m.loading = true
					m.messagesBackwardLink = ""
					m.loadingMore = false
					m.forceScrollBottom = true
					return m, loadMessagesCmd(m.client, "", msg.id, 200)
				}
				break
			}
		}

		// If it didn't exist (first load), insert it at the beginning
		if !found {
			selfChat := graph.Chat{
				ID:       msg.id,
				Topic:    "Personal notes (You)",
				ChatType: "oneOnOne",
			}
			m.chats = append([]graph.Chat{selfChat}, m.chats...)
			if m.selectedChat > 0 {
				m.selectedChat++ // Keep the visual selection where it was
			}
		}
		return m, nil

	case messagesMsg:
		return m.handleMessagesMsg(msg)

	case tickMsg:
		return m.handleTickMsg(msg)

	case presenceTickMsg:
		return m.handlePresenceMsg(msg)

	case presenceTickResultMsg:
		return m.handlePresenceResultMsg(msg)

	case notificationsMsg:
		return m.handleNotificationsMsg(msg)

	case notificationsErrMsg:
		if is401(msg.err) {
			if !m.tokenRenewing {
				m.tokenRenewing = true
				m.tokenRenewingType = "notif"
				return m, authHelperRenewal(&m, "notif")
			}
		} else {
			m.notifErr = msg.err
		}
		m.notifLoaded = true
		return m, nil

	case assignmentsMsg:
		m.assignments = msg.items
		m.assignLoaded = true
		m.assignErr = nil
		return m, nil

	case assignmentsErrMsg:
		if is401(msg.err) {
			if !m.tokenRenewing {
				m.tokenRenewing = true
				m.tokenRenewingType = "edu"
				return m, authHelperRenewal(&m, "edu")
			}
		}
		m.assignErr = msg.err
		m.assignLoaded = true
		return m, nil

	case assignmentDetailMsg:
		if msg.err == nil {
			for i, a := range m.assignments {
				if a.ID == msg.assignmentID {
					if msg.refFiles != nil {
						m.assignments[i].RefFiles = msg.refFiles
					}
					if msg.myFiles != nil {
						m.assignments[i].MyFiles = msg.myFiles
					}
					m.assignments[i].ResourcesFolderUrl = msg.resourcesFolderUrl
					break
				}
			}
		}
		return m, nil

	case navigateToThreadMsg:
		for _, ch := range msg.channels {
			m.channelToTeam[ch.ID] = msg.teamID
		}
		for i, t := range m.teams {
			if t.ID == msg.teamID {
				m.selectedTeam = i
				break
			}
		}
		m.channels = msg.channels
		m.workspace = WorkspaceTeams
		m.focusLeft = false
		m.viewMode = ModeChat
		m.loadedConvID = msg.threadID
		m.loading = true
		for i, ch := range msg.channels {
			if ch.ID == msg.threadID {
				m.selectedChan = i
				break
			}
		}
		m.messagesBackwardLink = ""
		m.loadingMore = false
		m.forceScrollBottom = true
		return m, loadMessagesCmd(m.client, msg.teamID, msg.threadID, 200)

	case setPresenceMsg:
		if msg.err != nil {
			m.presenceError = msg.err.Error()
		} else {
			m.presenceError = ""
		}
		// Refresh own presence immediately
		if m.selfID != "" {
			cmds = append(cmds, pollPresenceCmd(m.client, []string{m.selfID}))
		}
		return m, tea.Batch(cmds...)

	case imagePastedMsg:
		m.downloading = false
		if msg.err != nil {
			m.downloadStatus = "✗ " + msg.err.Error()
		} else {
			m.downloadStatus = "✓ Image pasted successfully"
			m.input.Reset()
			// Reload messages to show the new one
			cmds = append(cmds, loadMessagesCmd(m.client, "", m.activeConversationID(), 200))
		}
		m.downloadStatusID++
		cmds = append(cmds, clearStatusAfter(m.downloadStatusID))
		return m, tea.Batch(cmds...)

	case downloadDoneMsg:
		m.downloading = false
		m.downloadStatus = strings.Join(msg.results, " | ")
		m.downloadStatusID++
		if m.viewMode == ModeFiles {
			m.viewport.SetContent(renderFilesContent(&m))
			m.viewport.GotoBottom()
		}
		return m, clearStatusAfter(m.downloadStatusID)

	case statusMsg:
		m.downloadStatus = msg.text
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case clearDownloadStatusMsg:
		if msg.id == m.downloadStatusID {
			m.downloadStatus = ""
			if m.viewMode == ModeFiles {
				m.viewport.SetContent(renderFilesContent(&m))
			}
		}
		return m, nil

	case pollChatsMsg:
		return m.handlePollChatsMsg(msg)

	case markNotifReadMsg:
		// silent — local state already updated optimistically
		return m, nil

	case searchUsersMsg:
		m.newDMResults = msg.results
		m.newDMCursor = 0
		m.newDMErr = ""
		return m, nil

	case searchUsersErrMsg:
		if is401(msg.err) {
			if !m.tokenRenewing {
				m.tokenRenewing = true
				m.tokenRenewingType = detectExpiredToken(msg.err)
				return m, authHelperRenewal(&m, detectExpiredToken(msg.err))
			}
		} else {
			m.newDMErr = msg.err.Error()
		}
		return m, nil

	case createDMMsg:
		for i, ch := range m.chats {
			if ch.ID == msg.chat.ID {
				m.selectedChat = i
				m.loadedConvID = ch.ID
				m.focusLeft = false
				m.recalculateViewportHeight()
				m.viewMode = ModeChat
				m.loading = true
				m.messagesBackwardLink = ""
				m.loadingMore = false
				m.forceScrollBottom = true
				return m, loadMessagesCmd(m.client, "", ch.ID, 200)
			}
		}
		m.chats = append([]graph.Chat{msg.chat}, m.chats...)
		m.selectedChat = 0
		m.loadedConvID = msg.chat.ID
		m.focusLeft = false
		m.recalculateViewportHeight()
		m.viewMode = ModeChat
		m.loading = true
		m.messagesBackwardLink = ""
		m.loadingMore = false
		m.forceScrollBottom = true
		return m, loadMessagesCmd(m.client, "", msg.chat.ID, 200)

	case createDMErrMsg:
		m.newDMErr = msg.err.Error()
		m.showNewDMPopup = true
		return m, nil

	case assignmentUploadDoneMsg:
		if msg.err != nil {
			m.downloadStatus = "✗ Upload failed: " + msg.err.Error()
		} else {
			m.downloadStatus = "✓ " + msg.fileName + " uploaded"
			return m, loadAssignmentDetailCmd(m.client,
				m.assignments[m.selectedAssign].ClassID,
				m.assignments[m.selectedAssign].ID)
		}
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case assignmentSubmitDoneMsg:
		if msg.err != nil {
			m.downloadStatus = "✗ Submit failed: " + msg.err.Error()
		} else {
			m.downloadStatus = "✓ Assignment submitted"
			for i, a := range m.assignments {
				if a.ID == msg.assignmentID {
					m.assignments[i].SubmissionStatus = "submitted"
					m.assignments[i].IsCompleted = true
					break
				}
			}
			m.focusLeft = true // Go back to list to avoid showing the wrong assignment
		}
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case assignmentUndoSubmitDoneMsg:
		if msg.err != nil {
			m.downloadStatus = "✗ Undo turn in failed: " + msg.err.Error()
		} else {
			m.downloadStatus = "✓ Submission reverted"
			for i, a := range m.assignments {
				if a.ID == msg.assignmentID {
					m.assignments[i].SubmissionStatus = "working"
					m.assignments[i].IsCompleted = false
					break
				}
			}
			m.focusLeft = true // Go back to list
		}
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case assignmentRemoveResourceDoneMsg:
		if msg.err != nil {
			m.downloadStatus = "✗ Remove failed: " + msg.err.Error()
		} else {
			m.downloadStatus = "✓ " + msg.fileName + " removed"

			return m, loadAssignmentDetailCmd(m.client,
				m.assignments[m.selectedAssign].ClassID,
				m.assignments[m.selectedAssign].ID)
		}
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case uploadDoneMsg:
		m.uploading = false
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ Upload failed: %v", msg.err)
		} else {
			m.downloadStatus = fmt.Sprintf("✓ Uploaded: %s", msg.item.Name)
			if m.workspace == WorkspaceDMs && msg.item.WebUrl != "" {
				m.downloadStatusID++
				return m, tea.Batch(
					sendMessageCmd(m.client, m.activeConversationID(), fmt.Sprintf("📎 [%s](%s)", msg.item.Name, msg.item.WebUrl), nil, nil),
					clearStatusAfter(m.downloadStatusID),
				)
			}
			if m.workspace == WorkspaceTeams && m.viewMode == ModeFiles {
				m.downloadStatusID++
				delete(m.folderCache, "root:"+m.channels[m.selectedChan].ID)
				if len(m.folderStack) == 0 {
					return m, nil
				}
				return m, tea.Batch(
					reloadCurrentFolderCmd(m.client, m.teams[m.selectedTeam].ID, m.folderStack[len(m.folderStack)-1]),
					clearStatusAfter(m.downloadStatusID),
				)
			}
		}
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case createFolderDoneMsg:
		m.files = append([]graph.DriveItem{msg.item}, m.files...)
		m.downloadStatus = fmt.Sprintf("✓ Folder \"%s\" created.", msg.item.Name)
		m.downloadStatusID++
		m.viewport.SetContent(renderFilesContent(&m))
		return m, clearStatusAfter(m.downloadStatusID)

	case deleteFileDoneMsg:
		name := ""
		if m.selectedFile < len(m.files) {
			name = m.files[m.selectedFile].Name
			m.files = append(m.files[:m.selectedFile], m.files[m.selectedFile+1:]...)
		}
		if m.selectedFile >= len(m.files) && m.selectedFile > 0 {
			m.selectedFile--
		}
		m.downloadStatus = fmt.Sprintf("✓ \"%s\" deleted.", name)
		m.downloadStatusID++
		// Invalidate root cache and reload from server to confirm
		delete(m.folderCache, "root:"+m.channels[m.selectedChan].ID)
		if len(m.folderStack) == 0 {
			return m, nil
		}
		return m, tea.Batch(
			reloadCurrentFolderCmd(m.client, m.teams[m.selectedTeam].ID, m.folderStack[len(m.folderStack)-1]),
			clearStatusAfter(m.downloadStatusID),
		)

	case dirPickerResultMsg:
		m.showDirPicker = false
		if msg.path != "" {
			m.prefs.DownloadDir = msg.path
			savePrefs(m.prefs)
		}
		return m, nil

	case createTeamMsg:
		if msg.err != nil {
			m.teamCreating = false
			m.createTeamErr = msg.err.Error()
		}
		return m, reloadTeamsAfterDelayCmd()

	case reloadTeamsAfterCreateMsg:
		m.teamCreating = false
		return m, loadTeamsCmd(m.client)

	case createChannelMsg:
		if msg.err != nil {
			m.createChannelErr = msg.err.Error()
			m.showCreateChannelPopup = false
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			m.viewport.SetContent(fmt.Sprintf("Error creating channel: %v", msg.err))
			return m, clearStatusAfter(m.downloadStatusID)
		}
		return m, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID)

	case deleteChannelMsg:
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}
		m.downloadStatus = "✓ Channel deleted"
		m.downloadStatusID++
		return m, tea.Batch(
			reloadChannelsAfterDelayCmd(),
			clearStatusAfter(m.downloadStatusID),
		)

	case deleteTeamMsg:
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}
		m.downloadStatus = "✓ Team deleted"
		m.downloadStatusID++
		return m, tea.Batch(
			reloadTeamsAfterShortDelayCmd(),
			clearStatusAfter(m.downloadStatusID),
		)

	case teamInfoMsg:
		m.teamInfo = msg.team
		m.showTeamInfo = true
		return m, nil

	case teamInfoErrMsg:
		m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case channelInfoMsg:
		m.channelInfo = msg.channel
		if m.viewMode == ModeInfo {
			m.viewport.SetContent(renderInfoContent(&m))
			m.viewport.GotoTop()
		}
		return m, nil

	case channelInfoErrMsg:
		m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case channelMembersMsg:
		m.channelMembers = msg.members
		if m.viewMode == ModeInfo {
			m.viewport.SetContent(renderInfoContent(&m))
			m.viewport.GotoTop()
		}
		return m, nil

	case channelMembersErrMsg:
		// silently ignore
		return m, nil

	case teamMembersMsg:
		m.teamMembers = msg.members
		m.membersLoading = false
		if m.showAddChannelMemberPopup {
			excluded := make(map[string]bool)
			for _, cm := range m.channelMembers {
				excluded[cm.ID] = true
			}
			var available []graph.TeamMember
			for _, tm := range msg.members {
				if !excluded[tm.ID] {
					available = append(available, tm)
				}
			}
			m.addChannelMemberResults = available
			return m, nil
		}
		if m.membersLoadSilent {
			m.membersLoadSilent = false
			return m, nil
		}
		m.showMembersPopup = true
		return m, nil

	case teamMembersErrMsg:
		m.membersLoading = false
		m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case addMemberMsg:
		if msg.err != nil {
			m.addMemberErr = msg.err.Error()
		} else {
			m.showAddMemberPopup = false
			m.addMemberInput.Reset()
			m.newDMResults = nil
			m.addMemberErr = ""
			m.downloadStatus = "✓ Member added"
			m.downloadStatusID++
			m.membersLoading = true
			return m, tea.Batch(
				loadTeamMembersCmd(m.client, m.teams[m.selectedTeam].ID),
				clearStatusAfter(m.downloadStatusID),
			)
		}
		return m, nil

	case removeMemberMsg:
		m.showRemoveMemberPopup = false
		if msg.err != nil {
			if is401(msg.err) {
				return m, func() tea.Msg { return errMsg{err: msg.err} }
			}
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
		} else {
			m.downloadStatus = "✓ Member removed"
			m.showMembersPopup = false
			return m, tea.Batch(
				loadTeamMembersCmd(m.client, m.teams[m.selectedTeam].ID),
				clearStatusAfter(m.downloadStatusID),
			)
		}
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

	case addChannelMemberMsg:
		m.showAddChannelMemberPopup = false
		m.addChannelMemberInput.Reset()
		m.addChannelMemberResults = nil
		if msg.err != nil {
			if is401(msg.err) {
				return m, func() tea.Msg { return errMsg{err: msg.err} }
			}
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
		} else {
			m.downloadStatus = "✓ Member added to channel"
			// Optimistically append the member so it renders instantly
			msg.member.Role = "Member"
			m.channelMembers = append(m.channelMembers, msg.member)
			m.downloadStatusID++
			if m.viewMode == ModeInfo {
				m.viewport.SetContent(renderInfoContent(&m))
			}
			return m, clearStatusAfter(m.downloadStatusID)
		}
		m.downloadStatusID++
		if m.viewMode == ModeInfo {
			m.viewport.SetContent(renderInfoContent(&m))
		}
		return m, clearStatusAfter(m.downloadStatusID)

	case removeChannelMemberMsg:
		m.showRemoveChannelMemberPopup = false
		if msg.err != nil {
			if is401(msg.err) {
				return m, func() tea.Msg { return errMsg{err: msg.err} }
			}
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
		} else {
			m.downloadStatus = "✓ Member removed from channel"
			// Optimistic update
			var updated []graph.TeamMember
			for _, cm := range m.channelMembers {
				if cm.ID != msg.userID {
					updated = append(updated, cm)
				}
			}
			m.channelMembers = updated
		}
		m.downloadStatusID++
		if m.viewMode == ModeInfo {
			m.viewport.SetContent(renderInfoContent(&m))
		}
		return m, clearStatusAfter(m.downloadStatusID)

	case delayedReloadChannelsMsg:
		return m, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID)

	case delayedReloadTeamsMsg:
		return m, loadTeamsCmd(m.client)

	case directorypicker.SelectedMsg:
		m.showDirPicker = false
		if msg.Path == "" {
			return m, nil
		}
		if m.pickerPurpose == "download" {
			m.prefs.DownloadDir = msg.Path
			savePrefs(m.prefs)
			m.confirmingDownload = true
		} else if m.pickerPurpose == "assignment_upload" {
			info, err := os.Stat(msg.Path)
			if err != nil || info.IsDir() {
				m.downloadStatus = "✗ Please select a file, not a folder"
				m.downloadStatusID++
				return m, clearStatusAfter(m.downloadStatusID)
			}
			return m, uploadAssignmentFileCmd(m.client, m.assignments[m.selectedAssign], msg.Path)
		} else if m.pickerPurpose == "upload" {
			info, err := os.Stat(msg.Path)
			if err != nil || info.IsDir() {
				m.downloadStatus = "✗ Please select a file, not a folder"
				m.downloadStatusID++
				return m, clearStatusAfter(m.downloadStatusID)
			}
			m.uploading = true
			isDM := m.workspace == WorkspaceDMs
			if !isDM && len(m.channels) > 0 {
				teamID := m.teams[m.selectedTeam].ID
				if len(m.folderStack) > 0 {
					folderID := m.folderStack[len(m.folderStack)-1].ID
					return m, uploadFileToFolderCmd(m.client, teamID, folderID, msg.Path)
				}
				// fallback — shouldn't happen with Option B
				channelName := m.channels[m.selectedChan].DisplayName
				return m, uploadFileCmd(m.client, teamID, channelName, msg.Path, false)
			}
			return m, uploadFileCmd(m.client, "", "", msg.Path, true)
		}
		return m, nil

	case directorypicker.CancelledMsg:
		m.showDirPicker = false
		return m, nil

	case markAsReadMsg:
		return m, nil

	case tea.MouseMsg:
		if msg.X < (m.width-5)/3 {
			// Left panel: use wheel to navigate the list
			if msg.Type == tea.MouseWheelUp {
				return m.Update(tea.KeyMsg{Type: tea.KeyUp})
			} else if msg.Type == tea.MouseWheelDown {
				return m.Update(tea.KeyMsg{Type: tea.KeyDown})
			} else if msg.Type == tea.MouseLeft {
				m.focusLeft = true
			}
		} else {
			// Right panel: route to the active viewport
			if m.showThread {
				m.threadViewport, cmd = m.threadViewport.Update(msg)
			} else {
				m.viewport, cmd = m.viewport.Update(msg)
			}
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			if m.viewport.AtTop() && m.messagesBackwardLink != "" && !m.loadingMore && m.viewMode == ModeChat && !m.showThread {
				m.loadingMore = true
				cmds = append(cmds, loadMoreMessagesCmd(m.client, m.messagesBackwardLink))
			}
			if msg.Type == tea.MouseLeft {
				m.focusLeft = false
			}
		}
		return m, tea.Batch(cmds...)

	case unreadStatusMsg:
		changed := false
		if msg.hasUnread {
			if !m.chatUnread[msg.chatID] {
				m.chatUnread[msg.chatID] = true
				changed = true
			}
		} else {
			if m.chatUnread[msg.chatID] {
				delete(m.chatUnread, msg.chatID)
				changed = true
			}
		}
		if changed {
			m.ReSortChats()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	// Always ensure viewport height is correct for current state
	m.recalculateViewportHeight()

	return m, tea.Batch(cmds...)
}
