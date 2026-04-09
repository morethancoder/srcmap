package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/morethancoder/srcmap/internal/mcp"
)

// Chat is the interactive terminal chat interface.
type Chat struct {
	Loop   *ToolLoop
	Input  io.Reader
	Output io.Writer
}

// NewChat creates a new chat interface.
func NewChat(client *OpenRouterClient, handler *mcp.ToolHandler, modelID string, costTracker *CostTracker) *Chat {
	loop := NewToolLoop(client, handler, modelID, costTracker)
	return &Chat{
		Loop:   loop,
		Input:  os.Stdin,
		Output: os.Stdout,
	}
}

// Run starts the interactive chat loop.
func (c *Chat) Run(ctx context.Context) error {
	fmt.Fprintf(c.Output, "srcmap agent (model: %s)\n", c.Loop.ModelID)
	fmt.Fprintf(c.Output, "Type /help for commands, /clear to reset, Ctrl+C to exit.\n\n")

	scanner := bufio.NewScanner(c.Input)
	for {
		fmt.Fprint(c.Output, "> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle slash commands
		if strings.HasPrefix(input, "/") {
			if c.handleSlashCommand(input) {
				continue
			}
		}

		response, err := c.Loop.SendMessage(ctx, input)
		if err != nil {
			fmt.Fprintf(c.Output, "Error: %v\n\n", err)
			continue
		}

		fmt.Fprintf(c.Output, "\n%s\n\n", response)
		fmt.Fprintln(c.Output, c.Loop.CostTracker.FormatFooter())
		fmt.Fprintln(c.Output)
	}

	return scanner.Err()
}

// handleSlashCommand processes UI-intercepted slash commands.
// Returns true if the command was handled.
func (c *Chat) handleSlashCommand(input string) bool {
	switch {
	case input == "/clear":
		c.Loop.ClearHistory()
		fmt.Fprintln(c.Output, "Chat history and session cost cleared.")
		return true

	case input == "/model":
		fmt.Fprintf(c.Output, "Current model: %s\n", c.Loop.ModelID)
		return true

	case input == "/sources":
		fmt.Fprintln(c.Output, "Use 'srcmap sources' CLI command to list sources.")
		return true

	case input == "/cost":
		fmt.Fprintln(c.Output, c.Loop.CostTracker.FormatFooter())
		return true

	case input == "/help":
		fmt.Fprintln(c.Output, "Commands:")
		fmt.Fprintln(c.Output, "  /clear   — Clear chat history and reset cost")
		fmt.Fprintln(c.Output, "  /model   — Show current model")
		fmt.Fprintln(c.Output, "  /sources — List loaded sources")
		fmt.Fprintln(c.Output, "  /cost    — Show session cost")
		fmt.Fprintln(c.Output, "  /help    — Show this help")
		return true
	}
	return false
}
