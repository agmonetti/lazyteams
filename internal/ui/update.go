package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"teamsTUI/internal/graph"
	"teamsTUI/internal/teams"
	"teamsTUI/internal/ui/components/directorypicker"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Commands

const pollInterval = 15       // seconds between DM polls
const presenceInterval = 60   // seconds between presence polls

type tickMsg struct{}
type presenceTickMsg struct{}
type messageSentMsg struct{}
type messageSendErrMsg struct{ err error }
type filesMsg struct {
	files    []graph.DriveItem
	folderID string // for caching: folderID or "root:<chanID>"
}
type filesErrMsg struct{ err error }
type chatsMsg struct{ chats []graph.Chat }
type chatsErrMsg struct{ err error }
type meMsg struct{ id string }
type meErrMsg struct{ err error }
type selfChatDiscoveredMsg struct {
	id              string
	newlyDiscovered bool
}

type navigateToThreadMsg struct {
	threadID string
	teamID   string
	channels []graph.Channel
}
type markNotifReadMsg struct{ err error }

type tokenExpiredMsg struct{ token string }
type tokenRenewedMsg struct{ err error }
type tokenRenewingMsg struct{}

type searchUsersMsg struct{ results []graph.UserSearchResult }
type searchUsersErrMsg struct{ err error }
type createDMMsg struct{ chat graph.Chat }
type createDMErrMsg struct{ err error }
type uploadDoneMsg struct {
	item graph.DriveItem
	err  error
}

type dirPickerResultMsg struct {
	path string
}

type createTeamMsg struct{ err error }
type reloadTeamsAfterCreateMsg struct{}

type createChannelMsg struct{ err error }

type deleteChannelMsg struct{ err error }
type deleteTeamMsg struct{ err error }

type teamInfoMsg struct{ team *graph.Team }
type teamInfoErrMsg struct{ err error }
type channelInfoMsg struct{ channel *graph.Channel }
type channelInfoErrMsg struct{ err error }

type channelMembersMsg struct{ members []graph.TeamMember }
type channelMembersErrMsg struct{ err error }

type teamMembersMsg struct{ members []graph.TeamMember }
type teamMembersErrMsg struct{ err error }
type addMemberMsg struct{ err error }
type removeMemberMsg struct{ err error }
type addChannelMemberMsg struct{ err error; member graph.TeamMember }
type removeChannelMemberMsg struct{ err error; userID string }

type delayedReloadChannelsMsg struct{}
type delayedReloadTeamsMsg struct{}

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

type pollChatsMsg struct {
	chats []graph.Chat
}

func pollChatsCmd(client *graph.Client) tea.Cmd {
	return func() tea.Msg {
		chats, err := client.GetChats()
		if err != nil {
			return nil // silent failure on poll, don't break the UI
		}
		return pollChatsMsg{chats}
	}
}

type presenceTickResultMsg struct {
	presences map[string]string
}

type setPresenceMsg struct {
	err error
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
			return nil // silent failure
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

type downloadDoneMsg struct {
	results []string
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

			if item.ID != "" {
				// Item with real Graph ID (channels, Class Materials)
				if driveID != "" {
					body, err = client.DownloadRemoteItem(driveID, item.ID)
				} else {
					body, err = client.DownloadItem(teamID, item.ID)
				}
			} else if item.WebUrl != "" {
				// Synthetic DM item: try to resolve via /shares
				resolved, resolveErr := client.ResolveSharedItem(item.WebUrl)
				if resolveErr == nil && resolved != nil && resolved.ID != "" {
					// Resolved: download with the real ID and driveId
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
					// Could not resolve via /shares
					if isSharePointURL(item.WebUrl) {
						// It's a SharePoint/OneDrive link but couldn't be resolved
						// = the file doesn't exist or was deleted
						results = append(results, fmt.Sprintf("✗ %s: not found on SharePoint", item.Name))
					} else {
						// External link (github, etc.) → open in browser
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
				// If native download fails and there's a WebUrl, open in browser
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

			destPath := filepath.Join(destDir, item.Name)
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

type clearDownloadStatusMsg struct{ id int }

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

		// 1. If we have a cache, bypass the network entirely
		if cachedID != "" {
			return selfChatDiscoveredMsg{id: cachedID, newlyDiscovered: false}
		}

		// 2. No cache, we need to brute-force the API
		id := client.DiscoverSelfChatID(selfID)
		if id != "" {
			return selfChatDiscoveredMsg{id: id, newlyDiscovered: true}
		}

		return nil // If all formats fail, fail silently
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

type threadReplySentMsg struct{}
type threadReplySendErrMsg struct{ err error }

type addReactionMsg struct{ err error }
type removeReactionMsg struct{ err error }

type editMessageMsg struct{ err error }
type deleteMessageMsg struct{ err error }

type loadMoreMessagesMsg struct {
	messages     []graph.Message
	backwardLink string
	err          error
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

type reactionsLoadedMsg struct {
	messageID string
	reactions map[string]bool
	err       error
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

func getOptimalWrapWidth(raw string, maxW int) int {
	lines := strings.Split(raw, "\n")
	max := 0
	for _, l := range lines {
		w := lipgloss.Width(l)
		if w > max {
			max = w
		}
	}
	if max >= maxW {
		return maxW
	}
	return 0
}

func cleanHTMLForEdit(content string) string {
	// Strip tags but preserve text
	content = strings.ReplaceAll(content, "<p>", "")
	content = strings.ReplaceAll(content, "</p>", "")
	content = strings.ReplaceAll(content, "<br>", "\n")
	content = strings.ReplaceAll(content, "&nbsp;", " ")
	content = strings.ReplaceAll(content, "&amp;", "&")
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")
	
	// Also clean up any mention markers we added in cleanHTML, in case they leaked into RawBody
	// (Though RawBody is usually raw HTML so it has <readonly><span> instead)
	content = graph.MentionSpan.ReplaceAllStringFunc(content, func(match string) string {
		sub := graph.MentionSpan.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return "@" + sub[1]
	})
	
	return strings.TrimSpace(content)
}

var mentionToken = regexp.MustCompile(`\x1E(.*?)\x1F`)
var ansiToken = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func highlightMentions(text string) string {
	mentionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	return mentionToken.ReplaceAllStringFunc(text, func(m string) string {
		sub := mentionToken.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		// Glamour may have injected ANSI codes (like color resets) inside the match,
		// which would break our outer mentionStyle wrapper. We strip them first.
		clean := ansiToken.ReplaceAllString(sub[1], "")
		return mentionStyle.Render(clean)
	})
}

func renderMarkdown(content string, width int) string {
	if content == "" {
		return ""
	}
	// Fallback in case of rendering error
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	// Highlight @mentions after Glamour renders (ANSI-safe)
	out = highlightMentions(out)
	// Glamour adds extra newlines at the end, let's trim them
	return strings.TrimSpace(out)
}

func rootMessages(msgs []graph.Message) []graph.Message {
	var result []graph.Message
	for _, m := range msgs {
		if m.RootMessageID == m.ID || m.RootMessageID == "" {
			if m.MessageType == "Text" || m.MessageType == "RichText/Html" {
				result = append(result, m)
			}
		}
	}
	return result
}

func validDMMessages(msgs []graph.Message) []graph.Message {
	var result []graph.Message
	for _, m := range msgs {
		if m.MessageType != "ThreadActivity/MemberJoined" &&
			m.MessageType != "ThreadActivity/MemberLeft" &&
			m.MessageType != "ThreadActivity/TopicUpdate" &&
			m.MessageType != "ThreadActivity/AddMember" {
			result = append(result, m)
		}
	}
	return result
}

func repliesFor(msgs []graph.Message, parentID string) []graph.Message {
	var result []graph.Message
	for _, m := range msgs {
		if m.RootMessageID == parentID && m.ID != parentID {
			result = append(result, m)
		}
	}
	// Sort by time ascending (oldest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func replyCount(msgs []graph.Message, parentID string) int {
	return len(repliesFor(msgs, parentID))
}

func formatMessagesWithCursor(messages []graph.Message, width, cursor int, cursorMode bool) string {
	var content string
	roots := rootMessages(messages)
	
	actualW := width - 4
	if actualW < 10 {
		actualW = 10
	}

	for i := len(roots) - 1; i >= 0; i-- {
		msg := roots[i]
		cursorStr := "  "
		if cursorMode && i == cursor {
			cursorStr = "▶ "
		}
		timeStr := msg.CreatedAt.Local().Format("02/01 15:04")
		formattedTime := metaStyle.Render(fmt.Sprintf("[%s]", timeStr))
		sender := selectedItemStyle.Render(msg.FromName)
		
		var attachmentsStr string
		for _, att := range msg.Attachments {
			icon := "[Link]"
			if att.Type == "file" {
				icon = "[File]"
			}
			linkStr := makeClickableLink(att.Name, att.URL)
			attachmentsStr += fmt.Sprintf("  %s %s\n", systemEventStyle.Render(icon), linkStr)
		}

		var body string
		if msg.Deleted {
			body = systemEventStyle.Render("This message has been deleted.")
		} else {
			body = renderMarkdown(msg.Body, actualW)
			if body != "" && attachmentsStr != "" {
				body += "\n\n"
			}
			body += attachmentsStr
		}

		content += fmt.Sprintf("%s%s %s:\n%s\n", cursorStr, formattedTime, sender, body)

		// Reply count indicator
		count := replyCount(messages, msg.ID)
		if count > 0 {
			replyStr := fmt.Sprintf("  ↳ %d repl", count)
			if count == 1 {
				replyStr += "y"
			} else {
				replyStr += "ies"
			}
			if cursorMode {
				replyStr += " [Enter to open]"
			}
			content += metaStyle.Render(replyStr) + "\n"
		}

		// Reactions
		if len(msg.Reactions) > 0 {
			var reactionStr string
			for _, r := range msg.Reactions {
				emoji := reactionEmoji(r.Key)
				if r.Count > 1 {
					reactionStr += fmt.Sprintf("%s %d  ", emoji, r.Count)
				} else {
					reactionStr += fmt.Sprintf("%s  ", emoji)
				}
			}
			content += "  " + metaStyle.Render(strings.TrimSpace(reactionStr)) + "\n"
		}

		content += "\n"
	}
	return content
}

// formatMessages converts the message list into a renderable string for the viewport
func formatMessages(messages []graph.Message, width int) string {
	var content string
	var lastDate string
	actualW := width - 2
	if actualW < 10 {
		actualW = 10
	}
	todayStr := time.Now().Format("02/01/2006")

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]

		// Subtle separator between different days
		msgDate := msg.CreatedAt.Local().Format("02/01/2006")
		if lastDate != "" && msgDate != lastDate {
			displayDate := msgDate
			if msgDate == todayStr {
				displayDate = "Hoy"
			}
			dateText := fmt.Sprintf(" %s ", displayDate)
			padLeft := (actualW - len(dateText)) / 2
			if padLeft < 0 {
				padLeft = 0
			}
			padRight := actualW - len(dateText) - padLeft
			if padRight < 0 {
				padRight = 0
			}
			sep := strings.Repeat("─", padLeft) + dateText + strings.Repeat("─", padRight)
			content += metaStyle.Render(sep) + "\n\n"
		}
		lastDate = msgDate

		timeStr := msg.CreatedAt.Local().Format("02/01 15:04")
		formattedTime := metaStyle.Render(fmt.Sprintf("[%s]", timeStr))

		sender := msg.FromName
		if sender == "" {
			sender = "User"
		}

		switch msg.MessageType {
		case "Text", "RichText/Html":
			var attachmentsStr string
			for _, att := range msg.Attachments {
				icon := "[Link]"
				if att.Type == "file" {
					icon = "[File]"
				}
				linkStr := makeClickableLink(att.Name, att.URL)
				attachmentsStr += fmt.Sprintf("  %s %s\n", systemEventStyle.Render(icon), linkStr)
			}

			if msg.Body != "" || attachmentsStr != "" {
				body := renderMarkdown(msg.Body, actualW)
				if body != "" && attachmentsStr != "" {
					body += "\n\n"
				}
				body += attachmentsStr

				content += fmt.Sprintf("%s %s:\n%s\n\n",
					formattedTime,
					selectedItemStyle.Render(sender),
					body)
			}

		case "Event/Call":
			content += fmt.Sprintf("%s %s\n\n",
				formattedTime,
				systemEventStyle.Render("[Meeting / System Call]"))

		case "ThreadActivity/AddMember", "ThreadActivity/MemberAdded", "ThreadActivity/DeleteMember", "ThreadActivity/MemberRemoved":
			continue

		default:
			continue
		}
	}
	content = lipgloss.NewStyle().Width(actualW).Render(content)
	return content
}

func formatMessagesDM(messages []graph.Message, width int, selfName, selfID string, cursor int, cursorMode bool) string {
	var content string
	var lastDate string
	actualW := width - 4
	if actualW < 10 {
		actualW = 10
	}
	todayStr := time.Now().Format("02/01/2006")

	validMsgs := validDMMessages(messages)

	for i := len(validMsgs) - 1; i >= 0; i-- {
		msg := validMsgs[i]
		
		msgDate := msg.CreatedAt.Local().Format("02/01/2006")
		if lastDate != "" && msgDate != lastDate {
			displayDate := msgDate
			if msgDate == todayStr {
				displayDate = "Hoy"
			}
			// Center the date separator based on the terminal width
			dateText := fmt.Sprintf(" %s ", displayDate)
			padLeft := (actualW - len(dateText)) / 2
			if padLeft < 0 {
				padLeft = 0
			}
			padRight := actualW - len(dateText) - padLeft
			if padRight < 0 {
				padRight = 0
			}
			sep := strings.Repeat("─", padLeft) + dateText + strings.Repeat("─", padRight)
			content += metaStyle.Render(sep) + "\n\n"
		}
		lastDate = msgDate
		
		cursorStr := "  "
		if cursorMode && i == cursor {
			cursorStr = "▶ "
		}

		timeStr := msg.CreatedAt.Local().Format("02/01 15:04")
		body := strings.TrimSpace(msg.Body)
		isSelf := msg.FromName == selfName || msg.FromName == "User" || 
			(selfID != "" && strings.HasSuffix(msg.FromUserID, selfID))
		
		if isSelf {
			tsRaw := metaStyle.Render(timeStr)
			timestamp := lipgloss.PlaceHorizontal(actualW, lipgloss.Right, tsRaw)

			var wrapped string
			if msg.Deleted {
				wrapped = systemEventStyle.Render("This message has been deleted.")
			} else {
				rawBody := strings.TrimSpace(body)
				maxW := actualW * 2 / 3
				
				optW := getOptimalWrapWidth(rawBody, maxW)
				wrapped = renderMarkdown(rawBody, optW)
			}
			
			// Find actual max width of the rendered output to create a tight bounding box
			lines := strings.Split(wrapped, "\n")
			actualMaxW := 0
			for _, l := range lines {
				lw := lipgloss.Width(l)
				if lw > actualMaxW {
					actualMaxW = lw
				}
			}
			
			styledBlock := lipgloss.NewStyle().Width(actualMaxW).Render(wrapped)
			placedBlock := lipgloss.PlaceHorizontal(actualW, lipgloss.Right, styledBlock)

			content += fmt.Sprintf("%s%s\n  %s\n", cursorStr, timestamp, placedBlock)
			if len(msg.Reactions) > 0 {
				var reactionStr string
				for _, rx := range msg.Reactions {
					emoji := reactionEmoji(rx.Key)
					if rx.Count > 1 {
						reactionStr += fmt.Sprintf("%s %d  ", emoji, rx.Count)
					} else {
						reactionStr += fmt.Sprintf("%s  ", emoji)
					}
				}
				reactionsBlock := lipgloss.PlaceHorizontal(actualW, lipgloss.Right, metaStyle.Render(strings.TrimSpace(reactionStr)))
				content += fmt.Sprintf("  %s\n", reactionsBlock)
			}
			content += "\n"
		} else {
			sender := selectedItemStyle.Render(msg.FromName)
			tsRaw := metaStyle.Render(timeStr)
			header := fmt.Sprintf("%s %s:", tsRaw, sender)
			
			var wrapped string
			if msg.Deleted {
				wrapped = systemEventStyle.Render("This message has been deleted.")
			} else {
				rawBody := strings.TrimSpace(body)
				wrapped = renderMarkdown(rawBody, actualW)
			}
			
			content += fmt.Sprintf("%s%s\n  %s\n", cursorStr, header, strings.ReplaceAll(wrapped, "\n", "\n  "))
			
			if len(msg.Reactions) > 0 {
				var reactionStr string
				for _, rx := range msg.Reactions {
					emoji := reactionEmoji(rx.Key)
					if rx.Count > 1 {
						reactionStr += fmt.Sprintf("%s %d  ", emoji, rx.Count)
					} else {
						reactionStr += fmt.Sprintf("%s  ", emoji)
					}
				}
				content += fmt.Sprintf("  %s\n", metaStyle.Render(strings.TrimSpace(reactionStr)))
			}
			content += "\n"
		}
	}
	return content
}

// Update
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	// --- INSERT MODE (TEXT INPUT) ---
		if m.isTyping {
			switch msg := msg.(type) {
			case tea.KeyMsg:
				// Si el popup está abierto, las flechas y enter van al popup
				if m.showMentionPopup {
					switch msg.String() {
					case "up":
						if m.mentionCursor > 0 {
							m.mentionCursor--
						}
						return m, nil
					case "down":
						if m.mentionCursor < len(m.mentionSuggestions)-1 {
							m.mentionCursor++
						}
						return m, nil
					case "enter", "tab":
						selected := m.mentionSuggestions[m.mentionCursor]
						// Reemplazar desde el @ hasta el final con el nombre completo
						v := m.input.Value()
						newVal := v[:m.mentionAtPos] + "@" + selected.DisplayName + " "
						m.input.SetValue(newVal)
						// Mover cursor al final
						m.input.CursorEnd()
						m.showMentionPopup = false
						m.mentionSuggestions = nil
						m.mentionCursor = 0
						if m.ready {
							rightInnerHeight := m.height - 6 - 2
							inputHeight := strings.Count(m.input.View(), "\n") + 1
							newVpHeight := rightInnerHeight - 4 - inputHeight
							if newVpHeight < 5 {
								newVpHeight = 5
							}
							m.viewport.Height = newVpHeight
						}
						return m, nil
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
						return m, nil
					}
				}
				switch msg.String() {
				case "esc": // Exit insert mode
				m.isTyping = false
				m.input.Blur()
				m.input.Reset()
				// Restore viewport height
				if m.ready {
					rightInnerHeight := m.height - 6 - 2
					m.viewport.Height = rightInnerHeight - 4 - 1
				}
				return m, nil
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
					return m, sendMessageCmd(m.client, m.activeConversationID(), v, mentions)
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
		
		return m, tea.Batch(cmds...)
	}
	// --------------------------------

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Width: real overhead = 2 per panel (border) × 2 panels = 4
		// + 1 col margin for Kitty = 5 total
		available        := m.width - 5
		leftOuterWidth   := available / 3
		rightOuterWidth  := available - leftOuterWidth
		leftInnerWidth   := leftOuterWidth - 2
		rightInnerWidth  := rightOuterWidth - 2
		panelOuterHeight := m.height - 6
		leftInnerHeight  := panelOuterHeight - 2
		rightInnerHeight := panelOuterHeight - 2
		
		m.input.SetWidth(rightInnerWidth - 2)
		// Max height for textarea to prevent it from covering the whole screen
		m.input.MaxHeight = rightInnerHeight / 3
		
		// We reserve fixed lines for the input placeholder
		vpInnerHeight := rightInnerHeight - 4 - 2 // reserve 2 lines
		if vpInnerHeight < 5 {
			vpInnerHeight = 5
		}
		
		if !m.ready {
			m.viewport = viewport.New(rightInnerWidth, vpInnerHeight)
			m.leftVp = viewport.New(leftInnerWidth, leftInnerHeight)
			m.threadViewport = viewport.New(rightInnerWidth, vpInnerHeight-6)
			m.ready = true
		} else {
			m.viewport.Width = rightInnerWidth
			m.viewport.Height = vpInnerHeight
			m.leftVp.Width = leftInnerWidth
			m.leftVp.Height = leftInnerHeight
			m.threadViewport.Width = rightInnerWidth
			m.threadViewport.Height = vpInnerHeight - 6
		}

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
			if which == "graph" {
				m.err = fmt.Errorf("MS_GRAPH_TOKEN expired — run ./msTTui-auth manually")
			} else {
				m.tokenRenewing = true
				return m, launchAuthHelperCmd("web")
			}
			return m, nil
		}
		m.err = msg.err
		m.loading = false
		return m, nil

	case tokenRenewingMsg:
		m.tokenRenewing = true
		return m, launchAuthHelperCmd("web")

	case tokenRenewedMsg:
		m.tokenRenewing = false
		if msg.err != nil {
			m.tokenRenewErr = msg.err.Error()
		} else {
			m.tokenRenewErr = ""
			return m, reloadTokensCmd(m.client)
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

		if strings.Contains(msg.err.Error(), "401") {
			if !m.tokenRenewing {
				m.tokenRenewing = true
				return m, launchAuthHelperCmd("web")
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
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}
		// Reload to see the updated reaction
		m.messagesBackwardLink = ""
			m.loadingMore = false
			return m, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.activeConversationID(), 200)

	case removeReactionMsg:
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}
		m.messagesBackwardLink = ""
			m.loadingMore = false
			return m, loadMessagesCmd(m.client, m.teams[m.selectedTeam].ID, m.activeConversationID(), 200)

	case reactionsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ %v", msg.err)
			m.downloadStatusID++
			return m, clearStatusAfter(m.downloadStatusID)
		}
		// Apply UserReacted to the specific message
		for i, message := range m.messages {
			if message.ID == msg.messageID {
				for j, r := range message.Reactions {
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

	case filesMsg:
		m.files = msg.files
		m.loading = false
		m.selectedFile = 0
		m.selectedFiles = make(map[int]bool)

		// Cache the result
		if msg.folderID != "" {
			m.folderCache[msg.folderID] = msg.files
		}

		m.viewport.SetContent(renderFilesContent(&m))
		m.viewport.GotoTop()
		return m, nil

	case teamsMsg:
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

	case channelsMsg:
		if len(m.teams) == 0 || m.selectedTeam >= len(m.teams) || msg.teamID != m.teams[m.selectedTeam].ID {
			return m, nil // stale response, discard
		}
		m.channels = msg.channels
		m.selectedChan = 0
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

	case meMsg:
		m.selfID = msg.id
		return m, nil

	case meErrMsg:
		// Don't block the app: without selfID, 1:1 chats will simply
		// show all participants instead of excluding me.
		return m, nil

	case chatsErrMsg:
		if is401(msg.err) {
			which := detectExpiredToken(msg.err)
			if which == "graph" {
				m.err = fmt.Errorf("MS_GRAPH_TOKEN expired — run ./msTTui-auth manually")
			} else if !m.tokenRenewing {
				m.tokenRenewing = true
				return m, launchAuthHelperCmd("web")
			}
		} else {
			m.err = msg.err
		}
		m.loading = false
		return m, nil

	case chatsMsg:
		m.chats = msg.chats
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

		// Chain async self-discovery, passing the cache if available
		if m.selfID != "" {
			cachedID := m.prefs.SelfChatIDs[m.selfID]
			return m, discoverSelfChatCmd(m.client, m.selfID, cachedID)
		}
		return m, tea.Batch(cmds...)

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
		m.messages = msg.messages
		m.messagesBackwardLink = msg.backwardLink
		m.loadingMore = false
		m.loading = false

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
			m.viewport.SetContent(content)
			m.viewport.GotoBottom()
			
			// Also update thread view if active
				if m.showThread {
					replies := repliesFor(m.messages, m.threadParentID)
					threadContent := formatThread(m.threadParentMsg, replies, m.threadViewport.Width, m.userName, m.threadCursor, true)
					m.threadViewport.SetContent(threadContent)
				}
		} else if m.viewMode == ModeFiles {
			m.files = teams.AggregateChatAttachments(m.messages)
			m.selectedFile = 0
			m.viewport.SetContent(renderFilesContent(&m))
			m.viewport.GotoTop()
		}
		return m, nil

	case tickMsg:
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

	case presenceTickMsg:
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

	case presenceTickResultMsg:
		for k, v := range msg.presences {
			m.presence[k] = v
		}
		return m, nil

	case notificationsMsg:
		m.notifications = msg.items
		m.notifLoaded = true
		m.notifErr = nil
		return m, nil

	case notificationsErrMsg:
		if is401(msg.err) {
			if !m.tokenRenewing {
				m.tokenRenewing = true
				return m, launchAuthHelperCmd("notif")
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
		m.assignErr = msg.err
		m.assignLoaded = true
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

	case downloadDoneMsg:
		m.downloading = false
		m.downloadStatus = strings.Join(msg.results, " | ")
		m.downloadStatusID++
		if m.viewMode == ModeFiles {
			m.viewport.SetContent(renderFilesContent(&m))
			m.viewport.GotoBottom()
		}
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
		// Update the chat list with fresh data
		// (no badge: lastModifiedDateTime is not available on this tenant)
		return m, nil

	case markNotifReadMsg:
	    // silent — local state already updated optimistically
	    return m, nil

	case searchUsersMsg:
		m.newDMResults = msg.results
		m.newDMCursor = 0
		m.newDMErr = ""
		return m, nil

	case searchUsersErrMsg:
		m.newDMErr = msg.err.Error()
		return m, nil

	case createDMMsg:
		for i, ch := range m.chats {
			if ch.ID == msg.chat.ID {
				m.selectedChat = i
				m.loadedConvID = ch.ID
				m.focusLeft = false
				m.viewMode = ModeChat
				m.loading = true
				m.messagesBackwardLink = ""
			m.loadingMore = false
			return m, loadMessagesCmd(m.client, "", ch.ID, 200)
			}
		}
		m.chats = append([]graph.Chat{msg.chat}, m.chats...)
		m.selectedChat = 0
		m.loadedConvID = msg.chat.ID
		m.focusLeft = false
		m.viewMode = ModeChat
		m.loading = true
		m.messagesBackwardLink = ""
			m.loadingMore = false
			return m, loadMessagesCmd(m.client, "", msg.chat.ID, 200)

	case createDMErrMsg:
		m.newDMErr = msg.err.Error()
		return m, nil

	case uploadDoneMsg:
		m.uploading = false
		if msg.err != nil {
			m.downloadStatus = fmt.Sprintf("✗ Upload failed: %v", msg.err)
		} else {
			m.downloadStatus = fmt.Sprintf("✓ Uploaded: %s", msg.item.Name)
			if m.workspace == WorkspaceDMs && msg.item.WebUrl != "" {
				m.downloadStatusID++
				return m, tea.Batch(
					sendMessageCmd(m.client, m.activeConversationID(), fmt.Sprintf("📎 [%s](%s)", msg.item.Name, msg.item.WebUrl), nil),
					clearStatusAfter(m.downloadStatusID),
				)
			}
			if m.workspace == WorkspaceTeams && m.viewMode == ModeFiles {
				m.loading = true
				m.downloadStatusID++
				return m, tea.Batch(
					loadFilesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].DisplayName, m.channels[m.selectedChan].ID),
					clearStatusAfter(m.downloadStatusID),
				)
			}
		}
		m.downloadStatusID++
		return m, clearStatusAfter(m.downloadStatusID)

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
		// silencioso
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
		} else if m.pickerPurpose == "upload" {
			info, err := os.Stat(msg.Path)
			if err != nil || info.IsDir() {
				m.downloadStatus = "✗ Please select a file, not a folder"
				m.downloadStatusID++
				return m, clearStatusAfter(m.downloadStatusID)
			}
			m.uploading = true
			isDM := m.workspace == WorkspaceDMs
			channelName := ""
			teamID := ""
			if !isDM && len(m.channels) > 0 {
				channelName = m.channels[m.selectedChan].DisplayName
				teamID = m.teams[m.selectedTeam].ID
			}
			return m, uploadFileCmd(m.client, teamID, channelName, msg.Path, isDM)
		}
		return m, nil

	case directorypicker.CancelledMsg:
		m.showDirPicker = false
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

	case tea.KeyMsg:
		// Help menu — intercepts keys
		if m.showHelp {
			switch msg.String() {
			case "esc", "q", "?", "enter", "space":
				m.showHelp = false
		}
		return m, nil
	}

	if m.showDeleteMsgPopup {
		switch msg.String() {
		case "y", "enter":
			return m, deleteMessageCmd(m.client, m.activeConversationID(), m.deleteMsgID)
		case "n", "esc":
			m.showDeleteMsgPopup = false
		}
		return m, nil
	}

	if m.showEditPopup {
		switch msg.String() {
		case "esc":
			m.showEditPopup = false
			m.editInput.Reset()
		case "enter":
			content := strings.TrimSpace(m.editInput.Value())
			if content != "" {
				return m, editMessageCmd(m.client, m.activeConversationID(), m.editMessageID, content)
			}
		default:
			var cmd tea.Cmd
			m.editInput, cmd = m.editInput.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.showReactionPicker {
		switch msg.String() {
		case "esc":
			m.showReactionPicker = false
			return m, nil
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
			key := m.reactionOptions[m.reactionCursor]
			// Search across all messages, not just roots
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
				if alreadyReacted {
					return m, removeReactionCmd(m.client, m.activeConversationID(), m.reactionTargetID, key)
				}
				return m, addReactionCmd(m.client, m.activeConversationID(), m.reactionTargetID, key)
			}
		}
		return m, nil
	}

	// Thread view — intercepts keys
		if m.showThread {
			// If typing, pass FIRST to input
			if m.isReplyTyping {
				if m.showMentionPopup {
					switch msg.String() {
					case "up":
						if m.mentionCursor > 0 {
							m.mentionCursor--
						}
						return m, nil
					case "down":
						if m.mentionCursor < len(m.mentionSuggestions)-1 {
							m.mentionCursor++
						}
						return m, nil
					case "enter", "tab":
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
						return m, nil
					case "esc":
						m.showMentionPopup = false
						if m.ready {
							rightInnerHeight := m.height - 6 - 2
							inputHeight := strings.Count(m.input.View(), "\n") + 1
							m.threadViewport.Height = rightInnerHeight - 10 - inputHeight
						}
						return m, nil
					}
				}

				switch msg.String() {
					case "esc":
						m.isReplyTyping = false
						m.input.Blur()
						m.input.Reset()
						// Restaurar threadViewport
							if m.ready {
								rightInnerHeight := m.height - 6 - 2
								m.threadViewport.Height = rightInnerHeight - 12
							}
							return m, nil
				case "enter":
						v := strings.TrimSpace(m.input.Value())
						if v != "" {
							m.input.Reset()
							m.isReplyTyping = false
							m.input.Blur()
							// Restaurar threadViewport
							if m.ready {
								rightInnerHeight := m.height - 6 - 2
								m.threadViewport.Height = rightInnerHeight - 12
							}
							mentions := resolveMentions(v, m.buildMemberIndex())
							return m, sendReplyCmd(m.client, m.activeConversationID(), m.threadParentID, v, mentions)
						}
					return m, nil
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
						return m, cmd
				}
			}
			// If not typing, handle thread navigation
			switch msg.String() {
			case "esc":
				m.showThread = false
				m.threadParentID = ""
				m.cursorMode = false
				return m, nil
					case "i", "r":
						m.isReplyTyping = true
						m.input.Focus()
						if m.ready {
							rightInnerHeight := m.height - 6 - 2
							inputHeight := strings.Count(m.input.View(), "\n") + 1
							m.threadViewport.Height = rightInnerHeight - 10 - inputHeight
						}
						return m, nil
			case "up", "k":
				if m.threadCursor > 0 {
					m.threadCursor--
					replies := repliesFor(m.messages, m.threadParentID)
					content := formatThread(m.threadParentMsg, replies, m.threadViewport.Width, m.userName, m.threadCursor, true)
					m.threadViewport.SetContent(content)
				} else {
					m.threadViewport.LineUp(1)
				}
				return m, nil
			case "down", "j":
				replies := repliesFor(m.messages, m.threadParentID)
				if m.threadCursor < len(replies) {
					m.threadCursor++
					content := formatThread(m.threadParentMsg, replies, m.threadViewport.Width, m.userName, m.threadCursor, true)
					m.threadViewport.SetContent(content)
				} else {
					m.threadViewport.LineDown(1)
				}
				return m, nil
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
					return m, getReactionsCmd(m.client, m.activeConversationID(), targetID, m.selfID)
				}
				return m, nil
			case "E":
				replies := repliesFor(m.messages, m.threadParentID)
				var targetMsg *graph.Message
				if m.threadCursor == 0 {
					targetMsg = &m.threadParentMsg
				} else if m.threadCursor-1 < len(replies) {
					targetMsg = &replies[m.threadCursor-1]
				}
				if targetMsg != nil && (targetMsg.FromName == "User" || targetMsg.FromName == m.userName || (m.selfID != "" && strings.HasSuffix(targetMsg.FromUserID, m.selfID))) {
					m.editMessageID = targetMsg.ID
					m.editOriginalBody = targetMsg.RawBody
					m.editInput.SetValue(cleanHTMLForEdit(targetMsg.RawBody))
					m.editInput.Focus()
					m.showEditPopup = true
				}
				return m, nil
			case "backspace", "delete":
				replies := repliesFor(m.messages, m.threadParentID)
				var targetMsg *graph.Message
				if m.threadCursor == 0 {
					targetMsg = &m.threadParentMsg
				} else if m.threadCursor-1 < len(replies) {
					targetMsg = &replies[m.threadCursor-1]
				}
				if targetMsg != nil && (targetMsg.FromName == "User" || targetMsg.FromName == m.userName || (m.selfID != "" && strings.HasSuffix(targetMsg.FromUserID, m.selfID))) {
					m.deleteMsgID = targetMsg.ID
					m.showDeleteMsgPopup = true
				}
				return m, nil
			}
			return m, nil
		}

		// Cursor mode — j/k move cursor, Enter opens thread
		if m.cursorMode && m.workspace == WorkspaceTeams && m.viewMode == ModeChat {
			switch msg.String() {
			case "esc":
				m.cursorMode = false
				m.messageCursor = 0
				content := formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode)
				m.viewport.SetContent(content)
				return m, nil
			case "e":
				rootMsgs := rootMessages(m.messages)
				if m.messageCursor < len(rootMsgs) {
					m.reactionTargetID = rootMsgs[m.messageCursor].ID
					m.loading = true
					return m, getReactionsCmd(m.client, m.activeConversationID(), m.reactionTargetID, m.selfID)
				}
				return m, nil
			case "E":
				rootMsgs := rootMessages(m.messages)
				if m.messageCursor < len(rootMsgs) {
					selected := rootMsgs[m.messageCursor]
					if selected.FromName == "User" || selected.FromName == m.userName || (m.selfID != "" && strings.HasSuffix(selected.FromUserID, m.selfID)) {
						m.editMessageID = selected.ID
						m.editOriginalBody = selected.RawBody
						m.editInput.SetValue(cleanHTMLForEdit(selected.RawBody))
						m.editInput.Focus()
						m.showEditPopup = true
					}
				}
				return m, nil
			case "backspace", "delete":
				rootMsgs := rootMessages(m.messages)
				if m.messageCursor < len(rootMsgs) {
					selected := rootMsgs[m.messageCursor]
					if selected.FromName == "User" || selected.FromName == m.userName || (m.selfID != "" && strings.HasSuffix(selected.FromUserID, m.selfID)) {
						m.deleteMsgID = selected.ID
						m.showDeleteMsgPopup = true
					}
				}
				return m, nil
			case "down", "k":
				if m.messageCursor > 0 {
					m.messageCursor--
				}
				content := formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode)
				m.viewport.SetContent(content)
				return m, nil
			case "up", "j":
				rootMsgs := rootMessages(m.messages)
				if m.messageCursor < len(rootMsgs)-1 {
					m.messageCursor++
				}
				content := formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode)
				m.viewport.SetContent(content)
				return m, nil
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
				return m, nil
			}
		}

		if m.cursorMode && m.workspace == WorkspaceDMs && m.viewMode == ModeChat {
			switch msg.String() {
			case "esc":
				m.cursorMode = false
				m.messageCursor = 0
				content := formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
				m.viewport.SetContent(content)
				return m, nil
			case "up", "k":
				validMsgs := validDMMessages(m.messages)
				if m.messageCursor < len(validMsgs)-1 {
					m.messageCursor++
				}
				content := formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
				m.viewport.SetContent(content)
				return m, nil
			case "down", "j":
				if m.messageCursor > 0 {
					m.messageCursor--
				}
				content := formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
				m.viewport.SetContent(content)
				return m, nil
			case "e":
				validMsgs := validDMMessages(m.messages)
				if m.messageCursor < len(validMsgs) {
					target := validMsgs[m.messageCursor]
					m.reactionTargetID = target.ID
					m.loading = true
					return m, getReactionsCmd(m.client, m.activeConversationID(), target.ID, m.selfID)
				}
				return m, nil
			case "E":
				validMsgs := validDMMessages(m.messages)
				if m.messageCursor < len(validMsgs) {
					selected := validMsgs[m.messageCursor]
					if selected.FromName == "User" || selected.FromName == m.userName || (m.selfID != "" && strings.HasSuffix(selected.FromUserID, m.selfID)) {
						m.editMessageID = selected.ID
						m.editOriginalBody = selected.RawBody
						m.editInput.SetValue(cleanHTMLForEdit(selected.RawBody))
						m.editInput.Focus()
						m.showEditPopup = true
					}
				}
				return m, nil
			case "backspace", "delete":
				validMsgs := validDMMessages(m.messages)
				if m.messageCursor < len(validMsgs) {
					selected := validMsgs[m.messageCursor]
					if selected.FromName == "User" || selected.FromName == m.userName || (m.selfID != "" && strings.HasSuffix(selected.FromUserID, m.selfID)) {
						m.deleteMsgID = selected.ID
						m.showDeleteMsgPopup = true
					}
				}
				return m, nil
			}
		}

		// Chat search — intercepts keys
		if m.isSearching {
			switch msg.String() {
			case "esc":
				m.isSearching = false
				m.searchInput.Reset()
				m.searchQuery = ""
				
				// Re-render chat
				var content string
				if m.workspace == WorkspaceDMs {
					content = formatMessagesDM(m.messages, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
				} else {
					content = formatMessagesWithCursor(m.messages, m.viewport.Width, m.messageCursor, m.cursorMode)
				}
				m.viewport.SetContent(content)
				return m, nil
			case "enter":
				m.isSearching = false
				return m, nil
			case "up", "down", "pgup", "pgdown", "ctrl+u", "ctrl+d":
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				
				m.searchQuery = strings.TrimSpace(m.searchInput.Value())
				
				// Filter messages and re-render
				var content string
				filtered := m.messages
				if m.searchQuery != "" {
					filtered = m.filterMessages(m.messages, m.searchQuery)
				}
				if m.workspace == WorkspaceDMs {
					content = formatMessagesDM(filtered, m.viewport.Width, m.userName, m.selfID, m.messageCursor, m.cursorMode)
				} else {
					content = formatMessagesWithCursor(filtered, m.viewport.Width, m.messageCursor, m.cursorMode)
				}
				m.viewport.SetContent(content)
				return m, cmd
			}
		}

		// Directory picker — intercepts all keys
		if m.showDirPicker {
			var cmd tea.Cmd
			m.dirPicker, cmd = m.dirPicker.Update(msg)
			if cmd != nil {
				return m, cmd
			}
			// Check if picker sent a result
			return m, nil
		}

		// Create team popup — intercepts keys
		if m.showCreateTeamPopup {
			switch msg.String() {
			case "esc":
				m.showCreateTeamPopup = false
				m.createTeamInput.Reset()
				m.createTeamErr = ""
				return m, nil
			case "enter":
				name := strings.TrimSpace(m.createTeamInput.Value())
				if name == "" {
					return m, nil
				}
				m.showCreateTeamPopup = false
				m.createTeamInput.Reset()
				m.teamCreating = true
				return m, createTeamCmd(m.client, name)
			default:
				var cmd tea.Cmd
				m.createTeamInput, cmd = m.createTeamInput.Update(msg)
				return m, cmd
			}
		}

		if m.showTeamInfo {
			if msg.String() == "esc" || msg.String() == "enter" {
				m.showTeamInfo = false
				m.teamInfo = nil
			}
			return m, nil
		}

		if m.showAddChannelMemberPopup {
			switch msg.String() {
			case "esc":
				m.showAddChannelMemberPopup = false
				m.addChannelMemberInput.Reset()
				m.addChannelMemberResults = nil
				m.addChannelMemberErr = ""
			case "up", "k":
				if m.addChannelMemberCursor > 0 {
					m.addChannelMemberCursor--
				}
			case "down", "j":
				if m.addChannelMemberCursor < len(m.addChannelMemberResults)-1 {
					m.addChannelMemberCursor++
				}
			case "g", "home":
				m.addChannelMemberCursor = 0
			case "G", "end":
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
					return m, addChannelMemberCmd(m.client, teamGUID, channelID, target)
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
			return m, nil
		}

		if m.showAddMemberPopup {
			switch msg.String() {
			case "esc":
				m.showAddMemberPopup = false
				m.addMemberInput.Reset()
				m.newDMResults = nil
				m.addMemberErr = ""
			case "up", "k":
				if m.newDMCursor > 0 {
					m.newDMCursor--
				}
			case "down", "j":
				if m.newDMCursor < len(m.newDMResults)-1 {
					m.newDMCursor++
				}
			case "g", "home":
				m.newDMCursor = 0
			case "G", "end":
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
					return m, addMemberCmd(m.client, m.teamThreadID, teamGUID, mri)
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
				return m, tea.Batch(cmds...)
			}
			return m, nil
		}

		if m.showRemoveMemberPopup {
			switch msg.String() {
			case "y", "enter":
				member := m.teamMembers[m.membersCursor]
				teamGUID := m.teams[m.selectedTeam].ID
				return m, removeMemberCmd(m.client, m.teamThreadID, teamGUID, member.ID)
			case "n", "esc":
				m.showRemoveMemberPopup = false
			}
			return m, nil
		}

		if m.showRemoveChannelMemberPopup {
			switch msg.String() {
			case "y", "enter":
				if m.channelMemberCursor < len(m.channelMembers) {
					member := m.channelMembers[m.channelMemberCursor]
					if member.ID != m.selfID {
						teamGUID := m.teams[m.selectedTeam].ID
						channelID := m.channels[m.selectedChan].ID
						return m, removeChannelMemberCmd(m.client, teamGUID, channelID, member.ID)
					}
				}
			case "n", "esc":
				m.showRemoveChannelMemberPopup = false
			}
			return m, nil
		}

		if m.showMembersPopup {
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
			return m, nil
		}

		// Delete channel popup
		if m.showDeleteChannelPopup {
			switch msg.String() {
			case "y", "enter":
				m.showDeleteChannelPopup = false
				channelID := m.channels[m.selectedChan].ID
				return m, deleteChannelCmd(m.client, m.teamThreadID, channelID)
			case "n", "esc":
				m.showDeleteChannelPopup = false
			}
			return m, nil
		}

		// Delete team popup
		if m.showDeleteTeamPopup {
			switch msg.String() {
			case "y", "enter":
				m.showDeleteTeamPopup = false
				return m, deleteTeamCmd(m.client, m.teamThreadID)
			case "n", "esc":
				m.showDeleteTeamPopup = false
			}
			return m, nil
		}

		// Create channel popup — intercepts keys
		if m.showCreateChannelPopup {
			switch msg.String() {
			case "esc":
				m.showCreateChannelPopup = false
				m.createChannelInput.Reset()
				m.createChannelErr = ""
				m.createChannelStep = 0
				return m, nil
			case "enter":
				if m.createChannelStep == 0 {
					name := strings.TrimSpace(m.createChannelInput.Value())
					if name == "" {
						return m, nil
					}
					m.createChannelStep = 1
					m.createChannelInput.Blur()
					return m, nil
				}
				// Step 1: type already selected, confirm
				name := strings.TrimSpace(m.createChannelInput.Value())
				m.showCreateChannelPopup = false
				m.createChannelInput.Reset()
				m.createChannelStep = 0
				teamGUID := m.teams[m.selectedTeam].ID
				return m, createChannelCmd(m.client, teamGUID, m.teamThreadID, name, m.createChannelType)
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
					return m, cmd
				}
			}
			return m, nil
		}

		// New DM popup — intercepts keys
		if m.showNewDMPopup {
			switch msg.String() {
			case "esc":
				m.showNewDMPopup = false
				m.newDMQuery.Reset()
				m.newDMResults = nil
				m.newDMErr = ""
				return m, nil
			case "up", "k":
				if m.newDMCursor > 0 {
					m.newDMCursor--
				}
			case "down", "j":
				if m.newDMCursor < len(m.newDMResults)-1 {
					m.newDMCursor++
				}
			case "g", "home":
				m.newDMCursor = 0
			case "G", "end":
				if len(m.newDMResults) > 0 {
					m.newDMCursor = len(m.newDMResults) - 1
				}
			case "enter":
				if len(m.newDMResults) > 0 {
					target := m.newDMResults[m.newDMCursor]
					m.showNewDMPopup = false
					m.newDMQuery.Reset()
					m.newDMResults = nil
					return m, createDMCmd(m.client, m.selfID, target.ID)
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
				return m, tea.Batch(cmds...)
			}
			return m, tea.Batch(cmds...)
		}

		// Presence menu — intercepts keys
		if m.showPresenceMenu {
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
				// m.selfID is already available from initial load
				if m.selfID != "" {
					cmds = append(cmds, setPresenceCmd(m.client, m.selfID, avail))
				}
			}
			return m, tea.Batch(cmds...)
		}

		// Download confirmation popup — intercepts keys before everything else
		if m.confirmingDownload {
			switch msg.String() {
			case "y", "enter":
				m.confirmingDownload = false
				m.downloading = true
				targets := m.downloadTargets
				driveID := m.currentFilesDriveID
				teamID := m.teams[m.selectedTeam].ID
				cmds = append(cmds, downloadFilesCmd(m.client, teamID, driveID, targets, m.prefs.DownloadDir))
				m.selectedFiles = make(map[int]bool)
				return m, tea.Batch(cmds...)
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
				return m, nil
			case "n", "esc":
				m.confirmingDownload = false
				m.downloadTargets = nil
				return m, nil
			}
			return m, nil // intercept all keys while the popup is open
		}

		switch msg.String() {
		case "/":
			if !m.focusLeft && m.viewMode == ModeChat {
				m.isSearching = true
				m.searchInput.Reset()
				m.searchInput.Focus()
				return m, nil
			}

		case "q", "ctrl+c":
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

		case "u":
			if m.workspace == WorkspaceTeams && m.viewMode == ModeFiles && !m.focusLeft {
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

		case "tab":
			m.focusLeft = !m.focusLeft

		case "1":
			m.workspace = WorkspaceTeams
			m.focusLeft = true
			m.focusList = 0

		case "2":
			m.workspace = WorkspaceDMs
			m.focusLeft = true
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
			m.workspace = WorkspaceActivity
			m.focusLeft = true
			if !m.notifLoaded {
				m.loading = true
				m.selectedNotif = 0
				cmds = append(cmds, loadNotificationsCmd(m.client))
			}

		case "4":
			m.workspace = WorkspaceAssignments
			m.focusLeft = true
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
			if m.focusLeft {
				if m.workspace == WorkspaceDMs {
					if len(m.chats) > 0 && m.selectedChat > 0 {
						m.selectedChat--
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
					if m.selectedTeam > 0 {
						m.selectedTeam--
						m.loading = true
						m.teamMembers = nil // invalidate members cache on team change
						cmds = append(cmds, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID))
					}
				} else if m.focusList == 1 && len(m.channels) > 0 {
					if m.selectedChan > 0 {
						m.selectedChan--
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
			if m.focusLeft {
				if m.workspace == WorkspaceDMs {
					if len(m.chats) > 0 && m.selectedChat < len(m.chats)-1 {
						m.selectedChat++
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
					if m.selectedTeam < len(m.teams)-1 {
						m.selectedTeam++
						m.loading = true
						m.teamMembers = nil // invalidate members cache on team change
						cmds = append(cmds, loadChannelsCmd(m.client, m.teams[m.selectedTeam].ID))
					}
				} else if m.focusList == 1 && len(m.channels) > 0 {
					if m.selectedChan < len(m.channels)-1 {
						m.selectedChan++
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
				if m.activityFilter > FilterAll {
					m.activityFilter--
					m.selectedNotif = 0
				}
			} else if m.focusLeft && m.workspace == WorkspaceAssignments {
				if m.assignFilter > FilterAll {
					m.assignFilter--
					m.selectedAssign = 0
				}
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
				if m.activityFilter < FilterOverdue {
					m.activityFilter++
					m.selectedNotif = 0
				}
			} else if m.focusLeft && m.workspace == WorkspaceAssignments {
				if m.assignFilter < FilterCompleted {
					m.assignFilter++
					m.selectedAssign = 0
				}
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
						// Check cache for root
						cacheKey := "root:" + m.channels[m.selectedChan].ID
						if cached, ok := m.folderCache[cacheKey]; ok {
							m.files = cached
							m.selectedFile = 0
							m.selectedFiles = make(map[int]bool)
							m.viewport.SetContent(renderFilesContent(&m))
							m.viewport.GotoTop()
						} else {
							m.loading = true
							cmds = append(cmds, loadFilesCmd(m.client, m.teams[m.selectedTeam].ID, m.channels[m.selectedChan].DisplayName, m.channels[m.selectedChan].ID))
						}
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
						m.workspace    = WorkspaceTeams
						m.focusLeft    = false
						m.viewMode     = ModeChat
						m.loadedConvID = n.SourceThread
						m.loading      = true
						m.messagesBackwardLink = ""
					m.loadingMore = false
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
			if !m.focusLeft && m.viewMode == ModeFiles && len(m.files) > 0 && !m.previewing {
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
			// Delete team — solo en focusList==0
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
			if m.focusLeft && m.workspace == WorkspaceActivity && len(m.notifications) > 0 {
			    n := &m.notifications[m.selectedNotif]
			    if !n.IsRead {
			        n.IsRead = true
			        cmds = append(cmds, markNotifReadCmd(m.client, n.ID))
			    }
			    m.focusLeft = false
			} else if m.focusLeft && m.workspace == WorkspaceAssignments {
				m.focusLeft = false
			} else if m.focusLeft && m.workspace == WorkspaceDMs && len(m.chats) > 0 {
				m.loading = true
				m.focusLeft = false
				m.isTyping = false
				m.viewMode = ModeChat // MUST RESET
				m.selectedFiles = make(map[int]bool)
				m.folderStack = nil
				chatID := m.chats[m.selectedChat].ID
				m.loadedConvID = chatID
				delete(m.chatUnread, chatID) // Clear badge on open
				m.messagesBackwardLink = ""
					m.loadingMore = false
					cmds = append(cmds, loadMessagesCmd(m.client, "", chatID, 200))
			} else if m.focusLeft && m.workspace == WorkspaceTeams && m.focusList == 0 {
				if len(m.teams) > 0 {
					m.focusList = 1
				}
			} else if m.focusLeft && m.focusList == 1 && len(m.channels) > 0 {
				m.loading = true
				m.focusLeft = false
				m.isTyping = false
				m.viewMode = ModeChat // MUST RESET
				m.selectedFiles = make(map[int]bool)
				m.folderStack = nil
				chanID := m.channels[m.selectedChan].ID
				m.loadedConvID = chanID
				m.messagesBackwardLink = ""
					m.loadingMore = false
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
					m.folderStack = append(m.folderStack, node)
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
					m.folderStack = append(m.folderStack, node)
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
	}

	return m, tea.Batch(cmds...)
}

func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	}
	if cmd == nil {
		return fmt.Errorf("no clipboard utility found")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// makeClickableLink wraps text with the ANSI OSC 8 sequence
// supported by modern terminals (Kitty, Alacritty, GNOME Terminal)
func makeClickableLink(text, url string) string {
	// \x1b]8;;URL\x1b\ TEXT \x1b]8;;\x1b\
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	}
	if err != nil {
		// Log error if needed, but for TUI we just silently fail or could send an error msg
	}
}

type previewResultMsg struct {
	content     string
	fileName    string
	err         error
	openBrowser bool   // true if opened in browser
	status      string // message to show in downloadStatus
}

func isTextFile(name string) bool {
	lower := strings.ToLower(name)
	textExts := []string{".txt", ".md", ".json", ".csv", ".xml", ".html", ".css", ".js", ".go", ".py", ".rs", ".log", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".sh", ".bash", ".sql", ".r", ".java", ".c", ".cpp", ".h", ".hpp", ".ts", ".tsx", ".jsx"}
	for _, ext := range textExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func previewFileCmd(client *graph.Client, item graph.DriveItem, driveID, teamID string) tea.Cmd {
	return func() tea.Msg {
		if !isTextFile(item.Name) {
			// Always use WebUrl so the browser shows preview (SharePoint/OneDrive)
			url := item.WebUrl
			openBrowser(url)
			return previewResultMsg{
				openBrowser: true,
				status:      fmt.Sprintf("Opening %s in browser...", item.Name),
			}
		}

		// Download to temp and read
		var body io.ReadCloser
		var err error
		if item.ID != "" {
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
				err = resolveErr
			}
		}
		if err != nil {
			return previewResultMsg{err: err}
		}
		defer body.Close()

		// Limit to 500KB
		limitedReader := io.LimitReader(body, 500*1024)
		data, err := io.ReadAll(limitedReader)
		if err != nil {
			return previewResultMsg{err: err}
		}

		return previewResultMsg{content: string(data), fileName: item.Name}
	}
}

func renderFilesContent(m *Model) string {
	if len(m.files) == 0 {
		return "  This folder is empty."
	}
	var b strings.Builder
	for i, f := range m.files {
		cursor := "  "
		style := normalItemStyle
		if i == m.selectedFile {
			cursor = "▶ "
			style = selectedItemStyle
		}

		checkbox := "  "
		if m.selectedFiles[i] {
			checkbox = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("✓ ") // green
		}

		icon := teams.GetFileIcon(f)
		link := f.DownloadUrl
		if link == "" {
			link = f.WebUrl
		}

		line := fmt.Sprintf("%s%s %s", checkbox, icon, f.Name)
		line = style.Render(line)

		clickableLine := makeClickableLink(line, link)

		b.WriteString(cursor + clickableLine + "\n")
	}

	if m.workspace == WorkspaceDMs {
		b.WriteString("\n\n  " + helpStyle.Render("(Showing recent attachments. Press 'C' to load full history)"))
	}

	if m.downloadStatus != "" {
		b.WriteString("\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(m.downloadStatus))
	}

	return b.String()
}

// resolveMentions parses @Name tokens from text and cross-references against
// a member list. Returns resolved MentionedUser slice with sequential ItemIDs.
// nameToMRI maps DisplayName → "8:orgid:GUID".
func resolveMentions(text string, nameToMRI map[string]string) []graph.MentionedUser {
	var mentions []graph.MentionedUser
	// Find all @word+ sequences (greedy, supports multi-word names like "@Juan Perez")
	// Strategy: scan for '@', then try longest match first against known names.
	seen := make(map[string]bool)
	itemID := 0

	// Build sorted list of names by length descending (longest match wins)
	names := make([]string, 0, len(nameToMRI))
	for name := range nameToMRI {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return len(names[i]) > len(names[j])
	})

	for _, name := range names {
		token := "@" + name
		if strings.Contains(text, token) && !seen[name] {
			seen[name] = true
			mentions = append(mentions, graph.MentionedUser{
				ItemID:      itemID,
				MRI:         nameToMRI[name],
				DisplayName: name,
			})
			itemID++
		}
	}
	return mentions
}

// buildMemberIndex builds a DisplayName → MRI map from team members and chat members.
func (m *Model) buildMemberIndex() map[string]string {
	index := make(map[string]string)
	// Team members (WorkspaceTeams)
	for _, tm := range m.teamMembers {
		if tm.DisplayName != "" && tm.ID != "" {
			index[tm.DisplayName] = "8:orgid:" + tm.ID
		}
	}
	// Chat members (WorkspaceDMs) — UserID already is the GUID
	if m.workspace == WorkspaceDMs && m.selectedChat < len(m.chats) {
		for _, member := range m.chats[m.selectedChat].Members {
			if member.DisplayName != "" && member.UserID != "" {
				index[member.DisplayName] = "8:orgid:" + member.UserID
			}
		}
	}
	return index
}

func (m *Model) filterMessages(msgs []graph.Message, query string) []graph.Message {
	if query == "" {
		return msgs
	}
	query = strings.ToLower(query)
	var filtered []graph.Message
	for _, msg := range msgs {
		if strings.Contains(strings.ToLower(msg.Body), query) || strings.Contains(strings.ToLower(msg.FromName), query) {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func isSharePointURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "sharepoint.com") || strings.Contains(lower, "onedrive.live.com")
}



func markNotifReadCmd(client *graph.Client, msgID string) tea.Cmd {
    return func() tea.Msg {
        err := client.MarkNotificationRead(msgID)
        return markNotifReadMsg{err}
    }
}
