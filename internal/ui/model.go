package ui

import (
	"os"

	"lazyteams/internal/graph"
	"lazyteams/internal/ui/components/directorypicker"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type errMsg struct{ err error }

type PendingImage struct {
	Data        []byte
	ContentType string
}

type clipboardImageLoadedMsg struct {
	Data        []byte
	ContentType string
}

type clipboardImageErrMsg struct {
	err error
}

type channelsErrMsg struct {
	teamID string
	err    error
}
type messagesErrMsg struct {
	err            error
	conversationID string
	partialMsgs    []graph.Message // messages loaded before the failure
}

type teamsMsg struct {
	teams []graph.Team
}

type channelsMsg struct {
	teamID   string
	channels []graph.Channel
}

type messagesMsg struct {
	messages     []graph.Message
	backwardLink string // empty if no more pages
}

type notificationsMsg struct {
	items []graph.NotificationItem
}
type notificationsErrMsg struct {
	err error
}
type pollNotificationsMsg struct {
	items []graph.NotificationItem
	err   error
}

type assignmentsMsg struct {
	items []graph.Assignment
}

type assignmentDetailMsg struct {
	assignmentID       string
	refFiles           []graph.AssignmentFile
	myFiles            []graph.AssignmentFile
	resourcesFolderUrl string
	err                error
}

type assignmentsErrMsg struct {
	err error
}

type assignmentUploadDoneMsg struct {
	assignmentID string
	fileName     string
	err          error
}

type assignmentSubmitDoneMsg struct {
	assignmentID string
	err          error
}

type assignmentUndoSubmitDoneMsg struct {
	assignmentID string
	err          error
}

type assignmentRemoveResourceDoneMsg struct {
	assignmentID string
	resourceID   string
	fileName     string
	err          error
}

type ViewMode int

const (
	ModeChat ViewMode = iota
	ModeFiles
	ModeInfo
)

type ActivityFilter int

const (
	FilterAll       ActivityFilter = iota
	FilterUnread                   // Not read
	FilterUpcoming                 // Upcoming (due in ≤7 days)
	FilterOverdue                  // Overdue
	FilterCompleted                // Completed/Submitted
)

type NotifFilter int

const (
	NotifFilterAll NotifFilter = iota
	NotifFilterUnread
	NotifFilterMentions    // activityType == "mention"
	NotifFilterTagMentions // activityType == "tagMention"
)

type Workspace int

const (
	WorkspaceTeams Workspace = iota
	WorkspaceDMs
	WorkspaceActivity
	WorkspaceAssignments
)

type FolderNode struct {
	ID   string
	Name string
	// DriveID, if not empty, indicates this node lives in a different
	// Drive than the Team's (shortcut/remoteItem case, e.g. Class Materials).
	DriveID string
}

type Model struct {
	client       *graph.Client
	teams        []graph.Team
	teamsLoaded  bool
	channels     []graph.Channel
	messages     []graph.Message
	files        []graph.DriveItem
	viewMode     ViewMode
	workspace    Workspace
	chats        []graph.Chat
	selectedChat int
	chatsLoaded  bool
	selfID       string
	selectedTeam int
	loadedConvID string
	prefs        Preferences
	selectedChan int
	selectedFile int
	focusLeft    bool // true = focus on left panel (teams/channels)
	focusList    int  // 0 = teams, 1 = channels (within left panel)
	loading      bool
	err          error
	width        int
	height       int

	// Viewports and state
	leftVp           viewport.Model
	viewport         viewport.Model
	ready            bool
	channelErr       error
	folderStack      []FolderNode // Stack of nodes for navigating back in subfolders and building the path
	currentFilesRoot FolderNode   // Root node of the current channel (holds DriveID for private channels)

	// File downloads
	currentFilesDriveID string       // "" = default team drive; otherwise explicit drive ID
	selectedFiles       map[int]bool // indices marked for multi-download
	confirmingDownload  bool         // true while the confirmation popup is showing
	downloadTargets     []graph.DriveItem
	downloadStatus      string                       // result message
	downloadStatusID    int                          // increments with each download, prevents an old clear from erasing a new status
	downloading         bool                         // true while downloading
	folderCache         map[string][]graph.DriveItem // folder cache: folderID → contents
	filesRefreshing     bool                         // true while a background files auto-refresh is in flight

	// channelID → teamID map for navigation from notifications
	channelToTeam map[string]string

	// teamThreadID is the threadId of the current team's General channel
	teamThreadID string

	// Thread view
	messageCursor   int
	cursorMode      bool
	showThread      bool
	threadParentID  string
	threadParentMsg graph.Message
	threadViewport  viewport.Model
	threadCursor    int // 0 = parent, 1..N = replies
	isReplyTyping   bool

	// Reactions
	showReactionPicker bool
	reactionCursor     int
	reactionTargetID   string
	reactionOptions    []string // keys
	reactionPending    bool

	// Edit message
	showEditPopup    bool
	editInput        textinput.Model
	editMessageID    string
	editOriginalBody string

	// Delete message
	showDeleteMsgPopup bool
	deleteMsgID        string

	// Mention autocomplete
	mentionQuery       string             // text after the active @, e.g. "mon"
	mentionSuggestions []graph.TeamMember // filtered list
	mentionCursor      int                // selected index in the popup
	showMentionPopup   bool
	mentionAtPos       int // position of the @ in the textarea value

	// Pagination
	messagesBackwardLink string // URL for loading older messages
	loadingMore          bool   // true while fetching older messages
	forceScrollBottom    bool   // true when we should force scroll to bottom on next message load

	// Input for sending messages
	input    textarea.Model
	isTyping bool

	// Pending clipboard images (not uploaded yet)
	pendingImages []PendingImage

	// Reply-to message in DM cursor mode; nil when not replying
	replyToMsg *graph.Message

	// Search in chat
	isSearching      bool
	searchInput      textinput.Model
	searchQuery      string
	searchCursor     int // index of the currently highlighted result (within the filtered list)
	searchMatchCount int // number of messages that match the current query

	// Token renewal
	tokenRenewing             bool
	tokenRenewingType         string // "graph", "fabric", "web", "notif", "edu"
	tokenRenewErr             string
	tokenRenewalQueue         []string
	tokenRenewFailures        []string
	tokenRenewalFrame         int
	tokenRenewalSpinnerActive bool
	debug                     bool
	renewalProc               *os.Process // running lazyteams-auth child, killed on quit

	// User
	userName string

	// Help Menu
	showHelp bool

	// Presence (selection)
	showPresenceMenu bool
	presenceCursor   int
	presenceOptions  []string
	presenceError    string

	// Activity / Notifications
	notifications           []graph.NotificationItem
	notifLoaded             bool
	notificationsRefreshing bool
	selectedNotif           int
	notifErr                error
	activityFilter          NotifFilter
	mobileMode              bool

	// Latest release tag when an update is available (from the GitHub check).
	latestVersion string

	// Assignments / Tasks
	assignments      []graph.Assignment
	assignLoaded     bool
	selectedAssign   int
	assignFileCursor int
	assignErr        error
	assignFilter     ActivityFilter

	// DM polling
	chatUnread            map[string]bool   // chatID → has unread messages
	presence              map[string]string // userID → Availability (Available, Busy, Away, Offline, etc.)
	dmSectionCollapsed    bool
	groupSectionCollapsed bool
	cursorOnDMHeader      bool
	cursorOnGroupHeader   bool

	// File preview
	previewing      bool   // true while showing file preview
	previewContent  string // file content to display
	previewFileName string // name of the file being previewed

	// New DM creation
	showNewDMPopup bool
	newDMQuery     textinput.Model
	newDMResults   []graph.UserSearchResult
	newDMCursor    int
	newDMErr       string

	// File upload
	uploading bool

	// Directory picker
	showDirPicker bool
	dirPicker     directorypicker.Model
	pickerPurpose string // "download" or "upload"

	// Create team
	showCreateTeamPopup bool
	createTeamInput     textinput.Model
	createTeamErr       string
	teamCreating        bool

	// Create channel
	showCreateChannelPopup bool
	createChannelInput     textinput.Model
	createChannelType      string // "Standard", "Private", "Shared"
	createChannelStep      int    // 0=name, 1=type
	createChannelErr       string

	// Show hidden teams
	showHidden bool
	// Show hidden channels
	showHiddenChannels bool

	// File management
	showDeleteFilePopup   bool
	showCreateFolderPopup bool
	createFolderInput     textinput.Model
	createFolderErr       string

	// Delete confirmations
	showDeleteChannelPopup bool
	showDeleteTeamPopup    bool

	// Info panels
	showTeamInfo bool
	teamInfo     *graph.Team
	channelInfo  *graph.Channel

	// Team members
	showMembersPopup      bool
	teamMembers           []graph.TeamMember
	membersLoading        bool
	membersLoadSilent     bool // true when loading members for mention resolution, not for display
	membersCursor         int
	showRemoveMemberPopup bool

	// Add member (team)
	showAddMemberPopup bool
	addMemberInput     textinput.Model
	addMemberErr       string

	// Add member to channel
	showAddChannelMemberPopup    bool
	addChannelMemberInput        textinput.Model
	addChannelMemberResults      []graph.TeamMember
	addChannelMemberCursor       int
	addChannelMemberErr          string
	showRemoveChannelMemberPopup bool
	channelMemberCursor          int
	showChangeChannelRolePopup   bool
	changeChannelRoleCursor      int // 0 = Owner, 1 = Member
	channelRoleErr               string

	// Channel members (separate from team members)
	channelMembers []graph.TeamMember
}

func New(client *graph.Client, userName string, debug bool) Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (Esc to cancel, Enter to send, Ctrl+P to paste image)"
	ta.ShowLineNumbers = false
	ta.CharLimit = 4000
	ta.SetWidth(50)
	ta.SetHeight(1)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	newDMInput := textinput.New()
	newDMInput.Placeholder = "Search by name..."
	newDMInput.CharLimit = 64
	newDMInput.Width = 40

	createTeamInput := textinput.New()
	createTeamInput.Placeholder = "Team name..."
	createTeamInput.CharLimit = 64
	createTeamInput.Width = 40

	createChannelInput := textinput.New()
	createChannelInput.Placeholder = "Channel name..."
	createChannelInput.CharLimit = 64
	createChannelInput.Width = 40

	addMemberInput := textinput.New()
	addMemberInput.Placeholder = "Search by name..."
	addMemberInput.CharLimit = 64
	addMemberInput.Width = 40

	addChannelMemberInput := textinput.New()
	addChannelMemberInput.Placeholder = "Filter team members..."
	addChannelMemberInput.CharLimit = 64
	addChannelMemberInput.Width = 40

	createFolderInput := textinput.New()
	createFolderInput.Placeholder = "Folder name..."
	createFolderInput.CharLimit = 255
	createFolderInput.Width = 40

	searchInput := textinput.New()
	searchInput.Placeholder = "Search in chat..."
	searchInput.CharLimit = 64
	searchInput.Width = 40

	editInput := textinput.New()
	editInput.CharLimit = 4000
	editInput.Width = 60

	prefs := loadPrefs()

	m := Model{
		client:                  client,
		focusLeft:               true,
		focusList:               0,
		loading:                 true,
		input:                   ta,
		searchInput:             searchInput,
		editInput:               editInput,
		createFolderInput:       createFolderInput,
		newDMQuery:              newDMInput,
		createTeamInput:         createTeamInput,
		createChannelInput:      createChannelInput,
		createChannelType:       "Standard",
		addMemberInput:          addMemberInput,
		addChannelMemberInput:   addChannelMemberInput,
		assignFilter:            FilterUpcoming,
		prefs:                   prefs,
		notificationsRefreshing: true,
		chatUnread:              make(map[string]bool),
		presence:                make(map[string]string),
		selectedFiles:           make(map[int]bool),
		folderCache:             make(map[string][]graph.DriveItem),
		channelToTeam:           make(map[string]string),
		userName:                userName,
		debug:                   debug,
		presenceOptions:         []string{"Available", "Busy", "DoNotDisturb", "BeRightBack", "Away", "Reset (Automatic)"},
		reactionOptions:         []string{"like", "heart", "laugh", "surprised", "sad", "angry"},
	}
	loadPrefsIntoModel(&m, prefs)
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		checkTokensCmd(m.client),
		loadTeamsCmd(m.client),
		loadChatsCmd(m.client),
		loadMeCmd(m.client),
		refreshTickCmd(),
		initialPresenceTickCmd(),
		unreadSweepCmd(),
		filesRefreshTickCmd(),
		loadNotificationsCmd(m.client),
		checkUpdateCmd(),
	)
}

func (m Model) activeConversationID() string {
	if m.workspace == WorkspaceDMs {
		if m.selectedChat < len(m.chats) {
			return m.chats[m.selectedChat].ID
		}
		return ""
	}
	if m.selectedChan < len(m.channels) {
		return m.channels[m.selectedChan].ID
	}
	return ""
}
