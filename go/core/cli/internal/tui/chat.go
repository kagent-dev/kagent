package tui

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/reflow/wordwrap"

	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	sessionview "github.com/kagent-dev/kagent/go/core/cli/internal/tui/session"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/theme"
)

// SendMessageFn abstracts the A2A client's SendStreamingMessage method for easier testing.
type SendMessageFn func(ctx context.Context, req *a2atype.SendMessageRequest) <-chan clia2a.StreamResult

// streamMsg and streamDoneMsg name the stream they came from, so a replaced turn or chat drops them.
type streamMsg struct {
	ch <-chan clia2a.StreamResult
	clia2a.StreamResult
}

type streamDoneMsg struct{ ch <-chan clia2a.StreamResult }

// toggleSidebarMsg asks the workspace to show or hide its sidebar for this run.
type toggleSidebarMsg struct{}

type chatModel struct {
	agentRef  string
	contextID string
	// The header is pinned above the viewport so it survives a long transcript.
	state      string
	lastActive time.Time
	verbose    bool

	vp    viewport.Model
	input textarea.Model

	// blocks is the transcript; agentText is the markdown still streaming, committed into blocks before anything else.
	blocks    []*block
	agentText string
	// tools finds a call's entry by ID, so its result fills it in place.
	tools map[string]*toolEntry
	// expanded shows every tool call's full output and every thought; ctrl+x toggles it.
	expanded bool
	// projectedThought is the assembler's last thought projection; thinking is
	// the thought block still growing, nil once the model moves on.
	projectedThought string
	thinking         *block
	// turnTools and turnTokens count the running turn's tool calls and model tokens, for its closing line.
	turnTools  int
	turnTokens int64
	// turnStart is when the user's prompt started the turn; zero for a turn this chat did not start.
	turnStart time.Time
	// attention is set when a turn finishes or waits on the user; the workspace
	// takes it to ring the bell when the terminal is not focused.
	attention bool
	// md renders markdown at mdWidth, rebuilt when the width changes.
	md      *glamour.TermRenderer
	mdWidth int
	// liveDone is the streaming reply's finished markdown, and liveOut its glamour rendering.
	liveDone, liveOut string
	// body is the laid-out transcript, without the running turn's status line.
	body string
	// spans maps content lines to blocks, and lines holds the shown content, so a click finds what it landed on.
	spans []span
	lines []string
	// geo is where View last drew the transcript, for the mouse.
	geo viewGeometry
	// selection is the text a drag selected, and dragFrom where the button went down.
	selection *selection
	dragFrom  *cell
	// unfocused is set while another workspace panel has focus, so the input's bar matches the dimmer border.
	unfocused bool
	// height is the chat's whole height, shared by the transcript and the growing input.
	height int
	// open is set when the chat has no box around it, so a bar closes the input below as well.
	open bool
	// inputCapped is set while the input has more rows than it has room to show.
	inputCapped bool
	// queue holds messages typed during a turn, sent one per turn as each ends.
	queue []string
	// unseen counts transcript lines added while scrolled up, for the jump-to-latest pill.
	unseen int
	// notice briefly takes the row above the input's bar, such as the exit prompt.
	notice   string
	noticeID int
	// cancelled is set when ctrl+c stopped the running turn.
	cancelled bool
	// prompts are the user's messages in this session, oldest first; promptAt is
	// the one in the input, len(prompts) for the draft, which draft keeps meanwhile.
	prompts  []string
	promptAt int
	draft    string
	// queuePaused names how the last turn ended, "failed" or "cancelled", while that holds the queue.
	queuePaused string

	// projected is the assembler's last text projection, so cumulative chunks yield a delta not a duplicate.
	assembler *clia2a.Assembler
	projected string
	lastState a2atype.TaskState

	working    bool
	workStart  time.Time
	statusText string

	spin spinner.Model

	// ctx is the workspace's context, so cancelling the program cancels an in-flight stream.
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
	input.ShowLineNumbers = false
	input.MaxHeight = 0 // the input grows with its content; fitInput sizes it
	// Later lines sit under the first line's text, not under its prompt.
	input.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "> "
		}
		return "  "
	})
	input.SetHeight(1)
	input.Cursor.SetMode(cursor.CursorStatic)
	input.Focus()

	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	// Letter keys are the input's, so typing never scrolls.
	vp.KeyMap = viewport.KeyMap{
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		Up:       key.NewBinding(key.WithKeys("up")),
		Down:     key.NewBinding(key.WithKeys("down")),
	}

	sp := spinner.New()
	sp.Spinner = spinner.Spinner{Frames: []string{"◐", "◓", "◑", "◒"}, FPS: time.Second / 6}
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
	if key, ok := msg.(tea.KeyMsg); ok {
		// Up pulls every queued message back into the input, after what is there, to edit as one.
		if key.String() == "up" && len(m.queue) > 0 && m.inputAtFirstRow() {
			m.pullQueue()
			return m, nil
		}
		// From the input's first or last row, up and down step through the prompts sent in this session.
		if s := key.String(); s == "up" && m.inputAtFirstRow() && m.promptAt > 0 || s == "down" && m.inputAtLastRow() && m.promptAt < len(m.prompts) {
			m.recallPrompt(s == "up")
			return m, nil
		}
		// Up and down move between the input's rows, wrapped ones included; at its first or last row they scroll the transcript.
		if s := key.String(); s == "up" && !m.inputAtFirstRow() || s == "down" && !m.inputAtLastRow() {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}
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
			m.showContent()
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
		oldWidth := m.vp.Width
		m.vp.Width, m.height = msg.Width, msg.Height
		m.input.SetWidth(msg.Width)
		m.fitInput()

		// Re-render content if width changed
		if oldWidth != msg.Width && msg.Width > 0 {
			m.render()
		}
		return m, nil
	case copiedMsg:
		return m, m.copiedNotice(msg)
	case noticeDoneMsg:
		if msg.id == m.noticeID {
			m.notice = ""
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+end":
			m.jumpToLatest()
			return m, nil
		case "end":
			if m.input.Value() == "" {
				m.jumpToLatest()
				return m, nil
			}
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				m.queuePaused = "" // Enter on an empty input resumes a paused queue
				return m, m.sendQueued()
			}
			m.input.Reset()
			m.promptAt, m.draft = len(m.prompts), ""
			// The one command the input takes; any other text, slashes included, is a message.
			if text == "/sidebar" {
				return m, func() tea.Msg { return toggleSidebarMsg{} }
			}
			// A message typed during a turn waits in the queue for the turn to end.
			if m.streaming {
				m.queue = append(m.queue, text)
				return m, nil
			}
			m.appendUser(text)
			return m, m.submit(text)
		case "alt+enter", "ctrl+j":
			// Shift+Enter arrives as alt+enter (see kittyInput); ctrl+j is the fallback everywhere.
			m.input.InsertString("\n")
			m.fitInput()
			return m, nil
		}
		// A paste, newlines and all, is input to edit, never a send.
		if msg.Paste {
			m.input.InsertString(cleanPaste(string(msg.Runes)))
			m.fitInput()
			return m, nil
		}
	case streamMsg:
		if msg.ch != m.streamCh {
			return m, nil
		}
		if msg.Err != nil {
			if m.cancelled {
				m.endStream()
				return m, m.sendQueued()
			}
			m.appendTransportError(msg.Err)
			m.endStream()
			if len(m.queue) > 0 {
				m.queuePaused = "failed"
			}
			return m, m.sendQueued()
		}
		m.appendEvent(msg.Event)
		return m, m.waitNext()
	case streamDoneMsg:
		if msg.ch != m.streamCh {
			return m, nil
		}
		m.endStream()
		return m, m.sendQueued()
	}

	m.input, cmd = m.input.Update(msg)
	m.fitInput()
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
	m.fitInput()
	rule := theme.SeparatorStyle().Render(strings.Repeat("─", max(10, width)))
	// The bar above the input is the chat box's border colour, focused or not.
	bar := lipgloss.NewStyle().Foreground(borderColor(!m.unfocused)).Render(strings.Repeat("▔", max(10, width)))
	parts := []string{m.headerView(width), rule}
	transcript := m.vp.View()
	m.geo = viewGeometry{top: len(parts), rows: lineCount(transcript), status: -1, pill: -1}
	parts = append(parts, transcript)
	if queued := m.queueView(width); queued != "" {
		parts = append(parts, queued)
	}
	// The row above the bar holds notices and the jump-to-latest pill; with neither, it takes no space.
	if m.noticeRow() {
		status := ""
		if m.notice != "" {
			status = theme.StatusStyle().Render(m.notice)
		}
		m.geo.status = lineCount(strings.Join(parts, "\n"))
		parts = append(parts, m.withPill(status, width))
	}
	parts = append(parts, bar, m.input.View())
	if m.open {
		// Without a box around the chat, a second bar closes the input from below.
		parts = append(parts, lipgloss.NewStyle().Foreground(borderColor(!m.unfocused)).Render(strings.Repeat("▁", max(10, width))))
	}
	view := lipgloss.JoinVertical(lipgloss.Left, parts...)
	m.geo.view = strings.Split(view, "\n")
	return strings.Join(highlightRows(slices.Clone(m.geo.view), m.selection, width), "\n")
}

// setHeaderMeta adds lifecycle detail to the header; the caller pre-renders state to keep this type-free.
func (m *chatModel) setHeaderMeta(state string, lastActive time.Time) {
	m.state, m.lastActive = state, lastActive
}

// cancelTurn stops a running turn; its stream then ends with an error that reads as cancelled.
func (m *chatModel) cancelTurn() {
	if m.streaming && m.cancel != nil {
		m.cancelled = true
		m.stop()
	}
}

// stop cancels an in-flight stream, so a replaced chat stops delivering.
func (m *chatModel) stop() {
	if m != nil && m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

// headerView pins who you are talking to; the ID is abbreviated so metadata survives a narrow pane.
func (m *chatModel) headerView(width int) string {
	parts := []string{
		// The chat's panel key leads the row, which doubles as the panel's title.
		panelKeyStyle(!m.unfocused).Render(fmt.Sprintf("[%d]", panelChat)) + " " + theme.HeadingStyle().Render(m.agentRef),
		theme.DimStyle().Render(sessionview.ShortID(m.contextID)),
	}
	if m.state != "" {
		parts = append(parts, m.state)
	}
	if !m.lastActive.IsZero() {
		parts = append(parts, theme.DimStyle().Render("active "+sessionview.Since(m.lastActive, time.Now())+" ago"))
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(strings.Join(parts, theme.DimStyle().Render(" · ")))
}

func (m *chatModel) submit(text string) tea.Cmd {
	m.streaming = true
	m.assembler = &clia2a.Assembler{}
	m.projected, m.projectedThought = "", ""
	m.lastState = ""
	m.turnTools, m.turnTokens, m.turnStart = 0, 0, time.Now()
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
			return streamDoneMsg{ch: ch}
		}
		return streamMsg{ch: ch, StreamResult: result}
	}
}

// endStream commits any in-flight agent text and clears the working state.
func (m *chatModel) endStream() {
	m.endThinking()
	m.commitAgentText()
	if m.cancelled && m.lastState != a2atype.TaskStateCanceled {
		m.appendLine(theme.AttentionStyle().Render("⊘ Cancelled"))
	}
	// A turn that failed or was cancelled pauses the queue rather than sending into the same trouble.
	switch {
	case len(m.queue) == 0:
	case m.cancelled || m.lastState == a2atype.TaskStateCanceled:
		m.queuePaused = "cancelled"
	case m.lastState == a2atype.TaskStateFailed || m.lastState == a2atype.TaskStateRejected:
		m.queuePaused = "failed"
	}
	m.cancelled = false
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
		m.appendLine(theme.ErrorStyle().Render("Protocol error: " + sanitize(err.Error())))
		return
	}
	// The terminal update carries the task's own total, which beats summing per-event usage.
	if usage, ok := kagenta2a.UsageFromMetadata(ev.Meta()); ok {
		m.turnTokens = usage.TotalTokens
	} else {
		m.turnTokens += eventTokens(ev)
	}
	m.renderThoughts()
	m.renderToolActivity(eventParts(ev))
	m.renderAssembledText()
	m.renderState()
	m.updateStatus()
}

// renderThoughts grows the thought block with new reasoning summary, starting
// one when the model starts thinking again after a tool call or text.
func (m *chatModel) renderThoughts() {
	text := assembledThought(m.assembler.Result())
	if text == m.projectedThought {
		return
	}
	delta, extends := strings.CutPrefix(text, m.projectedThought)
	if !extends {
		delta = text
	}
	m.projectedThought = text
	if m.thinking == nil {
		m.thinking = &block{thought: &thoughtEntry{start: time.Now()}, expanded: m.expanded}
		m.appendBlock(m.thinking)
		delta = strings.TrimLeft(delta, "\n")
	}
	m.thinking.thought.text += delta
	m.render()
}

// endThinking closes the thought block once the model acts or answers.
func (m *chatModel) endThinking() {
	if m.thinking != nil {
		m.thinking.thought.end = time.Now()
		m.thinking = nil
		m.render()
	}
}

// assembledThought projects the reasoning summaries in a task's artifacts.
func assembledThought(result a2atype.SendMessageResult) string {
	task, ok := result.(*a2atype.Task)
	if !ok {
		return ""
	}
	var groups []string
	for _, artifact := range task.Artifacts {
		if artifact == nil {
			continue
		}
		if text := clia2a.ThoughtText(artifact.Parts); text != "" {
			groups = append(groups, text)
		}
	}
	return strings.Join(groups, "\n\n")
}

// eventParts returns what one event carries, so tool activity shows as it happens.
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

// renderAssembledText appends newly assembled text; the cumulative projection grows the block in place.
func (m *chatModel) renderAssembledText() {
	text, err := assembledText(m.assembler.Result())
	if err != nil {
		m.appendTransportError(err)
		return
	}
	if text == m.projected {
		return
	}
	m.endThinking()
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

// assembledText projects agent output; only artifacts carry it, status messages are control-plane.
func assembledText(result a2atype.SendMessageResult) (string, error) {
	switch result := result.(type) {
	case *a2atype.Message:
		return clia2a.PartsText(result.Parts)
	case *a2atype.Task:
		var groups []string
		for _, artifact := range result.Artifacts {
			if artifact == nil {
				continue
			}
			text, err := clia2a.PartsText(artifact.Parts)
			if err != nil {
				return "", err
			}
			if text != "" {
				groups = append(groups, text)
			}
		}
		return strings.Join(groups, "\n"), nil
	default:
		return "", nil
	}
}

// renderState banners a state change, and closes a finished turn with how it went.
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
	// The gateway rejects a message carrying a TaskID, so a reply starts a new task rather than resuming.
	case a2atype.TaskStateInputRequired:
		m.appendLine(theme.StatusStyle().Render("⏸ Input required. Resuming a paused task is not supported yet; a reply starts a new one."))
	case a2atype.TaskStateAuthRequired:
		m.appendLine(theme.StatusStyle().Render("⏸ Authentication required. This task cannot continue here."))
	case a2atype.TaskStateCompleted:
		m.endThinking()
		m.commitAgentText()
		m.appendLine(doneLine(time.Since(cmp.Or(m.turnStart, m.workStart)), m.turnTools, m.turnTokens))
	case a2atype.TaskStateCanceled:
		m.appendLine(theme.AttentionStyle().Render("⊘ Cancelled"))
	case a2atype.TaskStateFailed, a2atype.TaskStateRejected:
		detail := ""
		if task.Status.Message != nil {
			text, err := clia2a.PartsText(task.Status.Message.Parts)
			if err != nil {
				m.appendTransportError(err)
			}
			detail = strings.TrimSpace(sanitize(text))
		}
		m.appendLine(theme.ErrorStyle().Render(failureLine(time.Since(cmp.Or(m.turnStart, m.workStart)), detail)))
	}
	if state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired {
		m.working = false
		// Finished or waiting on the user, either of which is worth a bell when they look away.
		m.attention = true
		m.updateStatus()
	} else if task.Status.Timestamp != nil {
		m.setWorkingTime(*task.Status.Timestamp)
	}
}

// doneLine closes a finished turn; a replayed one, whose length is not recorded, gives zero took.
func doneLine(took time.Duration, tools int, tokens int64) string {
	parts := []string{"Done"}
	if took > 0 {
		parts[0] += " in " + took.Round(time.Second).String()
	}
	if tools > 0 {
		parts = append(parts, plural(tools, "tool"))
	}
	if tokens > 0 {
		parts = append(parts, compactCount(tokens)+" tokens")
	}
	return theme.ReadyStyle().Bold(true).Render("✓") + theme.FaintStyle().Render(" "+strings.Join(parts, " · "))
}

// failureLine is a failed turn's closing line, with the first line of its reason.
func failureLine(took time.Duration, reason string) string {
	line := "✗ Failed"
	if took > 0 {
		line += " after " + took.Round(time.Second).String()
	}
	if first, _, _ := strings.Cut(reason, "\n"); first != "" {
		line += " · " + ansi.Truncate(first, 120, "…")
	}
	return line
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// compactCount writes a count as 950, 1.4k or 12k.
func compactCount(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	default:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
}

// eventTokens is the tokens one event's model call used, from the usage the executor attaches.
func eventTokens(ev a2atype.Event) int64 {
	usage, _ := ev.Meta()[kagenta2a.UsageMetadataKey].(map[string]any)
	// ADK puts event metadata on the artifact, or on the update in its legacy shape.
	if update, ok := ev.(*a2atype.TaskArtifactUpdateEvent); ok && usage == nil && update.Artifact != nil {
		usage, _ = update.Artifact.Metadata[kagenta2a.UsageMetadataKey].(map[string]any)
	}
	number := func(keys ...string) int64 {
		for _, key := range keys {
			switch value := usage[key].(type) {
			case float64:
				return int64(value)
			case int64:
				return value
			case int32:
				return int64(value)
			}
		}
		return 0
	}
	if total := number("totalTokenCount", "total_token_count"); total > 0 {
		return total
	}
	return number("promptTokenCount", "prompt_token_count") + number("candidatesTokenCount", "candidates_token_count")
}

// AppendHistoryTask renders a past task as streaming drew it: its user
// messages, its reply's thoughts, tool calls and text, and its closing line.
func (m *chatModel) AppendHistoryTask(task *a2atype.Task) {
	if task == nil {
		return
	}
	for _, msg := range task.History {
		if msg == nil || msg.Role != a2atype.MessageRoleUser {
			continue
		}
		text, err := clia2a.PartsText(msg.Parts)
		if err != nil {
			m.appendTransportError(err)
			continue
		}
		if strings.TrimSpace(text) != "" {
			m.appendUser(text)
		}
	}
	tools, tokens := 0, int64(0)
	for _, artifact := range task.Artifacts {
		if artifact == nil {
			continue
		}
		tools += m.replayParts(artifact.Parts)
		tokens += eventTokens(&a2atype.TaskArtifactUpdateEvent{Artifact: artifact})
	}
	if usage, ok := kagenta2a.UsageFromMetadata(task.Metadata); ok {
		tokens = usage.TotalTokens
	}
	// A past turn's length is not recorded, so its closing line names none.
	switch task.Status.State {
	case a2atype.TaskStateCompleted:
		m.appendLine(doneLine(0, tools, tokens))
	case a2atype.TaskStateCanceled:
		m.appendLine(theme.AttentionStyle().Render("⊘ Cancelled"))
	case a2atype.TaskStateFailed, a2atype.TaskStateRejected:
		reason := ""
		if task.Status.Message != nil {
			reason, _ = clia2a.PartsText(task.Status.Message.Parts)
		}
		m.appendLine(theme.ErrorStyle().Render(failureLine(0, strings.TrimSpace(sanitize(reason)))))
	}
}

// replayParts draws a past reply's parts in order: thoughts collapsed, tool
// calls with their results, and text as markdown. It returns the tool calls drawn.
func (m *chatModel) replayParts(parts a2atype.ContentParts) int {
	var text []*a2atype.Part
	calls := 0
	flush := func() {
		if answer, err := clia2a.PartsText(text); err != nil {
			m.appendTransportError(err)
		} else if strings.TrimSpace(answer) != "" {
			m.appendBlock(&block{text: answer, markdown: true})
		}
		text = nil
	}
	for _, part := range parts {
		switch {
		case part == nil:
		case kagenta2a.IsThought(part):
			flush()
			at := time.Now()
			m.appendBlock(&block{thought: &thoughtEntry{text: clia2a.ThoughtText(a2atype.ContentParts{part}), start: at, end: at}, expanded: m.expanded})
		case part.Data() != nil:
			flush()
			if activity, ok := kagenta2a.ParseToolActivity(part); ok && activity.Kind == kagenta2a.ToolCallKind {
				calls++
			}
			m.renderToolActivity(a2atype.ContentParts{part})
		default:
			text = append(text, part)
		}
	}
	flush()
	return calls
}

func (m *chatModel) appendUser(text string) {
	m.jumpToLatest() // sending shows the conversation's end
	text = sanitize(text)
	m.appendBlock(&block{text: text, user: true})
	// Past prompts, replayed or sent, are what up recalls; browsing them is not disturbed.
	if m.promptAt == len(m.prompts) {
		m.promptAt++
	}
	m.prompts = append(m.prompts, text)
}

// recallPrompt loads the previous prompt (back) or the next one, ending at the
// unsent draft. Going back starts at the prompt's first row, so up keeps going back.
func (m *chatModel) recallPrompt(back bool) {
	if m.promptAt == len(m.prompts) {
		m.draft = m.input.Value()
	}
	step := 1
	if back {
		step = -1
	}
	m.promptAt += step
	text := m.draft
	if m.promptAt < len(m.prompts) {
		text = m.prompts[m.promptAt]
	}
	m.input.SetValue(text)
	for back && !m.inputAtFirstRow() {
		m.input.CursorUp()
	}
	if back {
		m.input.CursorStart()
	}
	m.fitInput()
}

// appendTransportError reports a stream failure, distinct from a task the agent itself failed.
func (m *chatModel) appendTransportError(err error) {
	m.appendLine(theme.ErrorStyle().Render("Connection error: " + sanitize(err.Error())))
}

// renderToolActivity shows kagent data parts; the reducer has no opinion about them.
func (m *chatModel) renderToolActivity(parts a2atype.ContentParts) {
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

		activity, ok := kagenta2a.ParseToolActivity(part)
		if !ok {
			continue
		}
		if m.tools == nil {
			m.tools = map[string]*toolEntry{}
		}
		switch entry := m.tools[activity.ID]; {
		case activity.Kind == kagenta2a.ToolCallKind:
			m.endThinking()
			args, _ := activity.Args.(map[string]any)
			entry = &toolEntry{started: time.Now(), name: activity.Name, args: args}
			m.turnTools++
			m.tools[activity.ID] = entry
			m.appendBlock(&block{tool: entry, expanded: m.expanded})
		case activity.Kind == kagenta2a.ToolResultKind && entry != nil:
			entry.response, entry.done = activity.Response, true
			m.render()
		case activity.Kind == kagenta2a.ToolResultKind:
			// A result whose call came before this stream shows on its own.
			m.appendBlock(&block{tool: &toolEntry{name: activity.Name, response: activity.Response, done: true}})
		}
	}
}

// toggleTools expands or collapses every tool call's output and every thought.
func (m *chatModel) toggleTools() {
	m.expanded = !m.expanded
	for _, b := range m.blocks {
		if b.tool != nil || b.thought != nil {
			b.expanded = m.expanded
		}
	}
	m.redraw()
}

// commitAgentText closes the in-flight block so another can follow it.
func (m *chatModel) commitAgentText() {
	if m.agentText == "" {
		return
	}
	m.blocks = append(m.blocks, &block{text: m.agentText, markdown: true})
	m.agentText = ""
}

func (m *chatModel) appendLine(s string) {
	m.appendBlock(&block{text: s})
}

func (m *chatModel) appendBlock(b *block) {
	m.commitAgentText()
	m.blocks = append(m.blocks, b)
	m.render()
}

// layout renders the transcript at width: each block, then the agent text
// still streaming. It also reports which lines each block took, for clicks.
func (m *chatModel) layout(width int) (string, []span) {
	if width != m.mdWidth || m.md == nil {
		m.md, _ = markdownRenderer(width) // nil renders markdown raw
		m.liveOut = ""
		m.mdWidth = width
	}
	rendered := make([]string, 0, len(m.blocks)+1)
	var spans []span
	line := 0
	add := func(text string, b *block) {
		text = wrapText(text, width)
		spans = append(spans, span{start: line, end: line + lineCount(text), block: b})
		line += lineCount(text) + 1 // blocks sit a blank line apart
		rendered = append(rendered, text)
	}
	for _, b := range m.blocks {
		switch {
		case b.tool != nil:
			add(colorLinks(renderTool(b.tool, width, b.expanded)), b)
		case b.thought != nil:
			add(renderThought(b.thought, width, b.expanded), b)
		case b.user:
			add(renderUser(b.text, width), b)
		case b.markdown:
			if b.cacheWidth != width || b.cache == "" {
				b.cache, b.cacheWidth = colorLinks(renderMarkdown(m.md, b.text)), width
			}
			add(b.cache, b)
		default:
			add(b.text, b)
		}
	}
	if m.agentText != "" {
		add(colorLinks(m.renderLive()), nil)
	}
	return strings.Join(rendered, "\n\n"), spans
}

// urlPattern matches a web URL up to whitespace or an escape sequence.
var urlPattern = regexp.MustCompile("https?://[^\\s\x1b<>\"'`]+")

// hyperlinks marks each URL as an OSC 8 hyperlink, so Cmd+click opens it even when it wraps.
func hyperlinks(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(url string) string {
		trimmed := strings.TrimRight(url, ".,;:!?)]}")
		return ansi.SetHyperlink(trimmed) + trimmed + ansi.ResetHyperlink() + url[len(trimmed):]
	})
}

// osc8 matches an OSC 8 hyperlink sequence; an empty URL closes the link.
var osc8 = regexp.MustCompile("\x1b\\]8;[^;]*;([^\x1b\a]*)(?:\a|\x1b\\\\)")

// wrapText wraps at spaces but never inside a URL, then hard-wraps rows still too wide.
// Each row closes and reopens its link, so every piece of a wrapped URL opens all of it.
func wrapText(text string, width int) string {
	w := wordwrap.NewWriter(width)
	w.Breakpoints = nil // URLs are full of hyphens
	_, _ = w.Write([]byte(text))
	_ = w.Close()
	lines := strings.Split(ansi.Hardwrap(hyperlinks(w.String()), width, true), "\n")
	open := ""
	for i, line := range lines {
		if open != "" {
			line = ansi.SetHyperlink(open) + line
		}
		for _, match := range osc8.FindAllStringSubmatch(line, -1) {
			open = match[1]
		}
		if open != "" {
			line += ansi.ResetHyperlink()
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// render lays the transcript out again at the current width, less 2 for padding.
func (m *chatModel) render() {
	width := m.vp.Width - 2
	if width <= 0 {
		width = 80
	}
	before := lineCount(m.body)
	m.body, m.spans = m.layout(width)
	if !m.vp.AtBottom() {
		m.unseen += max(lineCount(m.body)-before, 0) // scrolled up, new output counts for the pill
	}
	m.showContent()
}

// showContent puts the transcript and the running turn's status line in the
// viewport, following the end only from the bottom. Status ticks call it
// without laying the transcript out again.
func (m *chatModel) showContent() {
	content := m.body
	if line := m.inlineStatus(); line != "" {
		if content != "" {
			content += "\n\n"
		}
		content += line
	}
	follow := m.vp.AtBottom()
	m.lines = strings.Split(content, "\n")
	m.vp.SetContent(content)
	if follow {
		m.jumpToLatest()
	}
}

// noticeTTL is how long a notice stays above the input.
const noticeTTL = 2 * time.Second

type noticeDoneMsg struct{ id int }

// showNotice puts a short line above the input and clears it after noticeTTL, unless a newer one replaced it.
func (m *chatModel) showNotice(text string) tea.Cmd {
	m.noticeID++
	id := m.noticeID
	m.notice = text
	return tea.Tick(noticeTTL, func(time.Time) tea.Msg { return noticeDoneMsg{id: id} })
}

// noticeRow reports whether the row above the input's bar shows: a notice or the pill.
func (m *chatModel) noticeRow() bool {
	return m.notice != "" || !m.vp.AtBottom()
}

// jumpToLatest scrolls the transcript to its end.
func (m *chatModel) jumpToLatest() {
	m.vp.GotoBottom()
	m.unseen = 0
}

// pillLabel is the jump-to-latest pill's text, or "" at the bottom.
func (m *chatModel) pillLabel() string {
	if m.vp.AtBottom() {
		m.unseen = 0
		return ""
	}
	label := "↓ Jump to latest"
	switch activity, _ := m.activity(); {
	case m.working:
		label = "↓ " + activity // the turn's status line is out of sight
	case m.unseen > 0:
		label = "↓ " + plural(m.unseen, "new line")
	}
	key := "ctrl+end"
	if m.input.Value() == "" {
		key = "end"
	}
	return "[ " + label + " · " + key + " ]"
}

// withPill fits the notice row to width, with the jump-to-latest pill at its right end.
func (m *chatModel) withPill(status string, width int) string {
	label := m.pillLabel()
	if label == "" {
		return ansi.Truncate(status, width, "…") // a wrapped row would push the frame past the window
	}
	m.geo.pill = max(width-ansi.StringWidth(label), 0)
	status = ansi.Truncate(status, max(m.geo.pill-1, 0), "…")
	return status + strings.Repeat(" ", max(m.geo.pill-ansi.StringWidth(status), 0)) + lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render(label)
}

// inlineStatus is the running turn's status line, or "" between turns, whose closing line says how it went.
func (m *chatModel) inlineStatus() string {
	if !m.working {
		return ""
	}
	return theme.StatusStyle().Render(m.spin.View() + " " + m.statusText)
}

// maxQueueRows is how many queued messages show; the rest are counted.
const maxQueueRows = 3

// queueView lists queued messages faintly above the input, each on one line.
func (m *chatModel) queueView(width int) string {
	if len(m.queue) == 0 {
		return ""
	}
	rows := []string{theme.FaintStyle().Render(fmt.Sprintf("⏳ Queued (%d) · ↑ edit", len(m.queue)))}
	if m.queuePaused != "" {
		rows[0] = theme.AttentionStyle().Render(fmt.Sprintf("⏸ Queue paused after a %s turn · Enter to resume · ↑ edit", m.queuePaused))
	}
	for i, text := range m.queue {
		if i == maxQueueRows {
			rows = append(rows, theme.FaintStyle().Render(fmt.Sprintf("  … %d more", len(m.queue)-i)))
			break
		}
		first, _, _ := strings.Cut(sanitize(text), "\n")
		rows = append(rows, theme.FaintStyle().Render(ansi.Truncate("  "+first, max(width, 8), "…")))
	}
	return strings.Join(rows, "\n")
}

// sendQueued sends the next queued message once a turn has ended.
func (m *chatModel) sendQueued() tea.Cmd {
	if len(m.queue) == 0 || m.queuePaused != "" || m.streaming {
		return nil
	}
	text := m.queue[0]
	m.queue = m.queue[1:]
	m.appendUser(text)
	return m.submit(text)
}

// pullQueue empties the queue into the input, after any text already there.
func (m *chatModel) pullQueue() {
	text := strings.Join(m.queue, "\n")
	if current := m.input.Value(); current != "" {
		text = current + "\n" + text
	}
	m.queue, m.queuePaused = nil, ""
	m.input.SetValue(text)
	m.input.CursorEnd()
	m.fitInput()
}

// minTranscriptRows is as far as a long input shrinks the transcript.
const minTranscriptRows = 3

// fitInput grows the input to show every line, and gives the transcript what is left.
func (m *chatModel) fitInput() {
	if m.height <= 0 {
		return
	}
	chrome := 3 + boolInt(m.noticeRow()) + boolInt(m.open) // the header, the rule under it, the bars around the input, and any notice row
	chrome += lineCount(m.queueView(max(m.vp.Width, 10)))
	rows := min(m.inputRows(), max(m.height-chrome-minTranscriptRows, 1))
	bottom := m.vp.AtBottom()
	fits := rows == m.inputRows()
	if rows != m.input.Height() || fits && m.inputCapped {
		m.input.SetHeight(rows)
		// SetHeight leaves the input's scroll alone; View loads its lines, and Update then scrolls to the cursor.
		_ = m.input.View()
		m.input, _ = m.input.Update(nil)
		if fits {
			m.scrollInputToTop()
		}
	}
	m.inputCapped = !fits
	m.vp.Height = max(m.height-chrome-rows, 1)
	if bottom {
		m.vp.GotoBottom()
	}
}

// scrollInputToTop shows the input from its first row, keeping the cursor where it is.
// The textarea scrolls only as far as its cursor needs, so rows that wrapped above
// the cursor before it grew stay hidden until the cursor visits them.
func (m *chatModel) scrollInputToTop() {
	col, ups := m.input.LineInfo().StartColumn+m.input.LineInfo().ColumnOffset, 0
	for ; !m.inputAtFirstRow() && ups <= m.inputRows(); ups++ {
		m.input.CursorUp()
	}
	_ = m.input.View()
	m.input, _ = m.input.Update(nil)
	for range ups {
		m.input.CursorDown()
	}
	m.input.SetCursor(col)
}

// inputAtFirstRow and inputAtLastRow report whether the cursor is on the input's first or last drawn row.
func (m *chatModel) inputAtFirstRow() bool {
	return m.input.Line() == 0 && m.input.LineInfo().RowOffset == 0
}

func (m *chatModel) inputAtLastRow() bool {
	info := m.input.LineInfo()
	return m.input.Line() == m.input.LineCount()-1 && info.RowOffset >= info.Height-1
}

// inputRows is how many rows the input's text wraps to, at least one.
func (m *chatModel) inputRows() int {
	width := max(m.input.Width()-1, 1) // one column spare for the cursor
	rows := 0
	for line := range strings.SplitSeq(m.input.Value(), "\n") {
		rows += lineCount(ansi.Wrap(line, width, "")) + boolInt(line == "")
	}
	return max(rows, 1)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// cleanPaste keeps pasted text's newlines and tabs and drops escape sequences and other control characters.
func cleanPaste(text string) string {
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(ansi.Strip(text))
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
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
		label, since := m.activity()
		m.statusText = fmt.Sprintf("%s %s", label, time.Since(since).Round(time.Second))
	} else {
		m.statusText = "" // the closing line says how the turn went
	}
	m.showContent()
}

// activity is what the turn is doing now, for the status line, and since when.
// Its words follow the web UI's turn phases (Sent, Working…, Answering…);
// thinking and the running tool are the terminal's finer detail.
func (m *chatModel) activity() (string, time.Time) {
	if m.thinking != nil {
		return "Thinking…", m.thinking.thought.start
	}
	for _, b := range slices.Backward(m.blocks) {
		if t := b.tool; t != nil && !t.done {
			return "Running " + sanitize(t.name) + "…", t.started
		}
	}
	switch {
	case m.assembler == nil || m.assembler.Result() == nil:
		return "Sent", m.workStart
	case m.agentText != "":
		return "Answering…", m.workStart
	}
	return "Working…", m.workStart
}
