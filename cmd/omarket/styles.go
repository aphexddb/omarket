package main

import "github.com/charmbracelet/lipgloss"

// Tokyo Night-ish palette (SPEC §4).
var (
	colorBg     = lipgloss.Color("#1a1b26")
	colorFg     = lipgloss.Color("#c0caf5")
	colorAccent = lipgloss.Color("#7aa2f7")
	colorGreen  = lipgloss.Color("#9ece6a")
	colorMuted  = lipgloss.Color("#565f89")
	colorRed    = lipgloss.Color("#f7768e")

	docStyle      = lipgloss.NewStyle().Margin(1, 2)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	labelStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	helpStyle     = lipgloss.NewStyle().Foreground(colorMuted)
	mutedStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	successStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	checkoutStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	errorStyle    = lipgloss.NewStyle().Foreground(colorRed)
	fgStyle       = lipgloss.NewStyle().Foreground(colorFg)
	ownedBadge    = lipgloss.NewStyle().Bold(true).Foreground(colorBg).Background(colorGreen).Padding(0, 1)
)
