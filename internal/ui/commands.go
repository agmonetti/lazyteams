package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"teamsTUI/internal/graph"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const pollInterval = 3
const presenceInterval = 60

func reloadChannelsAfterDelayCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return delayedReloadChannelsMsg{}
	})
}

func reloadTeamsAfterShortDelayCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return delayedReloadTeamsMsg{}
	})
}

func loadTeamInfoCmd(client *graph.Client, teamID string) tea.Cmd {
	return func() tea.Msg {
		team, err := client.GetTeamInfo(teamID)
		if err != nil {
			return teamInfoErrMsg{err}
		}
		return teamInfoMsg{team}
	}
}

func loadChannelInfoCmd(client *graph.Client, teamID, channelID string) tea.Cmd {
	return func() tea.Msg {
		ch, err := client.GetChannelInfo(teamID, channelID)
		if err != nil {
			return channelInfoErrMsg{err}
		}
		return channelInfoMsg{ch}
	}
}

func loadChannelMembersCmd(client *graph.Client, channelThreadID string) tea.Cmd {
	return func() tea.Msg {
		members, err := client.GetChannelMembers(channelThreadID)
		if err != nil {
			return channelMembersErrMsg{err}
		}
		return channelMembersMsg{members}
	}
}

func loadTeamMembersCmd(client *graph.Client, teamGUID string) tea.Cmd {
	return func() tea.Msg {
		members, err := client.GetTeamMembers(teamGUID)
		if err != nil {
			return teamMembersErrMsg{err}
		}
		return teamMembersMsg{members}
	}
}

func addMemberCmd(client *graph.Client, teamThreadID, teamGUID, userMRI string) tea.Cmd {
	return func() tea.Msg {
		err := client.AddTeamMember(teamThreadID, teamGUID, userMRI)
		return addMemberMsg{err}
	}
}

func removeMemberCmd(client *graph.Client, teamThreadID, teamGUID, userID string) tea.Cmd {
	return func() tea.Msg {
		err := client.RemoveTeamMember(teamThreadID, teamGUID, userID)
		return removeMemberMsg{err}
	}
}

func addChannelMemberCmd(client *graph.Client, teamGUID, channelThreadID string, target graph.TeamMember) tea.Cmd {
	return func() tea.Msg {
		tenantID := client.GetTenantID()
		err := client.AddChannelMember(teamGUID, channelThreadID, target.ID, tenantID)
		return addChannelMemberMsg{err: err, member: target}
	}
}

func removeChannelMemberCmd(client *graph.Client, teamGUID, channelThreadID, userID string) tea.Cmd {
	return func() tea.Msg {
		tenantID := client.GetTenantID()
		err := client.RemoveChannelMember(teamGUID, channelThreadID, userID, tenantID)
		return removeChannelMemberMsg{err: err, userID: userID}
	}
}

func createTeamCmd(client *graph.Client, name string) tea.Cmd {
	return func() tea.Msg {
		err := client.CreateTeam(name)
		return createTeamMsg{err}
	}
}

func createChannelCmd(client *graph.Client, teamGUID, teamThreadID, name, channelType string) tea.Cmd {
	return func() tea.Msg {
		err := client.CreateChannel(teamGUID, teamThreadID, name, channelType)
		return createChannelMsg{err}
	}
}

func deleteChannelCmd(client *graph.Client, teamThreadID, channelThreadID string) tea.Cmd {
	return func() tea.Msg {
		err := client.DeleteChannel(teamThreadID, channelThreadID)
		return deleteChannelMsg{err}
	}
}

func deleteTeamCmd(client *graph.Client, teamThreadID string) tea.Cmd {
	return func() tea.Msg {
		err := client.DeleteTeam(teamThreadID)
		return deleteTeamMsg{err}
	}
}

func reloadTeamsAfterDelayCmd() tea.Cmd {
	return tea.Tick(35*time.Second, func(t time.Time) tea.Msg {
		return reloadTeamsAfterCreateMsg{}
	})
}

func launchAuthHelperCmd(expiredToken string) tea.Cmd {
	return func() tea.Msg {
		exe, err := os.Executable()
		if err != nil {
			return tokenRenewedMsg{err: err}
		}
		authHelper := filepath.Join(filepath.Dir(exe), "msTTui-auth")
		cmd := exec.Command(authHelper, "--renew", expiredToken)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return tokenRenewedMsg{err: err}
		}
		return tokenRenewedMsg{err: nil}
	}
}

func reloadTokensCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		err := client.ReloadTokens()
		return tokenRenewedMsg{err: err}
	}
}

func searchUsersCmd(client *graph.Client, query string) tea.Cmd {
	return func() tea.Msg {
		results, err := client.SearchUsers(query)
		if err != nil {
			return searchUsersErrMsg{err}
		}
		return searchUsersMsg{results}
	}
}

func createDMCmd(client *graph.Client, selfID, targetID string) tea.Cmd {
	return func() tea.Msg {
		chat, err := client.CreateOneOnOneChat(selfID, targetID)
		if err != nil {
			return createDMErrMsg{err}
		}
		return createDMMsg{chat}
	}
}

func uploadFileCmd(client *graph.Client, teamID, channelName, filePath string, isDM bool) tea.Cmd {
	return func() tea.Msg {
		if strings.HasPrefix(filePath, "~/") {
			home, _ := os.UserHomeDir()
			filePath = filepath.Join(home, filePath[2:])
		}
		var item graph.DriveItem
		var err error
		if isDM {
			item, err = client.UploadFileToOneDrive(filePath)
		} else {
			item, err = client.UploadFileToChannel(teamID, channelName, filePath)
		}
		return uploadDoneMsg{item: item, err: err}
	}
}

func uploadFileToFolderCmd(client *graph.Client, teamID, folderID, filePath string) tea.Cmd {
	return func() tea.Msg {
		if strings.HasPrefix(filePath, "~/") {
			home, _ := os.UserHomeDir()
			filePath = filepath.Join(home, filePath[2:])
		}
		item, err := client.UploadFileToFolder(teamID, folderID, filePath)
		return uploadDoneMsg{item: item, err: err}
	}
}

func reloadCurrentFolderCmd(client *graph.Client, teamID string, node FolderNode) tea.Cmd {
	cacheKey := node.ID
	if node.DriveID != "" {
		return func() tea.Msg {
			items, err := client.GetItemChildren(node.DriveID, node.ID)
			if err != nil {
				return errMsg{err}
			}
			return filesMsg{files: items, folderID: cacheKey}
		}
	}
	return func() tea.Msg {
		items, err := client.GetFolderChildren(teamID, node.ID)
		if err != nil {
			return errMsg{err}
		}
		return filesMsg{files: items, folderID: cacheKey}
	}
}

func is401(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "401") || strings.Contains(s, "Lifetime validation failed")
}

func detectExpiredToken(err error) string {
	s := err.Error()
	if strings.Contains(s, "Lifetime validation failed") || strings.Contains(s, "MS_GRAPH") {
		return "graph"
	}
	return "web"
}

func refreshTickCmd() tea.Cmd {
	return tea.Tick(pollInterval*time.Second, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func refreshPresenceTickCmd() tea.Cmd {
	return tea.Tick(presenceInterval*time.Second, func(t time.Time) tea.Msg {
		return presenceTickMsg{}
	})
}

func initialPresenceTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return presenceTickMsg{}
	})
}

func loadMeCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		id, err := client.GetMe()
		if err != nil {
			return meErrMsg{err}
		}
		return meMsg{id}
	}
}

func loadChatsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		chats, err := client.GetChats()
		if err != nil {
			return chatsErrMsg{err}
		}
		return chatsMsg{chats}
	}
}

func pollChatsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		chats, err := client.GetChats()
		if err != nil {
			return nil
		}
		return pollChatsMsg{chats}
	}
}

func setPresenceCmd(client *graph.Client, userID, availability string) tea.Cmd {
	return func() tea.Msg {
		var err error
		if availability == "Reset (Automatic)" {
			err = client.ClearPresence(userID)
		} else {
			err = client.SetPresence(userID, availability, availability)
		}
		return setPresenceMsg{err}
	}
}

func pollPresenceCmd(client *graph.Client, userIDs []string) tea.Cmd {
	return func() tea.Msg {
		if len(userIDs) == 0 {
			return nil
		}
		presences, err := client.GetPresences(userIDs)
		if err != nil {
			return nil
		}
		return presenceTickResultMsg{presences}
	}
}

func loadNotificationsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		items, err := client.FetchNotifications()
		if err != nil {
			return notificationsErrMsg{err}
		}
		return notificationsMsg{items}
	}
}

func loadAssignmentsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		items, err := client.FetchAssignments()
		if err != nil {
			return assignmentsErrMsg{err}
		}
		return assignmentsMsg{items}
	}
}

func loadAssignmentDetailCmd(client *graph.Client, classID, assignmentID string) tea.Cmd {
	return func() tea.Msg {
		refFiles, myFiles, resourcesFolderUrl, err := client.FetchAssignmentFiles(classID, assignmentID)
		return assignmentDetailMsg{
			assignmentID:       assignmentID,
			refFiles:           refFiles,
			myFiles:            myFiles,
			resourcesFolderUrl: resourcesFolderUrl,
			err:                err,
		}
	}
}

func uploadAssignmentFileCmd(client *graph.Client, a graph.Assignment, filePath string) tea.Cmd {
	return func() tea.Msg {
		item, err := client.UploadFileToSubmissionFolder(a.ResourcesFolderUrl, filePath)
		if err != nil {
			return assignmentUploadDoneMsg{assignmentID: a.ID, err: err}
		}
		fileURL := fmt.Sprintf("https://graph.microsoft.com/v1.0/drives/%s/items/%s",
			item.ParentReference.DriveID, item.ID)
		err = client.RegisterAssignmentResource(a.ClassID, a.ID, a.SubmissionID, fileURL, filepath.Base(filePath))
		return assignmentUploadDoneMsg{assignmentID: a.ID, fileName: filepath.Base(filePath), err: err}
	}
}

func submitAssignmentCmd(client *graph.Client, a graph.Assignment) tea.Cmd {
	return func() tea.Msg {
		err := client.SubmitAssignment(a.ClassID, a.ID, a.SubmissionID)
		return assignmentSubmitDoneMsg{assignmentID: a.ID, err: err}
	}
}

func undoSubmitAssignmentCmd(client *graph.Client, a graph.Assignment) tea.Cmd {
	return func() tea.Msg {
		err := client.UndoSubmitAssignment(a.ClassID, a.ID, a.SubmissionID)
		return assignmentUndoSubmitDoneMsg{assignmentID: a.ID, err: err}
	}
}

func removeAssignmentResourceCmd(client *graph.Client, a graph.Assignment, resourceID string, fileName string) tea.Cmd {
	return func() tea.Msg {
		err := client.RemoveAssignmentResource(a.ClassID, a.ID, a.SubmissionID, resourceID)
		return assignmentRemoveResourceDoneMsg{assignmentID: a.ID, resourceID: resourceID, fileName: fileName, err: err}
	}
}

func navigateToThreadCmd(client *graph.Client, teams []graph.Team, threadID string) tea.Cmd {
	return func() tea.Msg {
		for _, team := range teams {
			channels, err := client.GetChannels(team.ID)
			if err != nil {
				continue
			}
			for _, ch := range channels {
				if ch.ID == threadID {
					return navigateToThreadMsg{
						threadID: threadID,
						teamID:   team.ID,
						channels: channels,
					}
				}
			}
		}
		return nil
	}
}

func openAssignmentFileCmd(client *graph.Client, file graph.AssignmentFile) tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequest("GET", file.FileUrl, nil)
		req.Header.Set("Authorization", "Bearer "+client.GraphToken)
		resp, err := client.HTTPClient.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		var item struct {
			WebUrl string `json:"webUrl"`
		}
		json.NewDecoder(resp.Body).Decode(&item)
		if item.WebUrl != "" {
			openBrowser(buildSharePointViewerURL(item.WebUrl))
			return downloadDoneMsg{results: []string{"⟳ " + file.Name + ": opened in browser"}}
		}
		return downloadDoneMsg{results: []string{"✗ " + file.Name + ": could not resolve URL"}}
	}
}

func getUniquePath(baseDir, name string) string {
	destPath := filepath.Join(baseDir, name)
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		return destPath
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)

	for i := 1; ; i++ {
		newName := fmt.Sprintf("%s (%d)%s", base, i, ext)
		newPath := filepath.Join(baseDir, newName)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}
}

func downloadFilesCmd(client *graph.Client, teamID, driveID string, items []graph.DriveItem, destDir string) tea.Cmd {
	return func() tea.Msg {
		if destDir == "" {
			home, _ := os.UserHomeDir()
			destDir = filepath.Join(home, "Downloads")
		}
		os.MkdirAll(destDir, 0755)

		var results []string
		for _, item := range items {
			var body io.ReadCloser
			var err error

			if strings.HasPrefix(item.WebUrl, "https://graph.microsoft.com") {
				body, err = client.DownloadGraphItem(item.WebUrl)
			} else if item.ID != "" {
				if driveID != "" {
					body, err = client.DownloadRemoteItem(driveID, item.ID)
				} else {
					body, err = client.DownloadItem(teamID, item.ID)
				}
			} else if item.WebUrl != "" {
				resolved, resolveErr := client.ResolveSharedItem(item.WebUrl)
				if resolveErr == nil && resolved != nil && resolved.ID != "" {
					resolvedDriveID := ""
					if resolved.RemoteItem != nil {
						resolvedDriveID = resolved.RemoteItem.ParentReference.DriveID
					}
					if resolvedDriveID != "" {
						body, err = client.DownloadRemoteItem(resolvedDriveID, resolved.ID)
					} else {
						body, err = client.DownloadItem(teamID, resolved.ID)
					}
				} else {
					if isSharePointURL(item.WebUrl) {
						results = append(results, fmt.Sprintf("✗ %s: not found on SharePoint", item.Name))
					} else {
						link := item.DownloadUrl
						if link == "" {
							link = item.WebUrl
						}
						openBrowser(link)
						results = append(results, fmt.Sprintf("⟳ %s: opened in browser", item.Name))
					}
					continue
				}
			} else {
				err = fmt.Errorf("no ID or download URL")
			}
			if err != nil {
				if item.WebUrl != "" {
					link := item.DownloadUrl
					if link == "" {
						link = item.WebUrl
					}
					openBrowser(link)
					results = append(results, fmt.Sprintf("⟳ %s: opened in browser", item.Name))
				} else {
					results = append(results, fmt.Sprintf("✗ %s: %v", item.Name, err))
				}
				continue
			}

			destPath := getUniquePath(destDir, item.Name)
			out, ferr := os.Create(destPath)
			if ferr != nil {
				body.Close()
				results = append(results, fmt.Sprintf("✗ %s: %v", item.Name, ferr))
				continue
			}
			_, cerr := io.Copy(out, body)
			body.Close()
			out.Close()
			if cerr != nil {
				results = append(results, fmt.Sprintf("✗ %s: %v", item.Name, cerr))
			} else {
				results = append(results, fmt.Sprintf("✓ %s → %s", item.Name, destPath))
			}
		}
		return downloadDoneMsg{results: results}
	}
}

func clearStatusAfter(id int) tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearDownloadStatusMsg{id: id}
	})
}

func discoverSelfChatCmd(client *graph.Client, selfID, cachedID string) tea.Cmd {
	return func() tea.Msg {
		if selfID == "" {
			return nil
		}

		if cachedID != "" {
			return selfChatDiscoveredMsg{id: cachedID, newlyDiscovered: false}
		}

		id := client.DiscoverSelfChatID(selfID)
		if id != "" {
			return selfChatDiscoveredMsg{id: id, newlyDiscovered: true}
		}

		return nil
	}
}

func loadTeamsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		teams, err := client.GetJoinedTeams()
		if err != nil {
			return errMsg{err}
		}
		return teamsMsg{teams}
	}
}

func loadChannelsCmd(client *graph.Client, teamID string) tea.Cmd {
	return func() tea.Msg {
		channels, err := client.GetChannels(teamID)
		if err != nil {
			return channelsErrMsg{teamID: teamID, err: err}
		}
		return channelsMsg{teamID: teamID, channels: channels}
	}
}

func loadMessagesCmd(client *graph.Client, teamID, channelID string, pageSize int) tea.Cmd {
	return func() tea.Msg {
		msgs, link, err := client.GetMessagesWithLink(teamID, channelID, pageSize)
		if err != nil {
			return messagesErrMsg{err: err, conversationID: channelID, partialMsgs: msgs}
		}

		return messagesMsg{messages: msgs, backwardLink: link}
	}
}

func loadMoreMessagesCmd(client *graph.Client, backwardLink string) tea.Cmd {
	return func() tea.Msg {
		page, err := client.GetMessagesFromLink(backwardLink)
		return loadMoreMessagesMsg{
			messages:     page.Messages,
			backwardLink: page.BackwardLink,
			err:          err,
		}
	}
}

func editMessageCmd(client *graph.Client, channelID, messageID, content string) tea.Cmd {
	return func() tea.Msg {
		err := client.EditMessage(channelID, messageID, content)
		return editMessageMsg{err}
	}
}

func deleteMessageCmd(client *graph.Client, channelID, messageID string) tea.Cmd {
	return func() tea.Msg {
		err := client.DeleteMessage(channelID, messageID)
		return deleteMessageMsg{err}
	}
}

func getReactionsCmd(client *graph.Client, channelID, messageID, selfID string) tea.Cmd {
	return func() tea.Msg {
		reactions, err := client.GetMessageReactions(channelID, messageID, selfID)
		return reactionsLoadedMsg{messageID: messageID, reactions: reactions, err: err}
	}
}

func addReactionCmd(client *graph.Client, channelID, messageID, key string) tea.Cmd {
	return func() tea.Msg {
		err := client.AddReaction(channelID, messageID, key)
		return addReactionMsg{err}
	}
}

func removeReactionCmd(client *graph.Client, channelID, messageID, key string) tea.Cmd {
	return func() tea.Msg {
		err := client.RemoveReaction(channelID, messageID, key)
		return removeReactionMsg{err}
	}
}

func sendReplyCmd(client *graph.Client, channelID, parentID, content string, mentions []graph.MentionedUser) tea.Cmd {
	return func() tea.Msg {
		err := client.SendReply(channelID, parentID, content, mentions)
		if err != nil {
			return threadReplySendErrMsg{err}
		}
		return threadReplySentMsg{}
	}
}

func sendMessageCmd(client *graph.Client, channelID, content string, mentions []graph.MentionedUser) tea.Cmd {
	return func() tea.Msg {
		err := client.SendMessage(channelID, content, mentions)
		if err != nil {
			return messageSendErrMsg{err}
		}
		return messageSentMsg{}
	}
}

func loadChannelRootCmd(client *graph.Client, teamID, channelName string) tea.Cmd {
	return func() tea.Msg {
		folder, err := client.GetChannelFolder(teamID, channelName)
		if err != nil {
			return channelRootMsg{err: err}
		}
		return channelRootMsg{
			node: FolderNode{
				ID:   folder.ID,
				Name: folder.Name,
			},
		}
	}
}

func loadFilesCmd(client *graph.Client, teamID, channelName, channelID string) tea.Cmd {
	return func() tea.Msg {
		files, err := client.GetChannelFiles(teamID, channelName)
		if err != nil {
			return filesErrMsg{err}
		}
		return filesMsg{files: files, folderID: "root:" + channelID}
	}
}

func loadFolderCmd(client *graph.Client, teamID string, node FolderNode) tea.Cmd {
	return func() tea.Msg {
		var (
			files []graph.DriveItem
			err   error
		)
		if node.DriveID != "" {
			files, err = client.GetItemChildren(node.DriveID, node.ID)
		} else {
			files, err = client.GetFolderChildren(teamID, node.ID)
		}
		if err != nil {
			return filesErrMsg{err}
		}
		return filesMsg{files: files, folderID: node.ID}
	}
}

type markAsReadMsg struct{ err error }

func markAsReadCmd(client *graph.Client, conversationID string, lastMsg graph.Message) tea.Cmd {
	return func() tea.Msg {
		err := client.MarkConversationAsRead(conversationID, lastMsg)
		return markAsReadMsg{err}
	}
}

type unreadStatusMsg struct {
	chatID    string
	hasUnread bool
}

func checkUnreadCmd(client *graph.Client, chat graph.Chat) tea.Cmd {
	return func() tea.Msg {
		result, err := client.GetConsumptionHorizon(chat.ID)
		if err != nil || result.ChatVersion == 0 {
			return nil
		}
		return unreadStatusMsg{
			chatID:    chat.ID,
			hasUnread: result.ChatVersion > result.LastReadTs,
		}
	}
}
