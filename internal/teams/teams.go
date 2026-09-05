package teams

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/agmonetti/lazyteams/internal/graph"
)

var urlRegex = regexp.MustCompile(`https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*)`)

// AggregateChatAttachments collects all attachments (files and links) from
// messages already loaded in memory, deduplicated by URL, from newest to
// oldest. No network calls: operates 100% on in-memory messages.
func AggregateChatAttachments(messages []graph.Message) []graph.DriveItem {
	type entry struct {
		item    graph.DriveItem
		created time.Time
	}

	seen := make(map[string]bool)
	var entries []entry

	for _, msg := range messages {
		// 1. Extract files and links from the official Attachments array
		for _, att := range msg.Attachments {
			if att.URL == "" || seen[att.URL] {
				continue
			}
			seen[att.URL] = true
			entries = append(entries, entry{
				item: graph.DriveItem{
					Name:           att.Name,
					WebUrl:         att.URL,
					IsExternalLink: att.Type == "link",
				},
				created: msg.CreatedAt,
			})
		}

		// 2. Extract URLs pasted manually in the message body
		urls := urlRegex.FindAllString(msg.Body, -1)
		for _, u := range urls {
			if seen[u] {
				continue
			}
			seen[u] = true

			// Try to infer a friendly name
			name := u
			if parsed, err := url.Parse(u); err == nil {
				parts := strings.Split(parsed.Path, "/")
				if len(parts) > 0 && parts[len(parts)-1] != "" {
					name = parts[len(parts)-1]
				} else {
					name = parsed.Host
				}
			}

			entries = append(entries, entry{
				item: graph.DriveItem{
					Name:           name,
					WebUrl:         u,
					IsExternalLink: true,
				},
				created: msg.CreatedAt,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].created.After(entries[j].created)
	})

	items := make([]graph.DriveItem, len(entries))
	for i, e := range entries {
		items[i] = e.item
	}
	return items
}

// GetFileIcon returns the text icon for a file based on its extension.
func GetFileIcon(item graph.DriveItem) string {
	if item.IsExternalLink {
		return "[LINK]"
	}
	if item.Folder != nil {
		return "[DIR]"
	}
	name := strings.ToLower(item.Name)
	switch {
	case strings.HasSuffix(name, ".pdf"):
		return "[PDF]"
	case strings.HasSuffix(name, ".pptx") || strings.HasSuffix(name, ".ppt"):
		return "[PPT]"
	case strings.HasSuffix(name, ".docx") || strings.HasSuffix(name, ".doc"):
		return "[DOC]"
	case strings.HasSuffix(name, ".xlsx") || strings.HasSuffix(name, ".xls"):
		return "[XLS]"
	case strings.HasSuffix(name, ".mp4") || strings.HasSuffix(name, ".mkv"):
		return "[VID]"
	case strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".rar"):
		return "[ZIP]"
	default:
		return "[FILE]"
	}
}
