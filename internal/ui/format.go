package ui

import (
	"fmt"
	"lazyteams/internal/graph"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
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
		return "@" + strings.TrimPrefix(sub[1], "@")
	})

	return strings.TrimSpace(content)
}

var mentionToken = regexp.MustCompile(`\x1E(.*?)\x1F`)
var searchToken = regexp.MustCompile(`\x11(.*?)\x12`)
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

// highlightSearchMatches wraps search matches (already delimited by the \x11/\x12
// sentinels injected by filterMessages) in brackets with an underline style.
func highlightSearchMatches(text string) string {
	searchStyle := lipgloss.NewStyle().Underline(true).Foreground(lipgloss.Color("220"))
	return searchToken.ReplaceAllStringFunc(text, func(m string) string {
		sub := searchToken.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		clean := ansiToken.ReplaceAllString(sub[1], "")
		return searchStyle.Render("[" + clean + "]")
	})
}

func renderMarkdown(content string, width int) string {
	if content == "" {
		return ""
	}
	// Render with glamour without wrapping. Glamour's own word-wrap breaks long
	// URLs at arbitrary characters (notably '.') and collapses soft line breaks
	// into spaces, both of which damage the message. We wrap the output
	// ourselves in wrapAnsi, which breaks long words at URL-safe points.
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(0),
	)
	if err != nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	out = highlightMentions(out)
	out = wrapAnsi(out, width)
	return strings.TrimSpace(out)
}

// urlWrapBreakChars are the characters after which we prefer to wrap long words
// (URLs). They are deliberately the "natural" break points of a URL; '.' and
// '_' are excluded so a URL never looks cut right after a dot or underscore.
const urlWrapBreakChars = "/?=&-"

func isUrlWrapBreakChar(r rune) bool {
	if r == ' ' {
		return true
	}
	return strings.ContainsRune(urlWrapBreakChars, r)
}

// styledChar is a single printable character together with the ANSI SGR
// sequences that must be emitted before it to reproduce the original styling.
type styledChar struct {
	r     rune
	style string
}

// flattenAnsi splits an ANSI-styled line into printable characters, each tagged
// with the escape sequence active at its position. Consecutive escape
// sequences accumulate so that re-emitting a tag reproduces the styling.
func flattenAnsi(line string) []styledChar {
	var out []styledChar
	var pending []rune
	style := ""
	flush := func() {
		for _, r := range pending {
			out = append(out, styledChar{r: r, style: style})
		}
		pending = pending[:0]
	}
	for i := 0; i < len(line); {
		if line[i] == '\x1b' {
			if i+1 < len(line) && line[i+1] == '[' {
				k := i + 2
				for k < len(line) && (line[k] < 0x40 || line[k] > 0x7e) {
					k++
				}
				if k < len(line) {
					flush()
					style += line[i : k+1]
					i = k + 1
					continue
				}
			}
			r, size := utf8.DecodeRuneInString(line[i:])
			pending = append(pending, r)
			i += size
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		pending = append(pending, r)
		i += size
	}
	flush()
	return out
}

// emitStyledChars writes the characters to out, emitting the appropriate SGR
// sequence whenever the styling changes, and closes with a reset if the last
// character was styled so the following newline does not leak styling.
func emitStyledChars(out *strings.Builder, chars []styledChar) {
	last := ""
	for _, c := range chars {
		if c.style != last {
			out.WriteString(c.style)
			last = c.style
		}
		out.WriteRune(c.r)
	}
	if last != "" {
		out.WriteString("\x1b[0m")
	}
}

func runeDisplayWidth(r rune) int {
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 0
	}
	return w
}

// wrapAnsi rewraps ANSI-styled content so that no line is wider than width.
// Long words (URLs) are broken at urlWrapBreakChars instead of mid-token; if a
// word has no break point within the limit it is hard-wrapped. Styling is
// preserved across the break by re-emitting the active escape sequence.
func wrapAnsi(content string, width int) string {
	if width <= 0 || content == "" {
		return content
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	var b strings.Builder
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(wrapAnsiLine(line, width))
	}
	return b.String()
}

func wrapAnsiLine(line string, width int) string {
	chars := flattenAnsi(line)
	if len(chars) == 0 {
		return line
	}
	var out strings.Builder
	start := 0
	for start < len(chars) {
		w := 0
		j := start
		lastBreak := -1
		for j < len(chars) {
			cw := runeDisplayWidth(chars[j].r)
			if w+cw > width {
				break
			}
			w += cw
			if isUrlWrapBreakChar(chars[j].r) {
				lastBreak = j
			}
			j++
		}
		if j >= len(chars) {
			emitStyledChars(&out, chars[start:])
			break
		}
		if j == start {
			// A single character is wider than the limit; force progress.
			j = start + 1
		}
		if lastBreak != -1 {
			emitStyledChars(&out, chars[start:lastBreak+1])
			out.WriteString("\n")
			start = lastBreak + 1
		} else {
			emitStyledChars(&out, chars[start:j])
			out.WriteString("\n")
			start = j
		}
	}
	return out.String()
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

func formatMessagesWithCursor(messages []graph.Message, width, cursor int, cursorMode bool, searchCursor int) string {
	var content string
	roots := rootMessages(messages)

	actualW := width - 4
	if actualW < 10 {
		actualW = 10
	}

	for i := len(roots) - 1; i >= 0; i-- {
		msg := roots[i]
		cursorStr := "  "
		if searchCursor >= 0 && searchCursor < len(messages) && msg.ID == messages[searchCursor].ID {
			cursorStr = symCursor
		} else if cursorMode && i == cursor {
			cursorStr = symCursor
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
			replyStr := fmt.Sprintf("  %s %d repl", symReply, count)
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
	content = highlightSearchMatches(content)
	return content
}

func formatMessagesDM(messages []graph.Message, width int, selfName, selfID string, cursor int, cursorMode bool, searchCursor int) string {
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
		if searchCursor >= 0 && searchCursor < len(messages) && msg.ID == messages[searchCursor].ID {
			cursorStr = symCursor
		} else if cursorMode && i == cursor {
			cursorStr = symCursor
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
	content = highlightSearchMatches(content)
	return content
}
