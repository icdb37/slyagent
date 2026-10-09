package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type itemKind int

const (
	itemUser itemKind = iota
	itemAssistant
	itemThinking
	itemToolCall
	itemToolResult
	itemSystem
	itemError
)

const (
	// minWrapWidth 是换行宽度的下限，避免窗口过窄时算出 <=0 的宽度。
	minWrapWidth = 8
	// boxOverhead 是 boxStyle 的左右边框(2) + 左右内边距(2) 占用的宽度。
	boxOverhead = 4
)

type historyItem struct {
	kind       itemKind
	text       string
	streaming  bool
	toolName   string
	toolInput  string
	toolResult string
	toolErr    string
}

// wrapText 按显示宽度换行：保留 ANSI 颜色码，中文等宽字符按 2 列计算，
// 超长单词（长 URL、路径）会硬折行，绝不超出 width 列。
func wrapText(s string, width int) string {
	if width < minWrapWidth {
		width = minWrapWidth
	}
	return ansi.Wrap(s, width, "-")
}

// wrapPrefixed 把 prefix 放在首行，续行用等宽空格缩进对齐。
func wrapPrefixed(prefix, text string, width int) string {
	pw := ansi.StringWidth(prefix)
	lines := strings.Split(wrapText(text, width-pw), "\n")
	indent := strings.Repeat(" ", pw)
	for i, l := range lines {
		if i == 0 {
			lines[i] = prefix + l
		} else {
			lines[i] = indent + l
		}
	}
	return strings.Join(lines, "\n")
}

// stripANSI 去掉 ANSI 转义序列，得到可放进剪贴板的纯文本。
func stripANSI(s string) string {
	return ansi.Strip(s)
}

// renderItem 渲染一条历史项；width 是可用显示宽度（终端列数），
// 返回包含 ANSI 颜色码、且每行不超过 width 列的字符串。
func renderItem(it historyItem, width int) string {
	if width < minWrapWidth {
		width = minWrapWidth
	}
	switch it.kind {
	case itemUser:
		return wrapPrefixed(userPrefixStyle.Render("> "), it.text, width)
	case itemAssistant:
		prefix := assistantPrefixStyle.Render("◀ 助手")
		sep := systemStyle.Render(strings.Repeat("─", min(width, 40)))
		text := wrapText(it.text, width)
		if it.streaming {
			text += cursorStyle.Render("▍")
		}
		return prefix + "\n" + sep + "\n" + text
	case itemThinking:
		return thinkingStyle.Render(wrapPrefixed("∎ ", it.text, width))
	case itemToolCall:
		header := systemStyle.Render("调用工具: ") + toolNameStyle.Render(it.toolName)
		body := wrapPrefixed("$ ", it.toolInput, width-boxOverhead)
		return header + "\n" + boxStyle.Render(body)
	case itemToolResult:
		var sb strings.Builder
		if it.toolErr != "" {
			sb.WriteString(errorStyle.Render("✗ "+wrapText(it.toolErr, width-boxOverhead)) + "\n")
		}
		sb.WriteString(wrapPrefixed("→ ", it.toolResult, width-boxOverhead))
		return boxStyle.Render(sb.String())
	case itemSystem:
		return systemStyle.Render(wrapText(it.text, width))
	case itemError:
		return errorStyle.Render(wrapPrefixed("✗ ", it.text, width))
	}
	return ""
}

// renderItems 把整个历史序列渲染为单一字符串，供 viewport 展示。
// 按 width 逐项换行，保证长行不会横向溢出。
func renderItems(items []historyItem, width int) string {
	if len(items) == 0 {
		return systemStyle.Render("(空) 输入消息后回车发送；Ctrl+C 退出")
	}
	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(renderItem(it, width))
	}
	return sb.String()
}

// lastAssistantText 返回最后一条非空助手回复的纯文本，供复制使用。
func lastAssistantText(items []historyItem) string {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].kind == itemAssistant && strings.TrimSpace(items[i].text) != "" {
			return stripANSI(items[i].text)
		}
	}
	return ""
}
