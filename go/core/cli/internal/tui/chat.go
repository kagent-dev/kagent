package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kagent-dev/kagent/go/api/utils"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/instance"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/theme"
	"github.com/muesli/reflow/wordwrap"
)

// SendMessageFn abstracts the A2A client's SendStreamingMessage method for easier testing.
type SendMessageFn func(ctx context.Context, req *a2atype.SendMessageRequest) <-chan clia2a.StreamResult

type streamDoneMsg struct{}

type toolCall struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Args any    `json:"args"`
}

type toolResult struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	Response any    `json:"response"`
}

type chatModel struct {
	agentRef  string
	contextID string
	// The header is pinned above the viewport rather than written into
	// history, so it stays visible however long the transcript grows. state
	// arrives pre-rendered: a local agent has no lifecycle and leaves it empty.
	state      string
	lastActive time.Time
	verbose    bool

	vp    viewport.Model
	input textarea.Model

	// agentText is the trailing block still being assembled; it grows in place
	// and is committed into history before anything else is written.
	history   string
	agentText string

	// projected is the assembler's last text projection, so a cumulative
	// replacement chunk yields a delta rather than duplicated text.
	assembler *clia2a.Assembler
	projected string
	lastState a2atype.TaskState

	working    bool
	workStart  time.Time
	statusText string

	spin spinner.Model

	// ctx is the workspace's context, so a signal that cancels the program also
	// cancels an in-flight stream rather than leaving it to process teardown.
	ctx       context.Context
	send      SendMessageFn
	streamCh  <-chan clia2a.StreamResult
	cancel    context.CancelFunc
	streaming bool
}

func newChatModel(ctx context.Context, agentRef string, contextID string, send SendMessageFn, verbose bool) *chatModel {
	input := textarea.New()
	input.Placeholder = "Type a message (Enter to send)"
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.Prompt = "> "
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.Focus()

	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true

	sp := spinner.New()
	sp.Spinner = spinner.Hamburger
	sp.Style = lipgloss.NewStyle().Foreground(theme.ColorPrimary)

	return &chatModel{
		ctx:       ctx,
		agentRef:  agentRef,
		contextID: contextID,
		verbose:   verbose,
		vp:        vp,
		input:     input,
		send:      send,
		spin:      sp,
	}
}

func (m *chatModel) Init() tea.Cmd {
	return m.spin.Tick
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
		// What View draws around the viewport: the pinned header and its rule
		// above; a rule, the status line, and the input below.
		const chromeHeight = 5
		vpHeight := max(msg.Height-chromeHeight, 5)

		oldWidth := m.vp.Width
		m.vp.Width = msg.Width
		m.vp.Height = vpHeight
		m.input.SetWidth(msg.Width)

		// Re-render content if width changed
		if oldWidth != msg.Width && msg.Width > 0 {
			m.render()
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "enter":
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
	case clia2a.StreamResult:
		if msg.Err != nil {
			m.appendTransportError(msg.Err)
			m.endStream()
			return m, nil
		}
		m.appendEvent(msg.Event)
		return m, m.waitNext()
	case streamDoneMsg:
		m.endStream()
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
	if m.working {
		status = fmt.Sprintf("%s %s", m.spin.View(), status)
	}
	rule := theme.SeparatorStyle().Render(strings.Repeat("─", max(10, width)))
	return lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(width),
		rule,
		m.vp.View(),
		rule,
		theme.StatusStyle().Render(status),
		m.input.View(),
	)
}

// setHeaderMeta adds lifecycle detail to the pinned header. The caller renders
// state, so this stays free of any control-plane type.
func (m *chatModel) setHeaderMeta(state string, lastActive time.Time) {
	m.state, m.lastActive = state, lastActive
}

// stop cancels an in-flight stream, so a replaced chat stops delivering.
func (m *chatModel) stop() {
	if m != nil && m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

// headerView pins who you are talking to, its state, and when it last ran.
// The ID is abbreviated so the metadata after it survives a narrow pane; the
// details pane carries the full copyable one.
func (m *chatModel) headerView(width int) string {
	parts := []string{
		theme.HeadingStyle().Render(m.agentRef),
		theme.DimStyle().Render(instance.ShortID(m.contextID)),
	}
	if m.state != "" {
		parts = append(parts, m.state)
	}
	if !m.lastActive.IsZero() {
		parts = append(parts, theme.DimStyle().Render("active "+instance.Since(m.lastActive, time.Now())+" ago"))
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(strings.Join(parts, theme.DimStyle().Render(" · ")))
}

func (m *chatModel) submit(text string) tea.Cmd {
	m.streaming = true
	m.assembler = &clia2a.Assembler{}
	m.projected = ""
	m.lastState = ""
	m.setWorkingTime(time.Time{})
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel

	msg := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(text))
	msg.ContextID = m.contextID
	req := &a2atype.SendMessageRequest{Message: msg}

	m.streamCh = m.send(ctx, req)
	return tea.Batch(m.waitNext(), m.tick())
}

func (m *chatModel) waitNext() tea.Cmd {
	ch := m.streamCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return result
	}
}

// endStream commits any in-flight agent text and clears the working state.
func (m *chatModel) endStream() {
	m.commitAgentText()
	m.streaming = false
	m.working = false
	m.lastActive = time.Now()
	m.updateStatus()
}

// appendEvent reduces one stream event and renders what it changed.
func (m *chatModel) appendEvent(ev a2atype.Event) {
	if ev == nil {
		return
	}
	if m.assembler == nil {
		m.assembler = &clia2a.Assembler{}
	}
	if err := m.assembler.Apply(ev); err != nil {
		m.appendLine(theme.ErrorStyle().Render(fmt.Sprintf("Protocol error: %v", err)))
		return
	}
	m.renderToolActivity(eventParts(ev))
	m.renderAssembledText()
	m.renderState()
}

// eventParts returns what one event carries, so tool activity shows as it happens
// rather than only in the assembled task.
func eventParts(ev a2atype.Event) a2atype.ContentParts {
	switch res := ev.(type) {
	case *a2atype.Message:
		return res.Parts
	case *a2atype.TaskStatusUpdateEvent:
		if res.Status.Message != nil {
			return res.Status.Message.Parts
		}
	case *a2atype.TaskArtifactUpdateEvent:
		if res.Artifact != nil {
			return res.Artifact.Parts
		}
	}
	return nil
}

// renderAssembledText appends newly assembled text. The projection is cumulative,
// so an extension grows the block in place and a replacement starts a new one.
func (m *chatModel) renderAssembledText() {
	text := assembledText(m.assembler.Result())
	if text == m.projected {
		return
	}
	delta, extends := strings.CutPrefix(text, m.projected)
	if extends {
		m.agentText += delta
	} else {
		m.commitAgentText()
		m.agentText = text
	}
	m.projected = text
	m.render()
}

// assembledText projects agent output. Only artifacts carry it; status messages
// are control-plane content, rendered by renderState.
func assembledText(result a2atype.SendMessageResult) string {
	switch result := result.(type) {
	case *a2atype.Message:
		return clia2a.PartsText(result.Parts)
	case *a2atype.Task:
		var groups []string
		for _, artifact := range result.Artifacts {
			if artifact == nil {
				continue
			}
			if text := clia2a.PartsText(artifact.Parts); text != "" {
				groups = append(groups, text)
			}
		}
		return strings.Join(groups, "\n")
	default:
		return ""
	}
}

// renderState banners a state change: paused states invite another message,
// terminal failures are errors, completion needs no banner.
func (m *chatModel) renderState() {
	task, ok := m.assembler.Result().(*a2atype.Task)
	if !ok {
		return
	}
	state := task.Status.State
	if state == m.lastState {
		return
	}
	m.lastState = state

	switch state {
	// The gateway rejects a message carrying a TaskID, so a reply cannot
	// resume a paused task yet; it starts a new one. Say so rather than
	// promising a continuation that does not happen.
	case a2atype.TaskStateInputRequired:
		m.appendLine(theme.StatusStyle().Render("⏸ Input required. Resuming a paused task is not supported yet; a reply starts a new one."))
	case a2atype.TaskStateAuthRequired:
		m.appendLine(theme.StatusStyle().Render("⏸ Authentication required. This task cannot continue here."))
	case a2atype.TaskStateFailed, a2atype.TaskStateRejected, a2atype.TaskStateCanceled:
		banner := fmt.Sprintf("✗ Task %s.", state)
		if task.Status.Message != nil {
			if detail := clia2a.PartsText(task.Status.Message.Parts); strings.TrimSpace(detail) != "" {
				banner += " " + detail
			}
		}
		m.appendLine(theme.ErrorStyle().Render(banner))
	}
	if state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired {
		m.working = false
		m.updateStatus()
	} else if task.Status.Timestamp != nil {
		m.setWorkingTime(*task.Status.Timestamp)
	}
}

// AppendHistoryTask renders a past task: its user messages, then its output.
func (m *chatModel) AppendHistoryTask(task *a2atype.Task) {
	if task == nil {
		return
	}
	for _, msg := range task.History {
		if msg == nil || msg.Role != a2atype.MessageRoleUser {
			continue
		}
		if text := clia2a.PartsText(msg.Parts); strings.TrimSpace(text) != "" {
			m.appendUser(text)
		}
	}
	if text := assembledText(task); strings.TrimSpace(text) != "" {
		m.appendLine(theme.AgentStyle().Render("Agent:") + "\n" + text)
	}
}

func (m *chatModel) appendUser(text string) {
	m.appendLine(theme.UserStyle().Render("You:") + " " + text)
}

// appendTransportError reports a stream or connection failure, which is
// distinct from a task the agent itself failed.
func (m *chatModel) appendTransportError(err error) {
	m.appendLine(theme.ErrorStyle().Render(fmt.Sprintf("Connection error: %v", err)))
}

// renderToolActivity shows tool calls and results. These are kagent data parts;
// the reducer has no opinion about them.
func (m *chatModel) renderToolActivity(parts a2atype.ContentParts) {
	var calls []toolCall
	var results []toolResult

	for _, part := range parts {
		if part == nil {
			continue
		}
		data := part.Data()
		if data == nil {
			continue
		}

		if m.verbose {
			if metaJSON, err := json.Marshal(part.Metadata); err == nil {
				m.appendLine(theme.DimStyle().Render(fmt.Sprintf("DEBUG: DataPart metadata: %s", string(metaJSON))))
			}
			if dataJSON, err := json.Marshal(data); err == nil {
				m.appendLine(theme.DimStyle().Render(fmt.Sprintf("DEBUG: DataPart data: %s", string(dataJSON))))
			}
		}

		if part.Metadata == nil {
			continue
		}
		typeVal, found := utils.GetMetadataValue(part.Metadata, "type")
		if !found {
			continue
		}
		kagentType, ok := typeVal.(string)
		if !ok {
			continue
		}
		dataMap, ok := data.(map[string]any)
		if !ok {
			continue
		}

		switch kagentType {
		case "function_call":
			calls = append(calls, toolCall{
				Name: getString(dataMap, "name"),
				ID:   getString(dataMap, "id"),
				Args: dataMap["args"],
			})
		case "function_response":
			results = append(results, toolResult{
				Name:     getString(dataMap, "name"),
				ID:       getString(dataMap, "id"),
				Response: dataMap["response"],
			})
		}
	}

	for _, call := range calls {
		display := theme.ToolCallStyle().Render(fmt.Sprintf("🔧 Tool Call: %s", call.Name))
		if call.ID != "" {
			display += theme.DimStyle().Render(fmt.Sprintf(" (id: %s)", call.ID))
		}
		if args := indentJSON(call.Args); args != "" {
			display += "\n" + theme.DimStyle().Render(args)
		}
		m.appendLine(display)
	}
	for _, result := range results {
		display := theme.ToolResultStyle().Render(fmt.Sprintf("📊 Tool Result: %s", result.Name))
		if result.ID != "" {
			display += theme.DimStyle().Render(fmt.Sprintf(" (id: %s)", result.ID))
		}
		if response := indentJSON(result.Response); response != "" {
			display += "\n" + response
		}
		m.appendLine(display)
	}
}

func indentJSON(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

// commitAgentText closes the in-flight block so another can follow it.
func (m *chatModel) commitAgentText() {
	if m.agentText == "" {
		return
	}
	m.history = joinBlocks(m.history, theme.AgentStyle().Render("Agent:")+"\n"+m.agentText)
	m.agentText = ""
}

func (m *chatModel) appendLine(s string) {
	m.commitAgentText()
	m.history = joinBlocks(m.history, s)
	m.render()
}

func joinBlocks(history, block string) string {
	if history == "" {
		return block
	}
	return history + "\n\n" + block
}

// render redraws the viewport, wrapping to the current width.
func (m *chatModel) render() {
	content := m.history
	if m.agentText != "" {
		content = joinBlocks(content, theme.AgentStyle().Render("Agent:")+"\n"+m.agentText)
	}
	if m.vp.Width > 2 {
		content = wordwrap.String(content, m.vp.Width-2) // -2 for padding
	}
	m.vp.SetContent(content)
	m.vp.GotoBottom()
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
		m.statusText = fmt.Sprintf("Working… %s", time.Since(m.workStart).Round(time.Second))
	} else {
		m.statusText = ""
	}
}

// getString safely extracts a string value from a map
func getString(m map[string]any, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
