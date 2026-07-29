package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"teamsTUI/internal/graph"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var trailingSpaceWithAnsi = regexp.MustCompile(`(\x1b\[[0-9;]*m|[ \t\r])+$`)

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
	content = strings.ReplaceAll(content, "<p>", "")
	content = strings.ReplaceAll(content, "</p>", "")
	content = strings.ReplaceAll(content, "<br>", "\n")
	content = strings.ReplaceAll(content, "&nbsp;", " ")
	content = strings.ReplaceAll(content, "&amp;", "&")
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")

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
		clean := ansiToken.ReplaceAllString(sub[1], "")
		return mentionStyle.Render(clean)
	})
}

func renderMarkdown(content string, width int) string {
	if content == "" {
		return ""
	}
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
	out = highlightMentions(out)
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
			m.MessageType != "ThreadActivity/AddMember" &&
			m.MessageType != "ThreadActivity/DeleteMember" &&
			m.MessageType != "ThreadActivity/DeleteUser" {
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
		isSelf := msg.FromName == "User" || msg.FromName == selfName ||
			(selfID != "" && strings.HasSuffix(msg.FromUserID, selfID))

		if isSelf {
			tsRaw := metaStyle.Render(timeStr)
			timestamp := lipgloss.PlaceHorizontal(actualW-2, lipgloss.Right, tsRaw)

			var wrapped string
			if msg.Deleted {
				wrapped = systemEventStyle.Render("This message has been deleted.")
			} else {
				rawBody := strings.TrimSpace(body)

				if strings.Contains(rawBody, "[Attached Image]") {
					imgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
					var renderedLines []string
					for _, line := range strings.Split(rawBody, "\n") {
						line = strings.TrimSpace(line)
						if line == "[Attached Image]" {
							renderedLines = append(renderedLines, imgStyle.Render(line))
						} else if line != "" {
							renderedLines = append(renderedLines, line)
						}
					}
					wrapped = strings.Join(renderedLines, "\n")
				} else {
					maxW := actualW * 2 / 3
					optW := getOptimalWrapWidth(rawBody, maxW)
					wrapped = renderMarkdown(rawBody, optW)
					var cleanLines []string
					for _, l := range strings.Split(wrapped, "\n") {
						cleanLines = append(cleanLines, trailingSpaceWithAnsi.ReplaceAllString(l, ""))
					}
					wrapped = strings.Join(cleanLines, "\n")
				}
			}

			// Align each line independently to the right
			var placedLines []string
			for _, l := range strings.Split(wrapped, "\n") {
				if strings.TrimSpace(l) == "" {
					continue
				}
				placedLines = append(placedLines, "  "+lipgloss.PlaceHorizontal(actualW-2, lipgloss.Right, l))
			}
			placedBlock := strings.Join(placedLines, "\n")

			content += fmt.Sprintf("%s%s\n%s\n", cursorStr, timestamp, placedBlock)
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
				if strings.Contains(rawBody, "[Attached Image]") {
					imgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
					var renderedLines []string
					for _, line := range strings.Split(rawBody, "\n") {
						line = strings.TrimSpace(line)
						if line == "[Attached Image]" {
							renderedLines = append(renderedLines, imgStyle.Render(line))
						} else if line != "" {
							renderedLines = append(renderedLines, line)
						}
					}
					wrapped = strings.Join(renderedLines, "\n")
				} else {
					wrapped = renderMarkdown(rawBody, actualW)
				}
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
