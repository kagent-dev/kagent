package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kagent-dev/kagent/go/cli/internal/tui/theme"
	jsonutil "github.com/kagent-dev/kagent/go/cli/internal/tui/util"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// SendMessageFn abstracts the A2A client's StreamMessage method for easier testing.
type SendMessageFn func(ctx context.Context, params protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error)

// RunChat starts the TUI chat, blocking until the user exits.
func RunChat(agentRef string, sessionID string, sendFn SendMessageFn, verbose bool) error {
	model := newChatModel(agentRef, sessionID, sendFn, verbose)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type a2aEventMsg struct {
	Event protocol.StreamingMessageEvent
}

type streamDoneMsg struct{}

type submitTaskMsg struct {
	Text string
}

type chatModel struct {
	agentRef  string
	sessionID string
	verbose   bool

	vp      viewport.Model
	input   textarea.Model
	history string

	working    bool
	workStart  time.Time
	statusText string

	spin spinner.Model

	send      SendMessageFn
	streamCh  <-chan protocol.StreamingMessageEvent
	cancel    context.CancelFunc
	streaming bool

	showInput bool
}

func newChatModel(agentRef string, sessionID string, send SendMessageFn, verbose bool) *chatModel {
	input := textarea.New()
	input.Placeholder = "Type a message (Enter to send)"
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.Prompt = "> "
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.Focus()

	vp := viewport.New(0, 0)
	initial := theme.HeadingStyle().Render(fmt.Sprintf("Chat with %s (session %s)", agentRef, sessionID))
	vp.SetContent(initial)
	vp.MouseWheelEnabled = true

	sp := spinner.New()
	sp.Spinner = spinner.Hamburger
	sp.Style = lipgloss.NewStyle().Foreground(theme.ColorPrimary)

	return &chatModel{
		agentRef:  agentRef,
		sessionID: sessionID,
		verbose:   verbose,
		vp:        vp,
		input:     input,
		send:      send,
		history:   initial,
		spin:      sp,
		showInput: true,
	}
}

func (m *chatModel) Init() tea.Cmd {
	return nil
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Always let viewport handle scrolling keys and mouse
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.working {
			var sCmd tea.Cmd
			m.spin, sCmd = m.spin.Update(msg)
			if sCmd != nil {
				cmds = append(cmds, sCmd)
			}
			return m, tea.Batch(cmds...)
		}
	case tickMsg:
		if m.working {
			m.updateStatus()
			return m, m.tick()
		}
		return m, nil
	case tea.WindowSizeMsg:
		// Reserve space for input and separator
		inputHeight := 3
		if !m.showInput {
			inputHeight = 0
		}
		sepHeight := 2 // extra line for status
		vpHeight := msg.Height - inputHeight - sepHeight
		if vpHeight < 5 {
			vpHeight = 5
		}
		m.vp.Width = msg.Width
		m.vp.Height = vpHeight
		m.input.SetWidth(msg.Width)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "enter":
			if !m.showInput {
				return m, nil
			}
			if m.streaming {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			m.appendUser(text)
			m.input.Reset()
			return m, m.submit(text)
		}
	case a2aEventMsg:
		m.appendEvent(msg.Event)
		return m, m.waitNext()
	case streamDoneMsg:
		m.streaming = false
		m.working = false
		m.updateStatus()
		return m, nil
	}

	m.input, cmd = m.input.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *chatModel) View() string {
	width := m.vp.Width
	if width <= 0 {
		width = 80 // default width if not yet sized
	}
	status := m.statusText
	if status == "" {
		status = ""
	}
	if m.working {
		status = fmt.Sprintf("%s %s", m.spin.View(), status)
	}
	if m.showInput {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.vp.View(),
			theme.SeparatorStyle().Render(strings.Repeat("─", max(10, width))),
			theme.StatusStyle().Render(status),
			m.input.View(),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.vp.View(),
		theme.SeparatorStyle().Render(strings.Repeat("─", max(10, width))),
		theme.StatusStyle().Render(status),
	)
}

func (m *chatModel) submit(text string) tea.Cmd {
	m.streaming = true
	m.working = true
	m.workStart = time.Now()
	m.updateStatus()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	params := protocol.SendMessageParams{
		Message: protocol.Message{
			Role:      protocol.MessageRoleUser,
			ContextID: &m.sessionID,
			Parts:     []protocol.Part{protocol.NewTextPart(text)},
		},
	}

	ch, err := m.send(ctx, params)
	if err != nil {
		m.appendError(err)
		m.streaming = false
		cancel()
		return nil
	}
	m.streamCh = ch
	return tea.Batch(m.waitNext(), m.tick())
}

func (m *chatModel) waitNext() tea.Cmd {
	ch := m.streamCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return a2aEventMsg{Event: ev}
	}
}

func (m *chatModel) appendUser(text string) {
	m.appendLine(theme.UserStyle().Render("You:") + " " + text)
}

func (m *chatModel) appendEvent(ev protocol.StreamingMessageEvent) {
	// Extract human-friendly text from the event payload; fallback to compact JSON
	b, err := ev.MarshalJSON()
	if err != nil {
		m.appendError(err)
		return
	}
	var v any
	if err := json.Unmarshal(b, &v); err == nil {
		// Handle status-update for progress and tool call summaries
		if kind := jsonutil.GetString(v, "kind"); kind == "status-update" {
			state := jsonutil.GetString(jsonutil.GetMap(v, "status"), "state")
			switch state {
			case "working":
				m.setWorking(jsonutil.GetString(jsonutil.GetMap(v, "status"), "timestamp"))
				// Summarize function/tool calls
				if msgMap := jsonutil.GetMap(jsonutil.GetMap(v, "status"), "message"); msgMap != nil {
					for _, p := range jsonutil.GetArray(msgMap, "parts") {
						pm, _ := p.(map[string]any)
						meta := jsonutil.GetMap(pm, "metadata")
						ktype := jsonutil.GetString(meta, "kagent_type")
						if ktype == "function_call" || ktype == "tool_call" {
							data := jsonutil.GetMap(pm, "data")
							name := jsonutil.GetString(data, "name")
							args := jsonutil.GetMap(data, "args")
							argsStr := jsonutil.CompactJSON(args)
							m.appendLine(theme.ToolStyle().Render("→ Calling ") + name + " " + argsStr)
						}
						if ktype == "function_result" || ktype == "tool_result" {
							data := jsonutil.GetMap(pm, "data")
							name := jsonutil.GetString(data, "name")
							m.appendLine(theme.ToolStyle().Render("← Result from ") + name)
						}
					}
				}
			case "completed":
				m.working = false
				m.updateStatus()
			}
			// Only render final response text when final=true or when state completed
			final := jsonutil.GetBool(v, "final")
			if final || state == "completed" {
				parts := make([]string, 0, 8)
				// Prefer text from status.message.parts
				if msgMap := jsonutil.GetMap(jsonutil.GetMap(v, "status"), "message"); msgMap != nil {
					for _, p := range jsonutil.GetArray(msgMap, "parts") {
						pm, _ := p.(map[string]any)
						if t := jsonutil.GetString(pm, "kind"); t == "text" {
							if s := jsonutil.GetString(pm, "text"); strings.TrimSpace(s) != "" {
								parts = append(parts, s)
							}
						}
					}
				}
				if len(parts) == 0 {
					// fallback to any nested text fields
					jsonutil.CollectTextFields(v, &parts)
				}
				text := strings.Join(parts, "")
				if strings.TrimSpace(text) != "" {
					m.appendLine(theme.AgentStyle().Render("Agent:") + "\n" + text)
				}
			}
			return
		}
		// Non-status events: render only text parts if present
		parts := make([]string, 0, 4)
		jsonutil.CollectTextFields(v, &parts)
		text := strings.Join(parts, "")
		if strings.TrimSpace(text) != "" {
			m.appendLine(theme.AgentStyle().Render("Agent:") + "\n" + text)
			return
		}
	}
	if m.verbose {
		m.appendLine(theme.AgentStyle().Render("Agent (raw):") + "\n" + string(b))
	}
}

func (m *chatModel) appendError(err error) {
	m.appendLine(theme.ErrorStyle().Render(fmt.Sprintf("Error: %v", err)))
}

func (m *chatModel) appendLine(s string) {
	if m.history == "" {
		m.history = s
	} else {
		m.history = m.history + "\n\n" + s
	}
	m.vp.SetContent(m.history)
	m.vp.GotoBottom()
}

// ResetTranscript clears the viewport with a new header/title.
func (m *chatModel) ResetTranscript(title string) {
	m.history = title
	m.vp.SetContent(m.history)
	m.vp.GotoBottom()
}

// SetInputVisible toggles input visibility.
func (m *chatModel) SetInputVisible(visible bool) {
	m.showInput = visible
}

// AppendEventJSON appends an event JSON blob by extracting text fields.
func (m *chatModel) AppendEventJSON(eventJSON string) {
	var v any
	if err := json.Unmarshal([]byte(eventJSON), &v); err != nil {
		return
	}
	parts := make([]string, 0, 8)
	jsonutil.CollectTextFields(v, &parts)
	text := strings.Join(parts, "")
	if strings.TrimSpace(text) != "" {
		m.appendLine(theme.AgentStyle().Render("Agent:") + "\n" + text)
	}
}

// styles now provided by theme package

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// JSON helpers now provided by util package

type tickMsg time.Time

func (m *chatModel) tick() tea.Cmd {
	if !m.working {
		return nil
	}
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *chatModel) setWorking(ts string) {
	if !m.working {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			m.workStart = t
		} else {
			m.workStart = time.Now()
		}
	}
	m.working = true
	m.updateStatus()
}

func (m *chatModel) updateStatus() {
	if m.working {
		dur := time.Since(m.workStart).Round(time.Second)
		m.statusText = fmt.Sprintf("Working… %s", dur.String())
	} else {
		m.statusText = ""
	}
}
