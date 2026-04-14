package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7ee8fa"))

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	promptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#50fa7b"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5555"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50fa7b"))

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8be9fd"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6272a4"))

	modelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f1fa8c"))

	toolNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffb86c"))

	toolArgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6272a4"))

	thinkingStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("#6272a4"))

	costStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50fa7b"))

	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#44475a"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50fa7b")).
			Bold(true)

	numberStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#bd93f9"))
)

func separator() string {
	return separatorStyle.Render(strings.Repeat("─", 52))
}

// RenderBanner returns the styled welcome banner.
func RenderBanner(modelID string) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + titleStyle.Render("srcmap") + dimStyle.Render(" agent") + "\n")
	b.WriteString("  " + dimStyle.Render("model ") + modelStyle.Render(modelID) + "\n")
	b.WriteString("  " + separator() + "\n")
	b.WriteString("  " + dimStyle.Render("/help for commands · /model to switch · ctrl+c to exit") + "\n\n")
	return b.String()
}

// RenderPrompt returns the styled input prompt.
func RenderPrompt() string {
	return promptStyle.Render("❯ ")
}

// RenderError styles an error message.
func RenderError(msg string) string {
	return errorStyle.Render("  ✗ " + msg)
}

// RenderSuccess styles a success message.
func RenderSuccess(msg string) string {
	return successStyle.Render("  ✓ ") + msg
}

// RenderInfo styles an info message.
func RenderInfo(msg string) string {
	return infoStyle.Render("  ℹ ") + msg
}

// RenderThinkingStart renders the start of a thinking block.
func RenderThinkingStart() string {
	return dimStyle.Render("  ── thinking ──────────────────────────────────────")
}

// RenderThinkingEnd renders the end of a thinking block.
func RenderThinkingEnd() string {
	return dimStyle.Render("  ─────────────────────────────────────────────────")
}

// RenderToolCallStart renders a tool call with its arguments inline.
func RenderToolCallStart(name string, argsJSON string) string {
	var b strings.Builder

	b.WriteString("  " + toolNameStyle.Render("▸ "+name))

	if argsJSON != "" && argsJSON != "{}" {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(argsJSON), &args); err == nil {
			var parts []string
			for k, v := range args {
				val := fmt.Sprintf("%v", v)
				if len(val) > 200 {
					val = val[:197] + "..."
				}
				parts = append(parts, toolArgStyle.Render(k+"=")+dimStyle.Render(val))
			}
			if len(parts) > 0 {
				b.WriteString("\n    " + strings.Join(parts, "\n    "))
			}
		}
	}

	b.WriteString("\n")
	return b.String()
}

// RenderToolResult renders the result of a tool call.
func RenderToolResult(result string) string {
	trimmed := strings.TrimSpace(result)
	lines := strings.Split(trimmed, "\n")
	const maxLines = 30
	const maxWidth = 200
	truncated := 0
	if len(lines) > maxLines {
		truncated = len(lines) - maxLines
		lines = lines[:maxLines]
	}

	var b strings.Builder
	b.WriteString("    " + dimStyle.Render("└─ result ("+fmt.Sprintf("%d", len(trimmed))+" bytes)") + "\n")
	for _, line := range lines {
		if len(line) > maxWidth {
			line = line[:maxWidth-3] + "..."
		}
		b.WriteString("    " + dimStyle.Render(line) + "\n")
	}
	if truncated > 0 {
		b.WriteString("    " + dimStyle.Render(fmt.Sprintf("… %d more lines", truncated)) + "\n")
	}
	return b.String()
}

// RenderCostFooter returns a styled cost footer.
func RenderCostFooter(last, session CostInfo) string {
	return fmt.Sprintf("  %s%d in %s%d out  %s  %s%d tokens %s",
		dimStyle.Render("↑"), last.InputTokens,
		dimStyle.Render("↓"), last.OutputTokens,
		costStyle.Render(fmt.Sprintf("$%.4f", last.CostUSD)),
		dimStyle.Render("· session "),
		session.InputTokens+session.OutputTokens,
		dimStyle.Render(fmt.Sprintf("$%.4f", session.CostUSD)),
	)
}

// RenderHelp returns the styled help text.
func RenderHelp() string {
	var b strings.Builder
	b.WriteString("\n")

	cmds := []struct{ cmd, desc string }{
		{"/sources", "List indexed sources (local)"},
		{"/sources --global", "List global sources only"},
		{"/model", "Switch model (interactive picker)"},
		{"/model <id>", "Switch to a specific model"},
		{"/clear", "Clear chat history and reset cost"},
		{"/cost", "Show session cost breakdown"},
		{"/help", "Show this help"},
	}

	for _, c := range cmds {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			modelStyle.Render(fmt.Sprintf("%-16s", c.cmd)),
			dimStyle.Render(c.desc),
		))
	}
	b.WriteString("\n")
	return b.String()
}

// RenderModelList returns the styled model picker list.
func RenderModelList(models []Model, currentModel string, limit int) string {
	var b strings.Builder
	b.WriteString("\n")

	for i, m := range models[:limit] {
		num := numberStyle.Render(fmt.Sprintf("  %2d ", i+1))
		name := m.ID
		ctx := dimStyle.Render(fmt.Sprintf(" %dk", m.ContextLength/1000))
		price := dimStyle.Render(fmt.Sprintf(" $%.2f/$%.2f", m.InputPrice, m.OutputPrice))

		marker := " "
		if m.ID == currentModel {
			marker = selectedStyle.Render("●")
			name = selectedStyle.Render(name)
		}

		b.WriteString(fmt.Sprintf("%s%s %s%s%s\n", num, marker, name, ctx, price))
	}

	b.WriteString("\n")
	return b.String()
}
