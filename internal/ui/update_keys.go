package ui

import (
	"fmt"
	"strings"
	"teamsTUI/internal/graph"
	"teamsTUI/internal/helpers"
	"teamsTUI/internal/teams"
	"teamsTUI/internal/ui/components/directorypicker"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	type popupHandler func(tea.KeyMsg) (tea.Model, tea.Cmd, bool)

	popups := []struct {
		active  bool
		handler popupHandler
	}{
		{m.showHelp, m.handleHelpPopup},
		{m.showDeleteMsgPopup, m.handleDeleteMsgPopup},
		{m.showEditPopup, m.handleEditPopup},
		{m.showReactionPicker, m.handleReactionPicker},
		{m.showThread, m.handleThreadView},
		{m.cursorMode && m.workspace == WorkspaceTeams && m.viewMode == ModeChat, m.handleCursorModeTeams},
		{m.cursorMode && m.workspace == WorkspaceDMs && m.viewMode == ModeChat, m.handleCursorModeDMs},
		{m.isSearching, m.handleSearching},
		{m.showDirPicker, m.handleDirPicker},
		{m.showCreateTeamPopup, m.handleCreateTeamPopup},
		{m.showTeamInfo, m.handleTeamInfo},
		{m.showAddChannelMemberPopup, m.handleAddChannelMemberPopup},
		{m.showAddMemberPopup, m.handleAddMemberPopup},
		{m.showRemoveMemberPopup, m.handleRemoveMemberPopup},
		{m.showRemoveChannelMemberPopup, m.handleRemoveChannelMemberPopup},
		{m.showMembersPopup, m.handleMembersPopup},
		{m.showDeleteChannelPopup, m.handleDeleteChannelPopup},
		{m.showDeleteTeamPopup, m.handleDeleteTeamPopup},
		{m.showCreateChannelPopup, m.handleCreateChannelPopup},
		{m.showNewDMPopup, m.handleNewDMPopup},
		{m.showPresenceMenu, m.handlePresenceMenu},
		{m.showCreateFolderPopup, m.handleCreateFolderPopup},
		{m.showDeleteFilePopup, m.handleDeleteFilePopup},
		{m.confirmingDownload, m.handleConfirmingDownload},
	}

	for _, p := range popups {
		if p.active {
			newM, cmd, consumed := p.handler(msg)
			if consumed {
				return newM, cmd
			}
		}
	}

	return m.handleMainSwitch(msg)
}
func (m Model) handleMainSwitch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg.String() {
	case "/":
		if !m.focusLeft && m.viewMode == ModeChat {
			m.isSearching = true
			m.searchInput.Reset()
			m.searchInput.Focus()
			return m, nil
		}

	case "q", "ctrl+c":
		if m.renewalProc != nil {
			helpers.SignalAuthProcess(m.renewalProc)
			m.renewalProc = nil
		}
		return m, tea.Quit

	case "?":
		m.showHelp = !m.showHelp

	case "p":
		m.showPresenceMenu = !m.showPresenceMenu
		m.presenceCursor = 0

	case "n":
		if m.workspace == WorkspaceDMs {
			m.showNewDMPopup = true
			m.newDMQuery.Reset()
			m.newDMResults = nil
			m.newDMErr = ""
			m.newDMCursor = 0
			m.newDMQuery.Focus()
		}

	case "N":
		if m.workspace == WorkspaceTeams {
			m.showCreateTeamPopup = true
			m.createTeamInput.Reset()
			m.createTeamErr = ""
			m.createTeamInput.Focus()
		}

	case "A":
		if m.workspace == WorkspaceTeams && m.focusLeft && m.focusList == 0 {
			m.showHidden = !m.showHidden
			for i, t := range m.teams {
				isHidden := contains(m.prefs.HiddenTeams, t.ID)
				if m.showHidden && isHidden {
					m.selectedTeam = i
					break
				}
				if !m.showHidden && !isHidden {
					m.selectedTeam = i
					break
				}
			}
			return m, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID)
		}
		if m.workspace == WorkspaceTeams && m.focusLeft && m.focusList == 1 && len(m.channels) > 0 {
			m.showHiddenChannels = !m.showHiddenChannels
			teamID := m.teams[m.selectedTeam].ID
			hidden := m.prefs.HiddenChannels[teamID]
			for i, c := range m.channels {
				isHidden := contains(hidden, c.ID)
				if m.showHiddenChannels && isHidden {
					m.selectedChan = i
					break
				}
				if !m.showHiddenChannels && !isHidden {
					m.selectedChan = i
					break
				}
			}
			return m, nil
		}

	case "H":
		if m.workspace == WorkspaceTeams && m.focusLeft && m.focusList == 0 && m.selectedTeam < len(m.teams) {
			teamID := m.teams[m.selectedTeam].ID
			if contains(m.prefs.HiddenTeams, teamID) {
				m.prefs.HiddenTeams = remove(m.prefs.HiddenTeams, teamID)
			} else {
				m.prefs.HiddenTeams = append(m.prefs.HiddenTeams, teamID)
			}
			savePrefs(m.prefs)
			m.selectedTeam = nextVisibleTeam(m.teams, m.prefs.HiddenTeams, m.selectedTeam, m.showHidden, +1)
			return m, nil
		}
		if m.workspace == WorkspaceTeams && m.focusLeft && m.focusList == 1 && len(m.channels) > 0 {
			teamID := m.teams[m.selectedTeam].ID
			ch := m.channels[m.selectedChan]
			hidden := m.prefs.HiddenChannels[teamID]
			if contains(hidden, ch.ID) {
				m.prefs.HiddenChannels[teamID] = remove(hidden, ch.ID)
			} else {
				m.prefs.HiddenChannels[teamID] = append(hidden, ch.ID)
			}
			savePrefs(m.prefs)
			m.selectedChan = nextVisibleChannel(m.channels, m.prefs.HiddenChannels[teamID], m.selectedChan, m.showHiddenChannels, +1)
			return m, nil
		}

	case "u":
		if m.workspace == WorkspaceAssignments && !m.focusLeft {
			a := m.assignments[m.selectedAssign]
			if a.SubmissionStatus != "submitted" && a.SubmissionStatus != "returned" && a.SubmissionStatus != "reassigned" {
				m.showDirPicker = true
				m.pickerPurpose = "assignment_upload"
				m.dirPicker = directorypicker.New(directorypicker.Options{
					Title:       "Select file to submit for assignment",
					InitialPath: m.prefs.DownloadDir,
					Mode:        "file",
					Width:       m.width,
					Height:      m.height,
				})
				m.dirPicker, _ = m.dirPicker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
				return m, nil
			}
		} else if m.workspace == WorkspaceTeams && m.viewMode == ModeFiles && !m.focusLeft {
			m.showDirPicker = true
			m.pickerPurpose = "upload"
			m.dirPicker = directorypicker.New(directorypicker.Options{
				Title:       "Select file to upload",
				InitialPath: m.prefs.DownloadDir,
				Mode:        "file",
				Width:       m.width,
				Height:      m.height,
			})
			m.dirPicker, _ = m.dirPicker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			return m, nil
		} else if m.workspace == WorkspaceDMs && !m.focusLeft {
			m.showDirPicker = true
			m.pickerPurpose = "upload"
			m.dirPicker = directorypicker.New(directorypicker.Options{
				Title:       "Select file to upload",
				InitialPath: m.prefs.DownloadDir,
				Mode:        "file",
				Width:       m.width,
				Height:      m.height,
			})
			m.dirPicker, _ = m.dirPicker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			return m, nil
		}

	case "s":
		if m.workspace == WorkspaceAssignments && !m.focusLeft {
			a := m.assignments[m.selectedAssign]
			if a.SubmissionID != "" && a.SubmissionStatus != "submitted" && a.SubmissionStatus != "returned" && a.SubmissionStatus != "reassigned" {
				return m, submitAssignmentCmd(m.client, a)
			}
		}

	case "S":
		if m.workspace == WorkspaceAssignments && !m.focusLeft {
			a := m.assignments[m.selectedAssign]
			if a.SubmissionID != "" && a.SubmissionStatus == "submitted" {
				return m, undoSubmitAssignmentCmd(m.client, a)
			}
		}

	case "F":
		if !m.focusLeft && m.viewMode == ModeFiles {
			m.showCreateFolderPopup = true
			m.createFolderInput.SetValue("")
			m.createFolderInput.Focus()
			m.createFolderErr = ""
			return m, nil
		}

	case "delete", "backspace":
		if m.workspace == WorkspaceAssignments && !m.focusLeft {
			a := m.assignments[m.selectedAssign]
			if a.SubmissionStatus != "submitted" && a.SubmissionStatus != "returned" && a.SubmissionStatus != "reassigned" {
				f := getAssignFile(a, m.assignFileCursor)
				// Only allow deleting from "My work" (index >= len(RefFiles))
				if f != nil && m.assignFileCursor >= len(a.RefFiles) && f.ID != "" {
					return m, removeAssignmentResourceCmd(m.client, a, f.ID, f.Name)
				}
			}
		} else if !m.focusLeft && m.viewMode == ModeFiles && len(m.files) > 0 {
			m.showDeleteFilePopup = true
			return m, nil
		}

	case "a":
		if !m.focusLeft && m.workspace == WorkspaceTeams && m.viewMode == ModeInfo {
			if m.channelInfo != nil && strings.ToLower(m.channelInfo.MembershipType) == "private" {
				m.showAddChannelMemberPopup = true
				m.addChannelMemberInput.Reset()
				m.addChannelMemberCursor = 0
				m.addChannelMemberErr = ""
				m.addChannelMemberResults = nil
				// Pre-load team members if missing
				if len(m.teamMembers) == 0 {
					return m, loadTeamMembersCmd(m.client, m.teams[m.selectedTeam].ID)
				}
				// Pre-populate with all non-channel members
				excluded := make(map[string]bool)
				for _, cm := range m.channelMembers {
					excluded[cm.ID] = true
				}
				var available []graph.TeamMember
				for _, tm := range m.teamMembers {
					if !excluded[tm.ID] {
						available = append(available, tm)
					}
				}
				m.addChannelMemberResults = available
			}
		}

	case "ctrl+b":
		m.mobileMode = !m.mobileMode

		panelOuterHeight := m.height - 6
		if m.mobileMode {
			panelOuterHeight = m.height - 2
		}
		if panelOuterHeight < 2 {
			panelOuterHeight = 2
		}
		leftInnerHeight := panelOuterHeight - 2

		if m.mobileMode {
			mobileWidth := m.width - 2
			if mobileWidth < 20 {
				mobileWidth = 20
			}
			mobileInnerWidth := mobileWidth - 2
			m.viewport.Width = mobileInnerWidth
			m.threadViewport.Width = mobileInnerWidth
			m.leftVp.Width = mobileInnerWidth
			m.input.SetWidth(mobileInnerWidth - 2)
		} else {
			available := m.width - 5
			leftOuterWidth := available / 3
			rightOuterWidth := available - leftOuterWidth
			rightInnerWidth := rightOuterWidth - 2

			m.viewport.Width = rightInnerWidth
			m.threadViewport.Width = rightInnerWidth
			m.leftVp.Width = leftOuterWidth - 2
			m.input.SetWidth(rightInnerWidth - 2)
		}
		m.leftVp.Height = leftInnerHeight
		m.input.MaxHeight = leftInnerHeight / 3
		m.recalculateViewportHeight()

	case "tab":
		if m.cursorMode {
			return m, nil
		}
		if !m.mobileMode {
			m.focusLeft = !m.focusLeft
		}

	case "1":
		if m.cursorMode {
			return m, nil
		}
		m.workspace = WorkspaceTeams
		m.focusLeft = true
		m.focusList = 0
		m.cursorOnDMHeader = false
		m.cursorOnGroupHeader = false

	case "2":
		if m.cursorMode {
			return m, nil
		}
		m.workspace = WorkspaceDMs
		m.focusLeft = true
		m.cursorOnDMHeader = true
		m.cursorOnGroupHeader = false
		if !m.chatsLoaded {
			m.loading = true
			cmds = append(cmds, loadChatsCmd(m.client))
		} else if len(m.chats) > 0 {
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
		}

	case "3":
		if m.cursorMode {
			return m, nil
		}
		m.workspace = WorkspaceActivity
		m.focusLeft = true
		m.cursorOnDMHeader = false
		m.cursorOnGroupHeader = false
		if !m.notifLoaded {
			m.loading = true
			m.selectedNotif = 0
			cmds = append(cmds, loadNotificationsCmd(m.client))
		}

	case "4":
		if m.cursorMode {
			return m, nil
		}
		m.workspace = WorkspaceAssignments
		m.focusLeft = true
		m.cursorOnDMHeader = false
		m.cursorOnGroupHeader = false
		if !m.assignLoaded {
			m.loading = true
			m.selectedAssign = 0
			cmds = append(cmds, loadAssignmentsCmd(m.client))
		}

	case "esc":
		if m.previewing {
			m.previewing = false
			m.previewContent = ""
			m.previewFileName = ""
			m.downloadStatus = ""
			m.recalculateViewportHeight()
			m.viewport.SetContent(renderFilesContent(&m))
			return m, nil
		}
		if m.searchQuery != "" {
			m.searchQuery = ""
			var content string
			if m.workspace == WorkspaceDMs {
				content = formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
			} else {
				content = formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode)
			}
			m.viewport.SetContent(content)
			return m, nil
		}
		if !m.focusLeft && m.viewMode == ModeInfo {
			m.viewMode = ModeChat
			m.loading = true
			m.messagesBackwardLink = ""
			m.loadingMore = false
			return m, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID, 200)
		}
		if !m.focusLeft {
			m.focusLeft = true
		}

	case "g", "home":
		if !m.focusLeft {
			if m.viewMode == ModeFiles {
				m.selectedFile = 0
				m.viewport.SetContent(renderFilesContent(&m))
			} else {
				m.viewport.GotoTop()
			}
		}

	case "G", "end":
		if !m.focusLeft {
			if m.viewMode == ModeFiles {
				if len(m.files) > 0 {
					m.selectedFile = len(m.files) - 1
				}
				m.viewport.SetContent(renderFilesContent(&m))
			} else {
				m.viewport.GotoBottom()
			}
		}

	case "up", "k":
		if m.workspace == WorkspaceAssignments && !m.focusLeft {
			if m.assignFileCursor > 0 {
				m.assignFileCursor--
			}
			return m, nil
		}
		if m.focusLeft {
			if m.workspace == WorkspaceDMs {
				if m.cursorOnGroupHeader {
					m.cursorOnGroupHeader = false
					dmChats := dmChatIndices(m.chats)
					if !m.dmSectionCollapsed && len(dmChats) > 0 {
						m.selectedChat = dmChats[len(dmChats)-1]
					} else {
						m.cursorOnDMHeader = true
					}
				} else if m.cursorOnDMHeader {
					// Already at top, do nothing
				} else {
					dmChats := dmChatIndices(m.chats)
					groupChats := groupChatIndices(m.chats)
					allVisible := visibleChatIndices(m.chats, m.dmSectionCollapsed, m.groupSectionCollapsed)
					pos := indexOf(allVisible, m.selectedChat)
					if pos > 0 {
						prevIdx := allVisible[pos-1]
						if isInGroup(m.chats, m.selectedChat) && isInDMs(m.chats, prevIdx) {
							m.cursorOnGroupHeader = true
						} else {
							m.selectedChat = prevIdx
							if m.viewMode == ModeChat {
								m.loading = true
								m.loadedConvID = m.chats[m.selectedChat].ID
								m.messagesBackwardLink = ""
								m.loadingMore = false
								cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
							} else if m.viewMode == ModeFiles {
								m.loadedConvID = m.chats[m.selectedChat].ID
								m.loading = true
								m.messagesBackwardLink = ""
								m.loadingMore = false
								cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
							}
						}
					} else if isInGroup(m.chats, m.selectedChat) {
						m.cursorOnGroupHeader = true
					} else if len(dmChats) > 0 && m.selectedChat == dmChats[0] {
						m.cursorOnDMHeader = true
					}
					_ = groupChats
				}
			} else if m.workspace == WorkspaceActivity {
				if len(m.notifications) > 0 && m.selectedNotif > 0 {
					m.selectedNotif--
				}
			} else if m.workspace == WorkspaceAssignments {
				filtered := filteredAssignments(m)
				if len(filtered) > 0 && m.selectedAssign > 0 {
					m.selectedAssign--
				}
			} else if m.focusList == 0 && len(m.teams) > 0 {
				prev := nextVisibleTeam(m.teams, m.prefs.HiddenTeams, m.selectedTeam, m.showHidden, -1)
				if prev != m.selectedTeam {
					m.selectedTeam = prev
					m.loading = true
					m.teamMembers = nil
					cmds = append(cmds, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID))
				}
			} else if m.focusList == 1 && len(m.channels) > 0 {
				teamID := m.teams[m.selectedTeam].ID
				prev := nextVisibleChannel(m.channels, m.prefs.HiddenChannels[teamID], m.selectedChan, m.showHiddenChannels, -1)
				if prev != m.selectedChan {
					m.selectedChan = prev
					m.loadedConvID = ""
					m.viewMode = ModeChat
					m.viewport.SetContent("")
				}
			}
		} else {
			if m.viewMode == ModeFiles {
				if m.selectedFile > 0 {
					m.selectedFile--
					m.viewport.SetContent(renderFilesContent(&m))
				}
			} else if m.viewMode == ModeInfo {
				if m.channelMemberCursor > 0 {
					m.channelMemberCursor--
					m.viewport.SetContent(renderInfoContent(&m))
				} else {
					m.viewport, cmd = m.viewport.Update(msg)
					cmds = append(cmds, cmd)
				}
			} else {
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
				if m.viewport.AtTop() && m.messagesBackwardLink != "" && !m.loadingMore && m.viewMode == ModeChat {
					m.loadingMore = true
					cmds = append(cmds, loadMoreMessagesCmd(m.client, m.messagesBackwardLink))
				}
			}
		}

	case "down", "j":
		if m.workspace == WorkspaceAssignments && !m.focusLeft {
			filtered := filteredAssignments(m)
			if m.selectedAssign >= 0 && m.selectedAssign < len(filtered) {
				a := filtered[m.selectedAssign]
				total := len(a.RefFiles) + len(a.MyFiles)
				if total > 0 && m.assignFileCursor < total-1 {
					m.assignFileCursor++
				}
			}
			return m, nil
		}
		if m.focusLeft {
			if m.workspace == WorkspaceDMs {
				if m.cursorOnDMHeader {
					m.cursorOnDMHeader = false
					dmChats := dmChatIndices(m.chats)
					if !m.dmSectionCollapsed && len(dmChats) > 0 {
						m.selectedChat = dmChats[0]
						if m.viewMode == ModeChat {
							m.loading = true
							m.loadedConvID = m.chats[m.selectedChat].ID
							m.messagesBackwardLink = ""
							m.loadingMore = false
							cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
						} else if m.viewMode == ModeFiles {
							m.loadedConvID = m.chats[m.selectedChat].ID
							m.loading = true
							m.messagesBackwardLink = ""
							m.loadingMore = false
							cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
						}
					} else {
						groupChats := groupChatIndices(m.chats)
						if len(groupChats) > 0 {
							m.cursorOnGroupHeader = true
						}
					}
				} else if m.cursorOnGroupHeader {
					m.cursorOnGroupHeader = false
					groupChats := groupChatIndices(m.chats)
					if !m.groupSectionCollapsed && len(groupChats) > 0 {
						m.selectedChat = groupChats[0]
						if m.viewMode == ModeChat {
							m.loading = true
							m.loadedConvID = m.chats[m.selectedChat].ID
							m.messagesBackwardLink = ""
							m.loadingMore = false
							cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
						} else if m.viewMode == ModeFiles {
							m.loadedConvID = m.chats[m.selectedChat].ID
							m.loading = true
							m.messagesBackwardLink = ""
							m.loadingMore = false
							cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
						}
					}
				} else {
					allVisible := visibleChatIndices(m.chats, m.dmSectionCollapsed, m.groupSectionCollapsed)
					pos1 := indexOf(allVisible, m.selectedChat)
					if pos1 < len(allVisible)-1 {
						nextIdx := allVisible[pos1+1]
						if isInDMs(m.chats, m.selectedChat) && isInGroup(m.chats, nextIdx) {
							m.cursorOnGroupHeader = true
						} else {
							m.selectedChat = nextIdx
							if m.viewMode == ModeChat {
								m.loading = true
								m.loadedConvID = m.chats[m.selectedChat].ID
								m.messagesBackwardLink = ""
								m.loadingMore = false
								cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
							} else if m.viewMode == ModeFiles {
								m.loadedConvID = m.chats[m.selectedChat].ID
								m.loading = true
								m.messagesBackwardLink = ""
								m.loadingMore = false
								cmds = append(cmds, loadMessagesCmd(m.client, "", m.loadedConvID, 200))
							}
						}
					} else if isInDMs(m.chats, m.selectedChat) {
						groupChats := groupChatIndices(m.chats)
						if len(groupChats) > 0 {
							m.cursorOnGroupHeader = true
						}
					}
				}
			} else if m.workspace == WorkspaceActivity {
				if len(m.notifications) > 0 && m.selectedNotif < len(m.notifications)-1 {
					m.selectedNotif++
				}
			} else if m.workspace == WorkspaceAssignments {
				filtered := filteredAssignments(m)
				if len(filtered) > 0 && m.selectedAssign < len(filtered)-1 {
					m.selectedAssign++
				}
			} else if m.focusList == 0 && len(m.teams) > 0 {
				next := nextVisibleTeam(m.teams, m.prefs.HiddenTeams, m.selectedTeam, m.showHidden, +1)
				if next != m.selectedTeam {
					m.selectedTeam = next
					m.loading = true
					m.teamMembers = nil
					cmds = append(cmds, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID))
				}
			} else if m.focusList == 1 && len(m.channels) > 0 {
				teamID := m.teams[m.selectedTeam].ID
				next := nextVisibleChannel(m.channels, m.prefs.HiddenChannels[teamID], m.selectedChan, m.showHiddenChannels, +1)
				if next != m.selectedChan {
					m.selectedChan = next
					m.loadedConvID = ""
					m.viewMode = ModeChat
					m.viewport.SetContent("")
				}
			}
		} else {
			if m.viewMode == ModeFiles {
				if m.selectedFile < len(m.files)-1 {
					m.selectedFile++
					m.viewport.SetContent(renderFilesContent(&m))
				}
			} else if m.viewMode == ModeInfo {
				if m.channelMemberCursor < len(m.channelMembers)-1 {
					m.channelMemberCursor++
					m.viewport.SetContent(renderInfoContent(&m))
				} else {
					m.viewport, cmd = m.viewport.Update(msg)
					cmds = append(cmds, cmd)
				}
			} else {
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
			}
		}

	case "left", "h":
		if m.focusLeft && m.workspace == WorkspaceActivity {
			if m.activityFilter > NotifFilterAll {
				m.activityFilter--
				m.selectedNotif = 0
			}
		} else if m.focusLeft && m.workspace == WorkspaceAssignments {
			if m.assignFilter == FilterCompleted {
				m.assignFilter = FilterOverdue
			} else if m.assignFilter == FilterOverdue {
				m.assignFilter = FilterUpcoming
			}
			m.selectedAssign = 0
		} else if !m.focusLeft {
			if m.viewMode == ModeInfo {
				m.viewMode = ModeChat
				m.loading = true
				m.messagesBackwardLink = ""
				m.loadingMore = false
				cmds = append(cmds, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID, 200))
			} else if m.viewMode == ModeFiles && len(m.folderStack) > 0 {
				m.folderStack = m.folderStack[:len(m.folderStack)-1]
				m.selectedFiles = make(map[int]bool)
				if len(m.folderStack) == 0 {
					m.currentFilesDriveID = ""
					// Check cache for root
					cacheKey := "root:" + m.channels[m.selectedChan].ID
					if cached, ok := m.folderCache[cacheKey]; ok {
						m.files = cached
						m.selectedFile = 0
						m.viewport.SetContent(renderFilesContent(&m))
						m.viewport.GotoTop()
					} else {
						m.loading = true
						cmds = append(cmds, loadFilesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].DisplayName, m.channels[m.selectedChan].ID))
					}
				} else {
					if len(m.folderStack) == 0 {
						return m, nil
					}
					parent := m.folderStack[len(m.folderStack)-1]
					m.currentFilesDriveID = parent.DriveID
					// Check cache
					if cached, ok := m.folderCache[parent.ID]; ok {
						m.files = cached
						m.selectedFile = 0
						m.viewport.SetContent(renderFilesContent(&m))
						m.viewport.GotoTop()
					} else {
						m.loading = true
						cmds = append(cmds, loadFolderCmd(m.client, m.teams[m.selectedTeam].ID, parent))
					}
				}
			} else {
				m.focusLeft = true
			}
		} else {
			m.focusList = 0
		}

	case "right", "l":
		if m.focusLeft && m.workspace == WorkspaceActivity {
			if m.activityFilter < NotifFilterTagMentions {
				m.activityFilter++
				m.selectedNotif = 0
			}
		} else if m.focusLeft && m.workspace == WorkspaceAssignments {
			if m.assignFilter == FilterUpcoming {
				m.assignFilter = FilterOverdue
			} else if m.assignFilter == FilterOverdue {
				m.assignFilter = FilterCompleted
			}
			m.selectedAssign = 0
		} else if m.focusLeft && m.workspace == WorkspaceTeams {
			m.focusList = 1
		}

	case "f":
		if !m.isTyping {
			m.isTyping = false // safety reset
			if m.workspace == WorkspaceTeams && len(m.channels) > 0 {
				if m.viewMode == ModeChat || m.viewMode == ModeInfo {
					m.viewMode = ModeFiles
					m.folderStack = nil
					m.currentFilesDriveID = ""
					m.loading = true
					// Start a fresh auto-refresh timer on entering Files so the
					// first background refresh happens 30s after entering.
					cmds = append(cmds, loadChannelRootCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID, m.channels[m.selectedChan].DisplayName))
					cmds = append(cmds, filesRefreshTickCmd())
				} else {
					m.viewMode = ModeChat
					m.loading = true
					m.messagesBackwardLink = ""
					m.loadingMore = false
					cmds = append(cmds, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID, 200))
				}
			} else if m.workspace == WorkspaceDMs && len(m.chats) > 0 {
				activeID := m.activeConversationID()
				if m.viewMode == ModeChat {
					// If we switch chats without pressing Enter, it still works
					if m.loadedConvID != activeID {
						m.loading = true
						m.loadedConvID = activeID
						m.viewMode = ModeChat // Must reset to ModeChat since we're requesting messages from scratch
						m.messagesBackwardLink = ""
						m.loadingMore = false
						cmds = append(cmds, loadMessagesCmd(m.client, "", activeID, 200))
					} else {
						// Local aggregation: zero network, uses what's already in m.messages.
						m.viewMode = ModeFiles
						m.folderStack = nil
						m.files = teams.AggregateChatAttachments(m.messages)
						m.selectedFile = 0
						m.viewport.SetContent(renderFilesContent(&m))
						m.viewport.GotoTop()
					}
				} else {
					m.viewMode = ModeChat
					if m.loadedConvID != activeID {
						m.loading = true
						m.loadedConvID = activeID
						m.messagesBackwardLink = ""
						m.loadingMore = false
						cmds = append(cmds, loadMessagesCmd(m.client, "", activeID, 200))
					} else {
						var fresh string
						msgsToRender := m.messages
						if m.searchQuery != "" {
							msgsToRender = m.filterMessages(m.messages, m.searchQuery)
						}
						if m.workspace == WorkspaceDMs {
							fresh = formatMessagesDM(msgsToRender, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
						} else {
							fresh = formatMessagesWithCursor(msgsToRender, m.viewport.Width, m.messageCursor, m.cursorMode)
						}
						m.viewport.SetContent(fresh)
					}
				}
			}
		}

	case " ":
		// Select/deselect file for multi-download
		if !m.focusLeft && m.viewMode == ModeFiles && len(m.files) > 0 {
			if m.selectedFiles[m.selectedFile] {
				delete(m.selectedFiles, m.selectedFile)
			} else {
				m.selectedFiles[m.selectedFile] = true
			}
			m.viewport.SetContent(renderFilesContent(&m))
		}

	case "o":
		if m.workspace == WorkspaceAssignments && !m.focusLeft {
			filtered := filteredAssignments(m)
			if len(filtered) > 0 && m.selectedAssign >= 0 && m.selectedAssign < len(filtered) {
				a := filtered[m.selectedAssign]
				f := getAssignFile(a, m.assignFileCursor)
				if f != nil {
					targets := []graph.DriveItem{{Name: f.Name, WebUrl: f.FileUrl}}
					m.downloadTargets = targets
					m.confirmingDownload = true
				}
			}
			return m, nil
		}
		// In WorkspaceActivity, right panel: navigate to notification's channel
		if m.workspace == WorkspaceActivity && !m.focusLeft {
			if m.selectedNotif < len(m.notifications) {
				n := m.notifications[m.selectedNotif]
				if n.SourceThread != "" {
					teamID, known := m.channelToTeam[n.SourceThread]
					if known {
						// Cache hit — direct navigation without network
						for i, t := range m.teams {
							if t.ID == teamID {
								m.selectedTeam = i
								break
							}
						}
						m.workspace = WorkspaceTeams
						m.focusLeft = false
						m.recalculateViewportHeight()
						m.viewMode = ModeChat
						m.loadedConvID = n.SourceThread
						m.loading = true
						m.messagesBackwardLink = ""
						m.loadingMore = false
						m.forceScrollBottom = true
						cmds = append(cmds, loadMessagesCmd(m.client, teamID, n.SourceThread, 200))
					} else {
						// Cache miss — async search across all teams
						cmds = append(cmds, navigateToThreadCmd(m.client, m.teams, n.SourceThread))
					}
				}
			}
			return m, tea.Batch(cmds...)
		}
		// Open download confirmation popup
		if !m.confirmingDownload && !m.focusLeft && m.viewMode == ModeFiles && len(m.files) > 0 {
			var targets []graph.DriveItem
			if len(m.selectedFiles) > 0 {
				for idx := range m.selectedFiles {
					if idx < len(m.files) && m.files[idx].Folder == nil {
						targets = append(targets, m.files[idx])
					}
				}
			} else if m.files[m.selectedFile].Folder == nil {
				targets = append(targets, m.files[m.selectedFile])
			}
			if len(targets) > 0 {
				m.downloadTargets = targets
				m.confirmingDownload = true
			}
		}

	case "v":
		// File preview: text in TUI, rest in browser
		if !m.focusLeft && m.viewMode == ModeChat && len(m.messages) > 0 {
			if !m.cursorMode {
				m.downloadStatus = "Press 'c' to enter Cursor Mode and select the message first"
				m.downloadStatusID++
				return m, clearStatusAfter(m.downloadStatusID)
			}

			var msg graph.Message
			if m.workspace == WorkspaceDMs {
				validMsgs := validDMMessages(m.messages)
				if m.messageCursor >= 0 && m.messageCursor < len(validMsgs) {
					msg = validMsgs[m.messageCursor]
				}
			} else {
				roots := rootMessages(m.messages)
				if m.messageCursor >= 0 && m.messageCursor < len(roots) {
					msg = roots[m.messageCursor]
				}
			}

			if msg.ID != "" {
				if len(msg.Attachments) > 0 {
					m.downloadStatus = "Downloading attachments..."
					m.downloadStatusID++
					return m, tea.Batch(
						openAttachmentsCmd(m.client, msg.Attachments),
						clearStatusAfter(m.downloadStatusID),
					)
				} else {
					m.downloadStatus = "No attachments in selected message"
					m.downloadStatusID++
					return m, clearStatusAfter(m.downloadStatusID)
				}
			}
		} else if !m.focusLeft && m.viewMode == ModeFiles && len(m.files) > 0 && !m.previewing {
			if len(m.selectedFiles) > 1 {
				m.downloadStatus = "Only one file can be previewed at a time"
				m.downloadStatusID++
				if m.viewMode == ModeFiles {
					m.viewport.SetContent(renderFilesContent(&m))
				}
				return m, clearStatusAfter(m.downloadStatusID)
			}
			item := m.files[m.selectedFile]
			if item.Folder != nil {
				// If it's a folder, no preview — ignore
			} else {
				driveID := m.currentFilesDriveID
				teamID := m.teams[m.selectedTeam].ID
				m.loading = true
				cmds = append(cmds, previewFileCmd(m.client, item, driveID, teamID))
			}
		}

	case "r":
		if m.workspace == WorkspaceActivity && m.notifErr != nil {
			m.notifErr = nil
			m.notifLoaded = false
			return m, loadNotificationsCmd(m.client)
		}

	case "c", "C":
		if (m.workspace == WorkspaceTeams || m.workspace == WorkspaceDMs) && m.viewMode == ModeChat && !m.isTyping && !m.focusLeft {
			m.cursorMode = !m.cursorMode
			m.messageCursor = 0
			var content string
			if m.workspace == WorkspaceDMs {
				content = formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
			} else {
				content = formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode)
			}
			m.viewport.SetContent(content)
		} else if m.workspace == WorkspaceTeams && m.focusList == 1 && m.teamThreadID != "" {
			m.showCreateChannelPopup = true
			m.createChannelInput.Reset()
			m.createChannelErr = ""
			m.createChannelStep = 0
			m.createChannelType = "Standard"
			m.createChannelInput.Focus()
		} else if !m.isTyping && m.workspace == WorkspaceDMs && m.viewMode == ModeFiles {
			m.loading = true
			m.folderStack = nil
			// Load full history (1000 messages is the practical limit per chunk without hanging)
			m.messagesBackwardLink = ""
			m.loadingMore = false
			cmds = append(cmds, loadMessagesCmd(m.client, "", m.activeConversationID(), 1000))
		}

	case "X", "x":
		if !m.focusLeft && m.viewMode == ModeInfo && len(m.channelMembers) > 0 {
			member := m.channelMembers[m.channelMemberCursor]
			if member.ID != m.selfID && m.channelInfo != nil && strings.ToLower(m.channelInfo.MembershipType) == "private" {
				m.showRemoveChannelMemberPopup = true
			}
		} else if m.workspace == WorkspaceTeams && m.focusList == 1 && len(m.channels) > 0 {
			ch := m.channels[m.selectedChan]
			if !strings.EqualFold(ch.DisplayName, "General") {
				m.showDeleteChannelPopup = true
			}
		}

	case "D", "d":
		// Delete team — only when focusList==0
		if m.workspace == WorkspaceTeams && m.focusList == 0 && len(m.teams) > 0 {
			m.showDeleteTeamPopup = true
		}

	case "i":
		// Full UI protection: only works in ModeChat
		if !m.focusLeft && m.viewMode == ModeChat && m.activeConversationID() != "" {
			m.isTyping = true
			m.input.Focus()
		}

	case "I":
		if m.workspace == WorkspaceTeams && m.focusList == 0 && len(m.teams) > 0 {
			return m, loadTeamInfoCmd(m.client, m.teams[m.selectedTeam].ID)
		} else if !m.focusLeft && m.workspace == WorkspaceTeams && m.focusList == 1 && len(m.channels) > 0 {
			if m.viewMode == ModeInfo {
				m.viewMode = ModeChat
				m.loading = true
				m.messagesBackwardLink = ""
				m.loadingMore = false
				return m, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID, 200)
			} else {
				m.viewMode = ModeInfo
				m.channelInfo = nil
				m.channelMembers = nil
				m.viewport.SetContent("Loading info...")
				return m, tea.Batch(
					loadChannelInfoCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].ID),
					loadChannelMembersCmd(m.client, m.channels[m.selectedChan].ID),
				)
			}
		}

	case "L":
		if m.workspace == WorkspaceTeams && m.focusList == 0 && len(m.teams) > 0 {
			teamID := m.teams[m.selectedTeam].ID
			link := ""
			if m.teamInfo != nil && m.teamInfo.WebUrl != "" {
				link = m.teamInfo.WebUrl
			} else {
				// Fallback to construction if teamInfo hasn't been loaded
				link = fmt.Sprintf("https://teams.microsoft.com/l/team/%s/conversations?groupId=%s", m.teamThreadID, teamID)
			}
			err := copyToClipboard(link)
			if err != nil {
				m.downloadStatus = "✗ Failed to copy link: " + err.Error()
			} else {
				m.downloadStatus = "✓ Link copied to clipboard"
			}
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}

	case "M":
		if m.workspace == WorkspaceTeams && m.focusList == 0 && len(m.teams) > 0 {
			m.membersLoading = true
			m.membersLoadSilent = false
			return m, loadTeamMembersCmd(m.client, m.teams[m.selectedTeam].ID)
		}

	case "enter":
		if m.cursorOnDMHeader {
			m.dmSectionCollapsed = !m.dmSectionCollapsed
			m.prefs.DMSectionCollapsed = m.dmSectionCollapsed
			savePrefs(m.prefs)
			return m, nil
		}
		if m.cursorOnGroupHeader {
			m.groupSectionCollapsed = !m.groupSectionCollapsed
			m.prefs.GroupSectionCollapsed = m.groupSectionCollapsed
			savePrefs(m.prefs)
			return m, nil
		}
		if m.focusLeft && m.workspace == WorkspaceActivity && len(m.notifications) > 0 {
			n := &m.notifications[m.selectedNotif]
			if !n.IsRead {
				n.IsRead = true
				cmds = append(cmds, markNotifReadCmd(m.client, n.ID))
			}
			m.focusLeft = false
		} else if m.focusLeft && m.workspace == WorkspaceAssignments {
			m.focusLeft = false
			m.assignFileCursor = 0
			filtered := filteredAssignments(m)
			if len(filtered) > 0 && m.selectedAssign >= 0 && m.selectedAssign < len(filtered) {
				a := filtered[m.selectedAssign]
				cmds = append(cmds, loadAssignmentDetailCmd(m.client, a.ClassID, a.ID))
			}
		} else if !m.focusLeft && m.workspace == WorkspaceAssignments {
			filtered := filteredAssignments(m)
			if len(filtered) > 0 && m.selectedAssign >= 0 && m.selectedAssign < len(filtered) {
				a := filtered[m.selectedAssign]
				f := getAssignFile(a, m.assignFileCursor)
				if f != nil {
					return m, openAssignmentFileCmd(m.client, *f)
				}
			}
			return m, nil
		} else if m.focusLeft && m.workspace == WorkspaceDMs && len(m.chats) > 0 {
			m.loading = true
			m.focusLeft = false
			m.recalculateViewportHeight()
			m.replyToMsg = nil
			m.isTyping = false
			m.viewMode = ModeChat // MUST RESET
			m.selectedFiles = make(map[int]bool)
			m.folderStack = nil
			chatID := m.chats[m.selectedChat].ID
			m.loadedConvID = chatID
			if m.chatUnread[chatID] {
				delete(m.chatUnread, chatID) // Clear badge on open
				m.ReSortChats()
			}
			m.messagesBackwardLink = ""
			m.loadingMore = false
			m.forceScrollBottom = true
			cmds = append(cmds, loadMessagesCmd(m.client, "", chatID, 200))
		} else if m.focusLeft && m.workspace == WorkspaceTeams && m.focusList == 0 {
			if len(m.teams) > 0 {
				m.focusList = 1
			}
		} else if m.focusLeft && m.focusList == 1 && len(m.channels) > 0 {
			m.loading = true
			m.focusLeft = false
			m.recalculateViewportHeight()
			m.replyToMsg = nil
			m.isTyping = false
			m.viewMode = ModeChat // MUST RESET
			m.selectedFiles = make(map[int]bool)
			m.folderStack = nil
			chanID := m.channels[m.selectedChan].ID
			m.loadedConvID = chanID
			m.messagesBackwardLink = ""
			m.loadingMore = false
			m.forceScrollBottom = true
			cmds = append(cmds, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, chanID, 200))
			// Pre-load team members for mention resolution if not already loaded
			if len(m.teamMembers) == 0 {
				m.membersLoadSilent = true
				cmds = append(cmds, loadTeamMembersCmd(m.client, m.teams[m.selectedTeam].ID))
			}
		} else if !m.focusLeft && m.viewMode == ModeFiles && len(m.files) > 0 {
			selected := m.files[m.selectedFile]
			if selected.RemoteItem != nil {
				node := FolderNode{
					ID:      selected.RemoteItem.ID,
					Name:    selected.Name,
					DriveID: selected.RemoteItem.ParentReference.DriveID,
				}
				m.folderStack = appendFolderNode(m.folderStack, node)
				m.currentFilesDriveID = node.DriveID
				// Check cache
				if cached, ok := m.folderCache[node.ID]; ok {
					m.files = cached
					m.selectedFile = 0
					m.selectedFiles = make(map[int]bool)
					m.viewport.SetContent(renderFilesContent(&m))
					m.viewport.GotoTop()
				} else {
					m.loading = true
					cmds = append(cmds, loadFolderCmd(m.client, m.teams[m.selectedTeam].ID, node))
				}
			} else if selected.Folder != nil {
				currentDriveID := ""
				if len(m.folderStack) > 0 {
					currentDriveID = m.folderStack[len(m.folderStack)-1].DriveID
				}
				node := FolderNode{ID: selected.ID, Name: selected.Name, DriveID: currentDriveID}
				m.folderStack = appendFolderNode(m.folderStack, node)
				m.currentFilesDriveID = currentDriveID
				// Check cache
				if cached, ok := m.folderCache[node.ID]; ok {
					m.files = cached
					m.selectedFile = 0
					m.selectedFiles = make(map[int]bool)
					m.viewport.SetContent(renderFilesContent(&m))
					m.viewport.GotoTop()
				} else {
					m.loading = true
					cmds = append(cmds, loadFolderCmd(m.client, m.teams[m.selectedTeam].ID, node))
				}
			} else {
				// It's a file, open it
				link := selected.DownloadUrl
				if link == "" {
					link = selected.WebUrl
				}
				if link != "" {
					openBrowser(link)
				}
			}
		}
	}
	return m, tea.Batch(cmds...)
}
