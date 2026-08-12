package ui

import (
	"fmt"
	"io"
	"lazyteams/internal/graph"
	"lazyteams/internal/teams"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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

func makeClickableLink(text, url string) string {
	// Strip any escape characters from the URL to prevent terminal injection
	safeURL := strings.ReplaceAll(url, "\x1b", "")
	// Strip terminal control characters from the visible text as well: sender
	// and attachment names come from untrusted server data.
	safeText := sanitizeName(text)
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", safeURL, safeText)
}

// nameControlRe strips C0/C1 control bytes (ESC, BEL, CR, LF, ...) from
// single-line names rendered into the terminal, preserving the \x11/\x12
// search sentinels and the \x1E/\x1F mention sentinels.
var nameControlRe = regexp.MustCompile(`[\x00-\x10\x13-\x1D\x7F\x80-\x9F]`)

func sanitizeName(s string) string {
	return nameControlRe.ReplaceAllString(s, "")
}

var (
	markdownLinkRe = regexp.MustCompile(`\[[^\]]*\]\((https?://[^)\s]+)\)`)
	plainURLRe     = regexp.MustCompile(`https?://[^\s)\x1e\x1f]+`)
)

// messageLink returns the first openable link in a message: an explicit link
// attachment, then the first http(s) URL in the body, then any other
// attachment URL. Returns "" when the message carries no link.
func messageLink(msg graph.Message) string {
	for _, att := range msg.Attachments {
		if att.Type == "link" && att.URL != "" {
			return att.URL
		}
	}
	if m := markdownLinkRe.FindStringSubmatch(msg.Body); len(m) > 1 {
		return m[1]
	}
	if m := plainURLRe.FindString(msg.Body); m != "" {
		return m
	}
	for _, att := range msg.Attachments {
		if att.URL != "" && att.Type != "link" {
			return att.URL
		}
	}
	return ""
}

func openBrowser(url string) {
	// Security filter: only allow safe web schemes to prevent local file execution
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return
	}

	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		// Intentionally ignore background errors
	}
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

func getAssignFile(a graph.Assignment, cursor int) *graph.AssignmentFile {
	if cursor < len(a.RefFiles) {
		return &a.RefFiles[cursor]
	}
	idx := cursor - len(a.RefFiles)
	if idx < len(a.MyFiles) {
		return &a.MyFiles[idx]
	}
	return nil
}

// buildSharePointViewerURL converts a raw SharePoint file URL into
// an Office Online viewer URL that works even for read-only files.
func buildSharePointViewerURL(webURL string) string {
	// empty URL case
	if webURL == "" {
		return ""
	}

	// Extract base (scheme + host) and path
	u, err := url.Parse(webURL)
	if err != nil {
		return webURL
	}
	// Insert /:b:/r before the path to activate the Office Online viewer
	viewerPath := "/:b:/r" + u.Path
	u.Path = viewerPath
	q := u.Query()
	q.Set("csf", "1")
	q.Set("web", "1")
	u.RawQuery = q.Encode()
	return u.String()
}

func previewFileCmd(client *graph.Client, item graph.DriveItem, driveID, teamID string) tea.Cmd {
	return func() tea.Msg {
		if !isTextFile(item.Name) {
			viewerURL := buildSharePointViewerURL(item.WebUrl)
			openBrowser(viewerURL)
			return previewResultMsg{
				openBrowser: true,
				status:      fmt.Sprintf("Opening %s in browser...", item.Name),
			}
		}

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
			if item.WebUrl != "" {
				viewerURL := buildSharePointViewerURL(item.WebUrl)
				openBrowser(viewerURL)
				return previewResultMsg{
					openBrowser: true,
					status:      fmt.Sprintf("No download access — opening %s in browser...", item.Name),
				}
			}
			return previewResultMsg{err: err}
		}
		defer body.Close()

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
			cursor = symCursor
			style = selectedItemStyle
		}

		checkbox := "  "
		if m.selectedFiles[i] {
			checkbox = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("✓ ")
		}

		icon := teams.GetFileIcon(f)
		link := f.DownloadUrl
		if link == "" {
			link = f.WebUrl
		}

		meta := ""
		if f.LastModifiedDateTime != "" {
			if t, err := time.Parse(time.RFC3339, f.LastModifiedDateTime); err == nil {
				age := t.Local().Format("02 Jan 2006")
				who := sanitizeName(f.LastModifiedBy.User.DisplayName)
				if who != "" {
					if len(who) > 25 {
						who = who[:25]
					}
					meta = metaStyle.Render(age + " · " + who)
				} else {
					meta = metaStyle.Render(age)
				}
			}
		}

		prefix := checkbox + icon + " "
		prefixWidth := lipgloss.Width(prefix)
		metaWidth := lipgloss.Width(meta)
		availableWidth := m.viewport.Width - lipgloss.Width(cursor) - 2
		nameMax := availableWidth - prefixWidth - metaWidth
		if nameMax < 10 {
			nameMax = 10
		}
		name := truncateText(f.Name, nameMax)
		padLen := availableWidth - prefixWidth - lipgloss.Width(name) - metaWidth
		if padLen < 1 {
			padLen = 1
		}
		pad := strings.Repeat(" ", padLen)

		namePart := style.Render(prefix + name)
		line := namePart + pad + meta
		clickableLine := makeClickableLink(line, link)
		b.WriteString(cursor + clickableLine + "\n")
	}

	if m.workspace == WorkspaceDMs {
		b.WriteString("\n\n  " + helpStyle.Render("(Showing recent attachments. Press 'C' to load full history)"))
	}

	if m.downloadStatus != "" {
		status := strings.ReplaceAll(m.downloadStatus, "\n", "\n  ")
		b.WriteString("\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(status))
	}

	return b.String()
}

func resolveMentions(text string, nameToMRI map[string]string) []graph.MentionedUser {
	var mentions []graph.MentionedUser
	seen := make(map[string]bool)
	itemID := 0

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

func (m *Model) buildMemberIndex() map[string]string {
	index := make(map[string]string)
	for _, tm := range m.teamMembers {
		if tm.DisplayName != "" && tm.ID != "" {
			index[tm.DisplayName] = "8:orgid:" + tm.ID
		}
	}
	if m.workspace == WorkspaceDMs && m.selectedChat < len(m.chats) {
		for _, member := range m.chats[m.selectedChat].Members {
			if member.DisplayName != "" && member.UserID != "" {
				index[member.DisplayName] = "8:orgid:" + member.UserID
			}
		}
	}
	return index
}

// activeSearchCursor returns the current search result cursor, or -1 when no
// search is active. It is passed to the format functions to highlight the
// current match.
func (m *Model) activeSearchCursor() int {
	if m.isSearching {
		return m.searchCursor
	}
	return -1
}

func (m *Model) filterMessages(msgs []graph.Message, query string) []graph.Message {
	if query == "" {
		return msgs
	}
	queryLower := strings.ToLower(query)
	re := regexp.MustCompile(`(?i)(` + regexp.QuoteMeta(query) + `)`)
	var filtered []graph.Message
	for _, msg := range msgs {
		matched := false
		if strings.Contains(strings.ToLower(msg.Body), queryLower) {
			msg.Body = re.ReplaceAllString(msg.Body, "\x11$1\x12")
			matched = true
		}
		if strings.Contains(strings.ToLower(msg.FromName), queryLower) {
			msg.FromName = re.ReplaceAllString(msg.FromName, "\x11$1\x12")
			matched = true
		}
		if matched {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// countSearchMatches returns the number of messages whose body or sender
// contains the query (case-insensitive). This is the total result count shown
// while searching.
func (m *Model) countSearchMatches(msgs []graph.Message, query string) int {
	if query == "" {
		return 0
	}
	queryLower := strings.ToLower(query)
	count := 0
	for _, msg := range msgs {
		if strings.Contains(strings.ToLower(msg.Body), queryLower) ||
			strings.Contains(strings.ToLower(msg.FromName), queryLower) {
			count++
		}
	}
	return count
}

func isSharePointURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "sharepoint.com") || strings.Contains(lower, "onedrive.live.com")
}

func nextVisibleTeam(teams []graph.Team, hiddenIDs []string, current int, showHidden bool, dir int) int {
	total := len(teams)
	next := current + dir
	for next >= 0 && next < total {
		isHidden := contains(hiddenIDs, teams[next].ID)
		if showHidden && isHidden {
			return next
		}
		if !showHidden && !isHidden {
			return next
		}
		next += dir
	}
	return current
}

func nextVisibleChannel(channels []graph.Channel, hiddenIDs []string, current int, showHidden bool, dir int) int {
	total := len(channels)
	next := current + dir
	for next >= 0 && next < total {
		isHidden := contains(hiddenIDs, channels[next].ID)
		if showHidden && isHidden {
			return next
		}
		if !showHidden && !isHidden {
			return next
		}
		next += dir
	}
	return current
}

func remove(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

func appendFolderNode(stack []FolderNode, node FolderNode) []FolderNode {
	if len(stack) > 0 {
		last := stack[len(stack)-1]
		if last.ID == node.ID && last.DriveID == node.DriveID {
			return stack
		}
	}
	return append(stack, node)
}

// isLastChannelOwner reports whether userID is the only Owner in the given
// channel member list. Used to block demoting the last owner, since the Teams
// Fabric API does not enforce that restriction server-side.
func isLastChannelOwner(members []graph.TeamMember, userID string) bool {
	ownerCount := 0
	for _, member := range members {
		if member.Role == "Owner" {
			ownerCount++
		}
	}
	return ownerCount == 1 && containsTeamMemberID(members, userID, "Owner")
}

func containsTeamMemberID(members []graph.TeamMember, userID, role string) bool {
	for _, member := range members {
		if member.ID == userID && member.Role == role {
			return true
		}
	}
	return false
}

func markNotifReadCmd(client *graph.Client, msgID string) tea.Cmd {
	return func() tea.Msg {
		err := client.MarkNotificationRead(msgID)
		return markNotifReadMsg{err}
	}
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func chatPriority(ch graph.Chat, selfChatID string) int {
	if (selfChatID != "" && ch.ID == selfChatID) || ch.Topic == "Personal notes (You)" {
		return 0
	}
	switch ch.ChatType {
	case "oneOnOne":
		return 1
	case "group":
		return 2
	default:
		return 3
	}
}

func sortChats(chats []graph.Chat, unreadMap map[string]bool, selfChatID string) {
	sort.SliceStable(chats, func(i, j int) bool {
		a, b := chats[i], chats[j]
		p1, p2 := chatPriority(a, selfChatID), chatPriority(b, selfChatID)
		if p1 != p2 {
			return p1 < p2
		}

		u1, u2 := unreadMap[a.ID], unreadMap[b.ID]
		if u1 && !u2 {
			return true
		}
		if !u1 && u2 {
			return false
		}

		return a.LastUpdatedDateTime > b.LastUpdatedDateTime
	})
}

func (m *Model) ReSortChats() {
	var currID string
	if m.selectedChat >= 0 && m.selectedChat < len(m.chats) {
		currID = m.chats[m.selectedChat].ID
	}
	selfChatID := ""
	if m.selfID != "" {
		selfChatID = fmt.Sprintf("19:%s_%s@unq.gbl.spaces", m.selfID, m.selfID)
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
}

func dmChatIndices(chats []graph.Chat) []int {
	var result []int
	for i, ch := range chats {
		if ch.ChatType == "oneOnOne" {
			result = append(result, i)
		}
	}
	return result
}

func groupChatIndices(chats []graph.Chat) []int {
	var result []int
	for i, ch := range chats {
		if ch.ChatType != "oneOnOne" {
			result = append(result, i)
		}
	}
	return result
}

func visibleChatIndices(chats []graph.Chat, dmCollapsed, groupCollapsed bool) []int {
	var result []int
	for i, ch := range chats {
		if ch.ChatType == "oneOnOne" && !dmCollapsed {
			result = append(result, i)
		} else if ch.ChatType != "oneOnOne" && !groupCollapsed {
			result = append(result, i)
		}
	}
	return result
}

func isInDMs(chats []graph.Chat, idx int) bool {
	if idx < 0 || idx >= len(chats) {
		return false
	}
	return chats[idx].ChatType == "oneOnOne"
}

func isInGroup(chats []graph.Chat, idx int) bool {
	if idx < 0 || idx >= len(chats) {
		return false
	}
	return chats[idx].ChatType != "oneOnOne"
}

func indexOf(slice []int, val int) int {
	for i, v := range slice {
		if v == val {
			return i
		}
	}
	return -1
}

func (m *Model) recalculateViewportHeight() {
	if !m.ready || m.height == 0 {
		return
	}
	panelOuterHeight := m.height - 6
	if m.mobileMode {
		panelOuterHeight = m.height - 2
	}
	if panelOuterHeight < 2 {
		panelOuterHeight = 2
	}
	rightInnerHeight := panelOuterHeight - 2

	inputHeight := strings.Count(m.input.View(), "\n") + 1

	popupHeight := 0
	if m.showMentionPopup && len(m.mentionSuggestions) > 0 {
		lines := len(m.mentionSuggestions)
		if lines > 5 {
			lines = 5
		}
		popupHeight = lines + 2
	}

	pendingLines := 0
	if len(m.pendingImages) > 0 {
		pendingLines = 1
	}

	replyLines := 0
	if m.replyToMsg != nil {
		available := m.width - 5
		leftOuterWidth := available / 3
		rightOuterWidth := available - leftOuterWidth
		rightInnerWidth := rightOuterWidth - 4

		name := m.replyToMsg.FromName
		if name == "User" {
			name = m.userName
		}
		preview := m.replyToMsg.Body
		if len([]rune(preview)) > 50 {
			preview = string([]rune(preview)[:50]) + "..."
		}
		replyBarText := fmt.Sprintf("↩ Replying to %s: \"%s\"", name, preview)
		textWidth := lipgloss.Width(replyBarText)
		if rightInnerWidth > 0 {
			replyLines = (textWidth + rightInnerWidth - 1) / rightInnerWidth
		} else {
			replyLines = 1
		}
		if replyLines < 1 {
			replyLines = 1
		}
	}

	// 4 lines for header (Title + \n + Tabs + \n\n)
	// 1 line for the \n after the viewport
	newVpHeight := rightInnerHeight - 4 - 1 - inputHeight - popupHeight - pendingLines - replyLines
	if newVpHeight < 5 {
		newVpHeight = 5
	}
	m.viewport.Height = newVpHeight
	m.threadViewport.Height = newVpHeight - 6

	// Recalculate width according to active mode
	if m.mobileMode {
		mobileWidth := m.width - 2
		if mobileWidth < 20 {
			mobileWidth = 20
		}
		mobileInnerWidth := mobileWidth - 2 // minus border
		m.viewport.Width = mobileInnerWidth
		m.threadViewport.Width = mobileInnerWidth
	} else {
		available := m.width - 5
		leftOuterWidth := available / 3
		rightOuterWidth := available - leftOuterWidth
		m.viewport.Width = rightOuterWidth - 2
		m.threadViewport.Width = rightOuterWidth - 2
	}
}

func (m *Model) getMobileWidth() int {
	w := m.width - 2
	if w < 10 {
		return 10
	}
	return w
}

func (m *Model) getMobileHeight() int {
	h := m.height - 1
	if h < 5 {
		return 5
	}
	return h
}
