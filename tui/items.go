package tui

import (
	"strings"
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

type historyItem struct {
	kind       itemKind
	text       string
	streaming  bool
	toolName   string
	toolInput  string
	toolResult string
	toolErr    string
}

// renderItem 渲染一条历史项；返回包含 ANSI 颜色码的字符串。
func renderItem(it historyItem) string {
	switch it.kind {
	case itemUser:
		return userPrefixStyle.Render("> ") + it.text
	case itemAssistant:
		prefix := assistantPrefixStyle.Render("◀ 助手")
		sep := systemStyle.Render(strings.Repeat("─", 40))
		text := it.text
		if it.streaming {
			text += cursorStyle.Render("▍")
		}
		return prefix + "\n" + sep + "\n" + text
	case itemThinking:
		return thinkingStyle.Render("∎ " + it.text)
	case itemToolCall:
		header := systemStyle.Render("调用工具: ") + toolNameStyle.Render(it.toolName)
		body := "$ " + it.toolInput
		return header + "\n" + boxStyle.Render(body)
	case itemToolResult:
		prefix := "→ "
		if it.toolErr != "" {
			prefix = errorStyle.Render("✗ "+it.toolErr) + "\n"
		}
		return boxStyle.Render(prefix + it.toolResult)
	case itemSystem:
		return systemStyle.Render(it.text)
	case itemError:
		return errorStyle.Render("✗ " + it.text)
	}
	return ""
}

// renderItems 把整个历史序列渲染为单一字符串，供 viewport 展示。
func renderItems(items []historyItem) string {
	if len(items) == 0 {
		return systemStyle.Render("(空) 输入消息后回车发送；Ctrl+C 退出")
	}
	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(renderItem(it))
	}
	return sb.String()
}