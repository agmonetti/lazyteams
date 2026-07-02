package ui

import (
	"teamsTUI/internal/graph"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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
)

type ActivityFilter int

const (
	FilterAll      ActivityFilter = iota
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
	editingDownloadDir  bool            // true while editing the destination folder
	downloadDirInput    textinput.Model // input for editing the download path

	// channelID → teamID map for navigation from notifications
	channelToTeam map[string]string

	// Input for sending messages
	input    textinput.Model
	isTyping bool

	// User
	userName string

	// Presence (selection)
	showPresenceMenu bool
	presenceCursor   int
	presenceOptions  []string
	presenceError    string

	// Sliding window for channels
	channelWindowStart int

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
}

func New(client *graph.Client, userName string) Model {
	ti := textinput.New()
	ti.Placeholder = "Press 'i' to type a message..."
	ti.CharLimit = 1000
	ti.Width = 50

	dirInput := textinput.New()
	dirInput.Placeholder = "~/Downloads"
	dirInput.CharLimit = 256

	return Model{
		client:       client,
		focusLeft:    true,
		focusList:    0,
		loading:      true,
		input:        ti,
		downloadDirInput: dirInput,
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
	return tea.Batch(loadTeamsCmd(m.client), loadMeCmd(m.client), refreshTickCmd(), initialPresenceTickCmd())
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
