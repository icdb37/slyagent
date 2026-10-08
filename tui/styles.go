package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorCyan    = lipgloss.Color("39")
	colorGreen   = lipgloss.Color("42")
	colorYellow  = lipgloss.Color("220")
	colorGray    = lipgloss.Color("243")
	colorDim     = lipgloss.Color("240")
	colorRed     = lipgloss.Color("196")
	colorMagenta = lipgloss.Color("205")

	topBarStyle = lipgloss.NewStyle().
		Foreground(colorMagenta).
		Bold(true)

	statusBarStyle = lipgloss.NewStyle().
		Foreground(colorGray)

	userPrefixStyle = lipgloss.NewStyle().
		Foreground(colorCyan).
		Bold(true)

	assistantPrefixStyle = lipgloss.NewStyle().
		Foreground(colorGreen).
		Bold(true)

	systemStyle = lipgloss.NewStyle().
		Foreground(colorDim)

	errorStyle = lipgloss.NewStyle().
		Foreground(colorRed)

	toolNameStyle = lipgloss.NewStyle().
		Foreground(colorYellow)

	thinkingStyle = lipgloss.NewStyle().
		Foreground(colorGray).
		Italic(true)

	cursorStyle = lipgloss.NewStyle().
		Foreground(colorMagenta)

	inputPromptStyle = lipgloss.NewStyle().
		Foreground(colorMagenta).
		Bold(true)

	boxStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorDim).
		Padding(0, 1)

	authStyle = lipgloss.NewStyle().
		Foreground(colorYellow).
		Bold(true)
)
