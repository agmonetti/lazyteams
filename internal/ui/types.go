package ui

import "teamsTUI/internal/graph"

type tickMsg struct{}
type presenceTickMsg struct{}
type messageSentMsg struct{}
type messageSendErrMsg struct{ err error }
type filesMsg struct {
	files    []graph.DriveItem
	folderID string
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
type tokenRenewedMsg struct{ 
	tokenType string
	err       error 
}

type tokenCheckDoneMsg struct {
	expired []string
}
type tokenRenewingMsg struct{}

type searchUsersMsg struct{ results []graph.UserSearchResult }
type searchUsersErrMsg struct{ err error }
type createDMMsg struct{ chat graph.Chat }
type createDMErrMsg struct{ err error }
type channelRootMsg struct {
	node FolderNode
	err  error
}

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
type createFolderDoneMsg struct{ item graph.DriveItem }
type deleteFileDoneMsg struct{}

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

type pollChatsMsg struct {
	chats []graph.Chat
}

type presenceTickResultMsg struct {
	presences map[string]string
}

type setPresenceMsg struct {
	err error
}

type downloadDoneMsg struct {
	results []string
}

type clearDownloadStatusMsg struct{ id int }

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

type reactionsLoadedMsg struct {
	messageID string
	reactions map[string]bool
	err       error
}

type previewResultMsg struct {
	content     string
	fileName    string
	err         error
	openBrowser bool
	status      string
}
