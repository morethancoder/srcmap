package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/morethancoder/srcmap/internal/config"
	"github.com/morethancoder/srcmap/internal/mcp"
)

// Chat is the interactive terminal chat interface.
type Chat struct {
	Loop       *ToolLoop
	Input      io.Reader
	Output     io.Writer
	ConfigPath string
	renderer   *glamour.TermRenderer
}

// NewChat creates a new chat interface.
func NewChat(client *OpenRouterClient, handler *mcp.ToolHandler, modelID string, costTracker *CostTracker) *Chat {
	loop := NewToolLoop(client, handler, modelID, costTracker)
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	return &Chat{
		Loop:     loop,
		Input:    os.Stdin,
		Output:   os.Stdout,
		renderer: renderer,
	}
}

// Run starts the interactive chat loop.
func (c *Chat) Run(ctx context.Context) error {
	fmt.Fprint(c.Output, RenderBanner(c.Loop.ModelID))

	scanner := bufio.NewScanner(c.Input)
	for {
		fmt.Fprint(c.Output, RenderPrompt())
		if !scanner.Scan() {
			fmt.Fprintln(c.Output)
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			if c.handleSlashCommand(ctx, input, scanner) {
				continue
			}
		}

		c.streamResponse(ctx, input)
	}

	return scanner.Err()
}

func (c *Chat) streamResponse(ctx context.Context, input string) {
	fmt.Fprintln(c.Output)

	var (
		hasOutput   bool
		hasThinking bool
		fullText    strings.Builder
	)

	finalText, err := c.Loop.SendMessage(ctx, input, func(ev StreamEvent) {
		switch ev.Type {
		case "thinking":
			if !hasThinking {
				fmt.Fprintln(c.Output, RenderThinkingStart())
				hasThinking = true
			}
			fmt.Fprint(c.Output, "  "+thinkingStyle.Render(ev.Delta))

		case "text":
			if hasThinking && !hasOutput {
				fmt.Fprintln(c.Output)
				fmt.Fprintln(c.Output, RenderThinkingEnd())
				hasThinking = false
			}
			hasOutput = true
			fullText.WriteString(ev.Delta)

		case "tool_call":
			if hasThinking {
				fmt.Fprintln(c.Output)
				fmt.Fprintln(c.Output, RenderThinkingEnd())
				hasThinking = false
			}
			fmt.Fprint(c.Output, RenderToolCallStart(ev.ToolName, ev.ToolArgs))

		case "tool_result":
			fmt.Fprint(c.Output, RenderToolResult(ev.ToolResult))

		case "done":
			if hasThinking {
				fmt.Fprintln(c.Output)
				fmt.Fprintln(c.Output, RenderThinkingEnd())
			}
		}
	})

	if err != nil {
		fmt.Fprintln(c.Output, RenderError(err.Error()))
		fmt.Fprintln(c.Output)
		return
	}

	// Render the final text response with glamour markdown
	text := finalText
	if text == "" {
		text = fullText.String()
	}
	if text != "" {
		if c.renderer != nil {
			if rendered, err := c.renderer.Render(text); err == nil {
				fmt.Fprint(c.Output, rendered)
			} else {
				fmt.Fprintln(c.Output, "  "+text)
			}
		} else {
			fmt.Fprintln(c.Output, "  "+text)
		}
	}

	fmt.Fprintln(c.Output, RenderCostFooter(
		c.Loop.CostTracker.LastResponse(),
		c.Loop.CostTracker.Session(),
	))
	fmt.Fprintln(c.Output)
}

// --- Slash commands ---

func (c *Chat) handleSlashCommand(ctx context.Context, input string, scanner *bufio.Scanner) bool {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch {
	case cmd == "/clear":
		c.Loop.ClearHistory()
		fmt.Fprintln(c.Output, RenderSuccess("Chat history and session cost cleared."))
		return true

	case cmd == "/model" && len(parts) > 1:
		newModel := parts[1]
		c.setModel(newModel)
		fmt.Fprintln(c.Output, RenderSuccess("Switched to "+modelStyle.Render(newModel)))
		return true

	case cmd == "/model":
		c.pickModel(ctx, scanner)
		return true

	case cmd == "/sources":
		c.listSources(len(parts) > 1 && parts[1] == "--global")
		return true

	case cmd == "/cost":
		fmt.Fprintln(c.Output, RenderCostFooter(
			c.Loop.CostTracker.LastResponse(),
			c.Loop.CostTracker.Session(),
		))
		return true

	case cmd == "/help":
		fmt.Fprint(c.Output, RenderHelp())
		return true
	}
	return false
}

func (c *Chat) pickModel(ctx context.Context, scanner *bufio.Scanner) {
	type fetchResult struct {
		models []Model
		err    error
	}
	ch := make(chan fetchResult, 1)

	go func() {
		models, err := c.Loop.Client.ListModels(ctx, 16000)
		ch <- fetchResult{models, err}
	}()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7ee8fa"))

	sm := &spinnerModel{spinner: s, label: "fetching models..."}
	p := tea.NewProgram(sm, tea.WithOutput(c.Output))

	go func() {
		res := <-ch
		var respStr string
		if res.err != nil {
			respStr = res.err.Error()
		} else {
			data, _ := json.Marshal(res.models)
			respStr = string(data)
		}
		p.Send(spinnerDoneMsg{response: respStr, err: res.err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintln(c.Output, RenderError("Could not fetch models: "+err.Error()))
		return
	}

	final := finalModel.(*spinnerModel)
	if final.result.err != nil {
		fmt.Fprintln(c.Output, RenderError("Could not fetch models: "+final.result.err.Error()))
		return
	}

	var models []Model
	json.Unmarshal([]byte(final.result.response), &models)

	if len(models) == 0 {
		fmt.Fprintln(c.Output, RenderInfo("No models available."))
		return
	}

	limit := 20
	if len(models) < limit {
		limit = len(models)
	}

	fmt.Fprint(c.Output, RenderModelList(models, c.Loop.ModelID, limit))
	fmt.Fprintf(c.Output, "  %s", infoStyle.Render("Enter number or model ID (empty to cancel): "))

	if !scanner.Scan() {
		return
	}

	choice := strings.TrimSpace(scanner.Text())
	if choice == "" {
		return
	}

	if n, err := strconv.Atoi(choice); err == nil && n >= 1 && n <= limit {
		selected := models[n-1]
		c.setModel(selected.ID)
		c.Loop.CostTracker.SetPricing(selected.InputPrice, selected.OutputPrice)
		fmt.Fprintln(c.Output, RenderSuccess("Switched to "+modelStyle.Render(selected.ID)))
		return
	}

	for _, m := range models {
		if m.ID == choice {
			c.Loop.CostTracker.SetPricing(m.InputPrice, m.OutputPrice)
			break
		}
	}
	c.setModel(choice)
	fmt.Fprintln(c.Output, RenderSuccess("Switched to "+modelStyle.Render(choice)))
}

func (c *Chat) listSources(globalOnly bool) {
	sources, err := c.Loop.ToolHandler.DB.ListSources(globalOnly)
	if err != nil {
		fmt.Fprintln(c.Output, RenderError("failed to list sources: "+err.Error()))
		return
	}

	if len(sources) == 0 {
		fmt.Fprintln(c.Output, dimStyle.Render("  No sources indexed yet. Use the agent to fetch packages."))
		return
	}

	fmt.Fprintln(c.Output)
	for _, s := range sources {
		scope := dimStyle.Render("local")
		if s.Global {
			scope = infoStyle.Render("global")
		}
		ver := s.Version
		if ver == "" {
			ver = "-"
		}
		name := toolNameStyle.Render(fmt.Sprintf("%-20s", s.Name))
		version := modelStyle.Render(ver)
		stats := dimStyle.Render(fmt.Sprintf("  symbols:%-4d sections:%-3d concepts:%-3d", s.MethodCount, s.SectionCount, s.ConceptCount))
		fmt.Fprintf(c.Output, "  %s %s %s%s\n", name, version, scope, stats)
	}
	fmt.Fprintln(c.Output)
}

func (c *Chat) setModel(modelID string) {
	c.Loop.SetModel(modelID)

	if c.ConfigPath == "" {
		return
	}
	cfg, _ := config.Load(c.ConfigPath)
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.Model = modelID
	if err := cfg.Save(c.ConfigPath); err != nil {
		fmt.Fprintln(c.Output, RenderError("Could not save model to config: "+err.Error()))
	}
}

// --- Bubble Tea spinner (model picker only) ---

type spinnerDoneMsg struct {
	response string
	err      error
}

type spinnerModel struct {
	spinner spinner.Model
	label   string
	done    bool
	result  spinnerDoneMsg
}

func (m *spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m *spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerDoneMsg:
		m.done = true
		m.result = msg
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *spinnerModel) View() string {
	if m.done {
		return ""
	}
	return fmt.Sprintf("  %s %s", m.spinner.View(), dimStyle.Render(m.label))
}
