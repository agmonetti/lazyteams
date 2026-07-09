package ui

import (
	"teamsTUI/internal/graph"
	"teamsTUI/internal/ui/components/directorypicker"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type errMsg struct{ err error }
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
	messages []graph.Message
}

type notificationsMsg struct {
	items []graph.NotificationItem
}
type notificationsErrMsg struct {
	err error
}

type assignmentsMsg struct {
	items []graph.Assignment
}
type assignmentsErrMsg struct {
	err error
}

type ViewMode int

const (
	ModeChat ViewMode = iota
	ModeFiles
	ModeInfo
)

type ActivityFilter int

const (
	FilterAll      ActivityFilter = iota
	FilterUnread                   // Not read
	FilterUpcoming                 // Upcoming (due in ≤7 days)
	FilterOverdue                  // Overdue
	FilterCompleted                // Completed/Submitted
)

type Workspace int

const (
	WorkspaceTeams       Workspace = iota
	WorkspaceDMs
	WorkspaceActivity
	WorkspaceAssignments
)

type FolderNode struct {
	ID      string
	Name    string
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
	leftVp      viewport.Model
	viewport    viewport.Model
	ready       bool
	channelErr  error
	folderStack []FolderNode // Stack of nodes for navigating back in subfolders and building the path

	// File downloads
	currentFilesDriveID string          // "" = default team drive; otherwise explicit drive ID
	selectedFiles       map[int]bool    // indices marked for multi-download
	confirmingDownload  bool            // true while the confirmation popup is showing
	downloadTargets     []graph.DriveItem
	downloadStatus      string          // result message
	downloadStatusID    int             // increments with each download, prevents an old clear from erasing a new status
	downloading         bool            // true while downloading
	folderCache         map[string][]graph.DriveItem // folder cache: folderID → contents

	// channelID → teamID map for navigation from notifications
	channelToTeam map[string]string

	// teamThreadID is the threadId of the current team's General channel
	teamThreadID string

	// Thread view
	messageCursor    int
	cursorMode       bool
	showThread       bool
	threadParentID   string
	threadParentMsg  graph.Message
	threadViewport   viewport.Model
	isReplyTyping    bool

	// Input for sending messages
	input    textarea.Model
	isTyping bool

	// Search in chat
	isSearching bool
	searchInput textinput.Model
	searchQuery string

	// Token renewal
	tokenRenewing bool
	tokenRenewErr string

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
	notifications      []graph.NotificationItem
	notifLoaded        bool
	selectedNotif      int
	notifErr           error
	activityFilter     ActivityFilter


	// Assignments / Tasks
	assignments      []graph.Assignment
	assignLoaded     bool
	selectedAssign   int
	assignErr        error
	assignFilter     ActivityFilter

	// DM polling
	chatUnread map[string]bool  // chatID → has unread messages
	presence   map[string]string // userID → Availability (Available, Busy, Away, Offline, etc.)

	// File preview
	previewing     bool   // true while showing file preview
	previewContent string // file content to display
	previewFileName string // name of the file being previewed

	// New DM creation
	showNewDMPopup    bool
	newDMQuery        textinput.Model
	newDMResults      []graph.UserSearchResult
	newDMCursor       int
	newDMErr          string

	// File upload
	uploading        bool

	// Directory picker
	showDirPicker   bool
	dirPicker       directorypicker.Model
	pickerPurpose   string // "download" or "upload"

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

	// Delete confirmations
	showDeleteChannelPopup bool
	showDeleteTeamPopup    bool

	// Info panels
	showTeamInfo    bool
	teamInfo        *graph.Team
	channelInfo     *graph.Channel

	// Team members
	showMembersPopup  bool
	teamMembers       []graph.TeamMember
	membersLoading    bool
	membersCursor     int
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

	// Channel members (separate from team members)
	channelMembers []graph.TeamMember
}

func New(client *graph.Client, userName string) Model {
	ta := textarea.New()
	ta.Placeholder = "Press 'i' to type a message..."
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

	searchInput := textinput.New()
	searchInput.Placeholder = "Search in chat..."
	searchInput.CharLimit = 64
	searchInput.Width = 40

	return Model{
		client:       client,
		focusLeft:    true,
		focusList:    0,
		loading:      true,
		input:        ta,
		searchInput:  searchInput,
		newDMQuery:   newDMInput,
		createTeamInput: createTeamInput,
		createChannelInput: createChannelInput,
		createChannelType: "Standard",
		addMemberInput: addMemberInput,
		addChannelMemberInput: addChannelMemberInput,
		prefs:        loadPrefs(),
		chatUnread:   make(map[string]bool),
		presence:     make(map[string]string),
		selectedFiles: make(map[int]bool),
		folderCache:  make(map[string][]graph.DriveItem),
		channelToTeam: make(map[string]string),
		userName:     userName,
		presenceOptions: []string{"Available", "Busy", "DoNotDisturb", "BeRightBack", "Away", "Reset (Automatic)"},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadTeamsCmd(m.client), loadMeCmd(m.client), refreshTickCmd(), initialPresenceTickCmd(), loadNotificationsCmd(m.client))
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
