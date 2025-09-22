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
	"github.com/kagent-dev/kagent/go/internal/a2a"
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
	switch res := ev.Result.(type) {
	case *protocol.TaskStatusUpdateEvent:
		if res.Final {
			m.working = false
			m.updateStatus()
		} else {
			// Timestamp is RFC3339 string; parse to time for consistent elapsed display
			if ts, err := time.Parse(time.RFC3339Nano, res.Status.Timestamp); err == nil {
				m.setWorkingTime(ts)
			} else {
				m.setWorkingTime(time.Time{})
			}
		}
		if res.Status.Message != nil {
			if res.Final {
				text := extractTextFromMessage(*res.Status.Message)
				if strings.TrimSpace(text) != "" {
					m.appendLine(theme.AgentStyle().Render("Agent:") + "\n" + text)
				}
			}
		}
	case *protocol.TaskArtifactUpdateEvent:
		// Render artifact content when the last chunk arrives
		if res.LastChunk != nil && *res.LastChunk {
			text := extractTextFromParts(res.Artifact.Parts)
			if strings.TrimSpace(text) != "" {
				m.appendLine(theme.AgentStyle().Render("Agent:") + "\n" + text)
			}
		}
	case *protocol.Message:
		text := extractTextFromMessage(*res)
		if strings.TrimSpace(text) != "" {
			style := theme.UserStyle()
			if res.Role == protocol.MessageRoleAgent {
				style = theme.AgentStyle()
			}
			m.appendLine(style.Render(fmt.Sprintf("%s:", res.Role)) + "\n" + text)
		}

	case *protocol.Task:
		// Show the last message in the task history
		if len(res.History) > 0 {
			last := res.History[len(res.History)-1]
			text := extractTextFromMessage(last)
			if strings.TrimSpace(text) != "" {
				m.appendLine(theme.AgentStyle().Render("Agent:") + "\n" + text)
			}
		}
	default:
		if m.verbose {
			if b, err := ev.MarshalJSON(); err == nil {
				m.appendLine(theme.AgentStyle().Render("Agent (raw):") + "\n" + string(b))
			}
		}
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

// extractTextFromMessage concatenates all text parts from a protocol.Message.
func extractTextFromMessage(msg protocol.Message) string {
	return a2a.ExtractText(msg)
}

// extractTextFromParts concatenates text from a slice of protocol.Part, stringifying non-text when reasonable.
func extractTextFromParts(parts []protocol.Part) string {
	b := strings.Builder{}
	for _, p := range parts {
		if tp, ok := p.(*protocol.TextPart); ok {
			b.WriteString(tp.Text)
			continue
		}

		if dp, ok := p.(*protocol.DataPart); ok {
			if jp, err := json.Marshal(dp.Data); err == nil {
				b.WriteString(string(jp))
			}
			continue
		}
	}
	return b.String()
}

// styles now provided by theme package

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type tickMsg time.Time

func (m *chatModel) tick() tea.Cmd {
	if !m.working {
		return nil
	}
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *chatModel) setWorkingTime(ts time.Time) {
	if !m.working {
		if !ts.IsZero() {
			m.workStart = ts
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
