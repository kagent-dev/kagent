package dialogs

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	"github.com/kagent-dev/kagent/go/cli/internal/tui/theme"
)

type wizardStep int

const (
	stepPickType wizardStep = iota
	stepRemoteURL
	stepRemoteHeaders
	stepCommandMethod
	stepCommandMode
	stepCommandDetails
	stepArgsEnv
	stepName
	stepDone
)

// Input field identifiers (typed keys for focus management)
type inputKey int

const (
	inImage inputKey = iota
	inPkg
	inCommand
)

// ServerTypeConfig defines the configuration for a server type (remote or command)
type ServerTypeConfig struct {
	ID          string
	DisplayName string
}

// CommandMethodConfig defines how to run an MCP command server
type CommandMethodConfig struct {
	ID          string
	DisplayName string
}

// CommandModeConfig defines sub-modes for command execution
type CommandModeConfig struct {
	ID          string
	DisplayName string
}

// WizardFlowConfig defines the step sequence and display positions for a flow
type WizardFlowConfig struct {
	Name          string
	StepPositions map[wizardStep]int
	TotalSteps    int
}

// Wizard configuration instances
var (
	// Server types
	serverTypes = struct {
		Remote  ServerTypeConfig
		Command ServerTypeConfig
	}{
		Remote:  ServerTypeConfig{ID: "remote", DisplayName: "Remote"},
		Command: ServerTypeConfig{ID: "command", DisplayName: "Command"},
	}

	// Command execution methods
	commandMethods = struct {
		Image   CommandMethodConfig
		Build   CommandMethodConfig
		Command CommandMethodConfig
	}{
		Image:   CommandMethodConfig{ID: "image", DisplayName: "Docker image (provide a Docker image)"},
		Build:   CommandMethodConfig{ID: "build", DisplayName: "Build \u0028e.g., kmcp.yaml\u0029"},
		Command: CommandMethodConfig{ID: "command", DisplayName: "Command (npx, uvx or another command)"},
	}

	// Command sub-modes (for direct command execution)
	commandModes = struct {
		Custom CommandModeConfig
		Npx    CommandModeConfig
		Uvx    CommandModeConfig
	}{
		Custom: CommandModeConfig{ID: "custom", DisplayName: "Custom"},
		Npx:    CommandModeConfig{ID: "npx", DisplayName: "npx"},
		Uvx:    CommandModeConfig{ID: "uvx", DisplayName: "uvx"},
	}

	// Wizard flows: adjust step positions to reorder the UI
	wizardFlows = struct {
		Command WizardFlowConfig
		Remote  WizardFlowConfig
	}{
		Command: WizardFlowConfig{
			Name: "command",
			StepPositions: map[wizardStep]int{
				stepPickType:       1,
				stepCommandMethod:  2,
				stepCommandMode:    2, // shares position with method
				stepCommandDetails: 3,
				stepArgsEnv:        4,
				stepName:           5,
			},
			TotalSteps: 5,
		},
		Remote: WizardFlowConfig{
			Name: "remote",
			StepPositions: map[wizardStep]int{
				stepPickType:      1,
				stepRemoteURL:     2,
				stepRemoteHeaders: 3,
				stepName:          4,
			},
			TotalSteps: 4,
		},
	}
)

// McpServerWizard provides a paginated wizard for creating MCP server entries.
type McpServerWizard struct {
	id     string
	width  int
	height int

	step   wizardStep
	result common.McpServerType
	ok     bool
	errMsg string

	// page models
	typeList   list.Model
	methodList list.Model

	urlInput     textinput.Model
	imageInput   textinput.Model
	pkgInput     textinput.Model
	commandInput textinput.Model
	argsInput    textinput.Model
	envInput     textinput.Model
	nameInput    textinput.Model
	filePicker   filepicker.Model

	// Headers support for remote servers
	headerKeyInput   textinput.Model
	headerValueInput textinput.Model
	headers          map[string]string

	chosenType   string // serverTypes.Remote.ID or serverTypes.Command.ID
	chosenMethod string // commandMethods.*.ID
	commandMode  string // commandModes.*.ID
	modeList     list.Model
	buildPath    string // stores selected file path from picker
}

func NewMcpServerWizard() *McpServerWizard {
	// Type list
	typeItems := []list.Item{
		choiceItem{"Command (Docker image, local package via npx/uvx, kmcp.yaml)"},
		choiceItem{"Remote (connect to an already running MCP via URL)"},
	}
	tl := list.New(typeItems, choiceDelegate{}, 40, 10)
	tl.Title = "Choose MCP server type"
	tl.SetShowStatusBar(false)
	tl.SetFilteringEnabled(false)
	tl.Styles.Title = lipgloss.NewStyle().Bold(true)
	tl.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(2)

	// Method list
	methodItems := []list.Item{
		choiceItem{commandMethods.Command.DisplayName},
		choiceItem{commandMethods.Image.DisplayName},
		choiceItem{commandMethods.Build.DisplayName},
	}
	ml := list.New(methodItems, choiceDelegate{}, 50, 12)
	ml.Title = "How do you want to run the MCP command?"
	ml.SetShowStatusBar(false)
	ml.SetFilteringEnabled(false)
	ml.Styles.Title = lipgloss.NewStyle().Bold(true)
	ml.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(2)

	mk := func(ph string, w int) textinput.Model {
		ti := textinput.New()
		ti.Prompt = "> "
		ti.Placeholder = ph
		ti.Width = w
		return ti
	}

	// Command sub-mode list (only used when methodCommand is chosen)
	modeItems := []list.Item{
		choiceItem{commandModes.Npx.DisplayName},
		choiceItem{commandModes.Uvx.DisplayName},
		choiceItem{commandModes.Custom.DisplayName},
	}
	mdl := list.New(modeItems, choiceDelegate{}, 30, 10)
	mdl.Title = "Command type"
	mdl.SetShowStatusBar(false)
	mdl.SetFilteringEnabled(false)
	mdl.Styles.Title = lipgloss.NewStyle().Bold(true)

	// File picker for Build method
	fp := filepicker.New()
	fp.ShowHidden = false
	fp.DirAllowed = true
	fp.FileAllowed = true
	cwd, _ := os.Getwd()
	fp.CurrentDirectory = cwd
	fp.SetHeight(10)

	w := &McpServerWizard{
		id:               "mcp_server_wizard",
		step:             stepPickType,
		typeList:         tl,
		methodList:       ml,
		modeList:         mdl,
		urlInput:         mk("https://your-mcp-server", 40),
		imageInput:       mk("ghcr.io/org/tool:tag", 40),
		pkgInput:         mk("@acme/mcp-tool", 40),
		commandInput:     mk("command to execute", 40),
		argsInput:        mk("comma-separated args (optional)", 40),
		envInput:         mk("comma-separated KEY=VALUE (optional)", 40),
		nameInput:        mk("server name", 40),
		filePicker:       fp,
		headerKeyInput:   mk("Header name (e.g., Authorization)", 40),
		headerValueInput: mk("Header value (e.g., Bearer ${API_KEY})", 50),
		headers:          make(map[string]string),
	}
	return w
}

func (w *McpServerWizard) ID() string                   { return w.id }
func (w *McpServerWizard) Fullscreen() bool             { return true }
func (w *McpServerWizard) Ok() bool                     { return w.ok }
func (w *McpServerWizard) Result() common.McpServerType { return w.result }

func (w *McpServerWizard) Init() tea.Cmd {
	return w.filePicker.Init()
}

// Update handles Bubble Tea messages and routes to the current step's components.
func (w *McpServerWizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Always update file picker so it can receive readDirMsg from Init()
	var fpCmd tea.Cmd
	w.filePicker, fpCmd = w.filePicker.Update(msg)

	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		w.width, w.height = m.Width, m.Height
		// Pass sizing into active list
		switch w.step {
		case stepPickType:
			w.typeList.SetSize(maxInt(40, m.Width-20), maxInt(8, m.Height-10))
		case stepCommandMethod:
			w.methodList.SetSize(maxInt(50, m.Width-20), maxInt(10, m.Height-10))
		}
		return w, fpCmd
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			if w.step == stepPickType {
				return w, tea.Quit
			}
			w.errMsg = ""
			w.prevStep()
			return w, nil
		case "q", "ctrl+c":
			return w, tea.Quit
		case "enter":
			return w, w.onEnter()
		case "tab":
			return w, w.onTab(false)
		case "shift+tab":
			return w, w.onTab(true)
		}
	}

	// Delegate updates
	switch w.step {
	case stepPickType:
		var cmd tea.Cmd
		w.typeList, cmd = w.typeList.Update(msg)
		return w, tea.Batch(fpCmd, cmd)
	case stepCommandMethod:
		var cmd tea.Cmd
		w.methodList, cmd = w.methodList.Update(msg)
		return w, tea.Batch(fpCmd, cmd)
	case stepCommandMode:
		var cmd tea.Cmd
		w.modeList, cmd = w.modeList.Update(msg)
		return w, tea.Batch(fpCmd, cmd)
	case stepCommandDetails:
		var cmds []tea.Cmd
		if fpCmd != nil {
			cmds = append(cmds, fpCmd)
		}
		// For Build method, check if user selected a file
		if w.chosenMethod == commandMethods.Build.ID {
			if didSelect, path := w.filePicker.DidSelectFile(msg); didSelect {
				w.buildPath = path
			}
		} else {
			// inputs vary by method; update all but only focused viewed
			w.imageInput, _ = w.imageInput.Update(msg)
			w.pkgInput, _ = w.pkgInput.Update(msg)
			w.commandInput, _ = w.commandInput.Update(msg)
			// if method is Command and we haven't chosen mode yet, allow modeList navigation
			if w.chosenMethod == commandMethods.Command.ID && w.commandMode == "" {
				w.modeList, _ = w.modeList.Update(msg)
			}
		}
		return w, tea.Batch(cmds...)
	case stepRemoteURL:
		var cmd tea.Cmd
		w.urlInput, cmd = w.urlInput.Update(msg)
		return w, tea.Batch(fpCmd, cmd)
	case stepRemoteHeaders:
		var cmds []tea.Cmd
		if fpCmd != nil {
			cmds = append(cmds, fpCmd)
		}
		w.headerKeyInput, _ = w.headerKeyInput.Update(msg)
		w.headerValueInput, _ = w.headerValueInput.Update(msg)
		return w, tea.Batch(cmds...)
	case stepArgsEnv:
		var cmds []tea.Cmd
		if fpCmd != nil {
			cmds = append(cmds, fpCmd)
		}
		w.argsInput, _ = w.argsInput.Update(msg)
		w.envInput, _ = w.envInput.Update(msg)
		return w, tea.Batch(cmds...)
	case stepName:
		var cmd tea.Cmd
		w.nameInput, cmd = w.nameInput.Update(msg)
		return w, tea.Batch(fpCmd, cmd)
	}

	return w, fpCmd
}

// View assembles the frame and delegates step-specific content rendering.
func (w *McpServerWizard) View() string {
	header := w.renderHeader()
	body := ""
	switch w.step {
	case stepPickType:
		body = w.typeList.View()
	case stepRemoteURL:
		body = w.labeled("Remote MCP URL", w.urlInput.View()) + w.errorView()
	case stepRemoteHeaders:
		body = w.renderHeadersStep()
	case stepCommandMethod:
		body = w.methodList.View()
	case stepCommandMode:
		body = w.modeList.View() + w.errorView()
	case stepCommandDetails:
		body = w.renderCommandDetails()
	case stepArgsEnv:
		body = w.labeled("Args", w.argsInput.View()) + "\n" + w.labeled("Env", w.envInput.View()) + w.errorView()
	case stepName:
		body = w.labeled("MCP server name", w.nameInput.View()) + w.errorView()
	case stepDone:
		body = theme.HeadingStyle().Render("Done")
	}

	// Fixed content area height so header stays at same line and help at bottom
	contentTarget := maxInt(12, w.height-10) // target content height inside the box
	headerLines := lineCount(header)
	bodyTarget := maxInt(3, contentTarget-headerLines)
	bodyPadded := lipgloss.NewStyle().Height(bodyTarget).Render(body)

	inner := lipgloss.JoinVertical(lipgloss.Left, header, bodyPadded)

	// Calculate box width: aim for 80% of screen width with reasonable min/max bounds
	boxWidth := min(maxInt(60, (w.width*8)/10), w.width-10)

	box := lipgloss.NewStyle().
		Width(boxWidth).
		Height(contentTarget).
		Padding(1, 2).
		Render(inner)
	return lipgloss.Place(w.width, w.height, lipgloss.Center, lipgloss.Center, box)
}

// onEnter handles the Enter key by delegating to a step-specific handler.
func (w *McpServerWizard) onEnter() tea.Cmd {
	w.errMsg = ""
	switch w.step {
	case stepPickType:
		return w.enterPickType()
	case stepRemoteURL:
		return w.enterRemoteURL()
	case stepRemoteHeaders:
		return w.enterRemoteHeaders()
	case stepCommandMethod:
		return w.enterCommandMethod()
	case stepCommandMode:
		return w.enterCommandMode()
	case stepCommandDetails:
		return w.enterCommandDetails()
	case stepArgsEnv:
		return w.enterArgsEnv()
	case stepName:
		return w.enterName()
	}
	return nil
}

// enterPickType processes selection of the top-level type (Remote or Command).
func (w *McpServerWizard) enterPickType() tea.Cmd {
	if it, ok := w.typeList.SelectedItem().(choiceItem); ok {
		if strings.HasPrefix(it.Title(), "Remote") {
			w.chooseRemoteType()
			return nil
		}
		w.chooseCommandType()
		return nil
	}
	return nil
}

// enterRemoteURL validates the remote URL and advances to headers step.
func (w *McpServerWizard) enterRemoteURL() tea.Cmd {
	u := strings.TrimSpace(w.urlInput.Value())
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		w.errMsg = "URL must start with http:// or https://"
		return nil
	}
	w.result.Type = serverTypes.Remote.ID
	w.result.URL = u
	w.step = stepRemoteHeaders
	w.headerKeyInput.SetValue("")
	w.headerValueInput.SetValue("")
	w.headerKeyInput.Focus()
	return nil
}

// enterRemoteHeaders handles adding headers or skipping to name step.
func (w *McpServerWizard) enterRemoteHeaders() tea.Cmd {
	key := strings.TrimSpace(w.headerKeyInput.Value())
	value := strings.TrimSpace(w.headerValueInput.Value())

	// If both are empty, user wants to skip/finish adding headers
	if key == "" && value == "" {
		w.result.Headers = w.headers
		w.step = stepName
		w.nameInput.SetValue("")
		w.nameInput.Focus()
		return nil
	}

	// Validate that both key and value are provided
	if key == "" {
		w.errMsg = "Header name is required (or leave both empty to continue)"
		return nil
	}
	if value == "" {
		w.errMsg = "Header value is required (or leave both empty to continue)"
		return nil
	}

	// Add the header
	w.headers[key] = value

	// Clear inputs for next header
	w.headerKeyInput.SetValue("")
	w.headerValueInput.SetValue("")
	w.headerKeyInput.Focus()
	w.errMsg = ""

	return nil
}

// enterCommandMethod records the chosen run method and routes to the right detail step.
func (w *McpServerWizard) enterCommandMethod() tea.Cmd {
	if it, ok := w.methodList.SelectedItem().(choiceItem); ok {
		w.enterMethodDetails(it.Title())
		return nil
	}
	return nil
}

// enterCommandMode applies the chosen sub-mode (Custom/npx/uvx) and advances.
func (w *McpServerWizard) enterCommandMode() tea.Cmd {
	if it, ok := w.modeList.SelectedItem().(choiceItem); ok {
		w.applyCommandMode(it.Title())
		return nil
	}
	return nil
}

// enterCommandDetails validates inputs for the chosen method and proceeds.
func (w *McpServerWizard) enterCommandDetails() tea.Cmd {
	if !w.validateCommandDetails() {
		return nil
	}
	// Command is required only for methodCommand (not for Docker image or build)
	if w.chosenMethod == commandMethods.Command.ID && strings.TrimSpace(w.commandInput.Value()) == "" {
		w.errMsg = "Command is required"
		return nil
	}
	w.proceedToArgsEnv()
	return nil
}

// enterArgsEnv advances to naming with a suggested name.
func (w *McpServerWizard) enterArgsEnv() tea.Cmd {
	w.step = stepName
	w.nameInput.SetValue("")
	w.nameInput.Focus()
	return nil
}

// enterName finalizes the result and closes the wizard.
func (w *McpServerWizard) enterName() tea.Cmd {
	nm := strings.TrimSpace(w.nameInput.Value())
	if nm == "" {
		w.errMsg = "Name is required"
		return nil
	}
	w.buildFinalResult(nm)
	w.ok = true
	return w.close()
}

// chooseRemoteType transitions to the remote URL entry step and prepares input.
func (w *McpServerWizard) chooseRemoteType() {
	w.chosenType = serverTypes.Remote.ID
	w.step = stepRemoteURL
	// Clear any stale input value when entering URL step
	w.urlInput.SetValue("")
	w.urlInput.Focus()
}

// chooseCommandType transitions to the command method selection step.
func (w *McpServerWizard) chooseCommandType() {
	w.chosenType = serverTypes.Command.ID
	w.step = stepCommandMethod
}

// enterMethodDetails stores the chosen method and focuses the appropriate input.
func (w *McpServerWizard) enterMethodDetails(displayName string) {
	// Map display name to ID
	switch displayName {
	case commandMethods.Image.DisplayName:
		w.chosenMethod = commandMethods.Image.ID
		w.step = stepCommandDetails
		w.imageInput.SetValue("")
		w.commandInput.SetValue("")
		w.imageInput.Focus()
	case commandMethods.Build.DisplayName:
		w.chosenMethod = commandMethods.Build.ID
		w.step = stepCommandDetails
		w.buildPath = ""
		// File picker will be shown for Build method
	case commandMethods.Command.DisplayName:
		w.chosenMethod = commandMethods.Command.ID
		// Move to a separate step to choose command mode
		w.commandMode = ""
		w.step = stepCommandMode
	}
}

// applyCommandMode configures fields based on the chosen command mode title
// and moves to the details step, focusing the appropriate input.
func (w *McpServerWizard) applyCommandMode(title string) {
	switch title {
	case commandModes.Npx.DisplayName:
		w.commandMode = commandModes.Npx.ID
		w.pkgInput.SetValue("")
		w.commandInput.SetValue("npx")
		w.step = stepCommandDetails
		w.pkgInput.Focus()
	case commandModes.Uvx.DisplayName:
		w.commandMode = commandModes.Uvx.ID
		w.pkgInput.SetValue("")
		w.commandInput.SetValue("uvx")
		w.step = stepCommandDetails
		w.pkgInput.Focus()
	default:
		w.commandMode = commandModes.Custom.ID
		w.commandInput.SetValue("")
		w.step = stepCommandDetails
		w.commandInput.Focus()
	}
}

// interpretModeFromSelection reads the current selection in the mode list and applies it.
// Returns true if a selection was applied.
func (w *McpServerWizard) interpretModeFromSelection() bool {
	if it, ok := w.modeList.SelectedItem().(choiceItem); ok {
		w.applyCommandMode(it.Title())
		return true
	}
	return false
}

// validateCommandDetails validates inputs for the selected method. For Command
// method without a chosen mode, it interprets the selected mode and stays on the page.
// Returns true if validation passes and we can proceed to the next step.
func (w *McpServerWizard) validateCommandDetails() bool {
	switch w.chosenMethod {
	case commandMethods.Image.ID:
		if strings.TrimSpace(w.imageInput.Value()) == "" {
			w.errMsg = "Image is required"
			return false
		}
	case commandMethods.Build.ID:
		if w.buildPath == "" {
			w.errMsg = "Please select a kmcp.yaml file"
			return false
		}
	case commandMethods.Command.ID:
		if w.commandMode == "" {
			// Interpret mode selection on Enter and remain on this step
			_ = w.interpretModeFromSelection()
			return false
		}
	}
	return true
}

// proceedToArgsEnv transitions to the args/env step and prepares inputs.
func (w *McpServerWizard) proceedToArgsEnv() {
	w.step = stepArgsEnv
	w.argsInput.SetValue("")
	w.envInput.SetValue("")
	w.argsInput.Focus()
}

// buildFinalResult constructs the final result object from the wizard state.
func (w *McpServerWizard) buildFinalResult(name string) {
	if w.chosenType == serverTypes.Command.ID {
		w.result.Type = serverTypes.Command.ID
		w.result.Name = name

		cmd := strings.TrimSpace(w.commandInput.Value())
		if cmd != "" {
			w.result.Command = cmd
		}

		switch w.chosenMethod {
		case commandMethods.Image.ID:
			w.result.Image = strings.TrimSpace(w.imageInput.Value())
			w.result.Build = ""
		case commandMethods.Build.ID:
			w.result.Build = w.buildPath
			w.result.Image = ""
		case commandMethods.Command.ID:
			w.result.Image = ""
			w.result.Build = ""
			if w.commandMode == commandModes.Npx.ID || w.commandMode == commandModes.Uvx.ID {
				pkg := strings.TrimSpace(w.pkgInput.Value())
				args := []string{pkg}
				if s := strings.TrimSpace(w.argsInput.Value()); s != "" {
					args = append(args, splitCSV(s)...)
				}
				w.result.Args = args
			}
		}
		if w.chosenMethod != commandMethods.Command.ID || (w.commandMode != commandModes.Npx.ID && w.commandMode != commandModes.Uvx.ID) {
			if s := strings.TrimSpace(w.argsInput.Value()); s != "" {
				w.result.Args = splitCSV(s)
			}
		}
		if s := strings.TrimSpace(w.envInput.Value()); s != "" {
			w.result.Env = splitCSV(s)
		}
		return
	}
	w.result.Type = serverTypes.Remote.ID
	w.result.Name = name
}

// onTab cycles focus within the current step.
func (w *McpServerWizard) onTab(reverse bool) tea.Cmd {
	switch w.step {
	case stepRemoteURL:
		return w.tabRemoteURL(reverse)
	case stepRemoteHeaders:
		return w.tabRemoteHeaders(reverse)
	case stepCommandDetails:
		return w.tabCommandDetails(reverse)
	case stepArgsEnv:
		return w.tabArgsEnv(reverse)
	case stepName:
		return w.tabName(reverse)
	}
	return nil
}

// tabRemoteURL has a single input; nothing to cycle.
func (w *McpServerWizard) tabRemoteURL(_ bool) tea.Cmd { return nil }

// tabRemoteHeaders toggles focus between header key and value inputs.
func (w *McpServerWizard) tabRemoteHeaders(reverse bool) tea.Cmd {
	if reverse {
		if w.headerValueInput.Focused() {
			w.headerKeyInput.Focus()
			w.headerValueInput.Blur()
		} else {
			w.headerKeyInput.Blur()
			w.headerValueInput.Focus()
		}
	} else {
		if w.headerKeyInput.Focused() {
			w.headerKeyInput.Blur()
			w.headerValueInput.Focus()
		} else {
			w.headerValueInput.Blur()
			w.headerKeyInput.Focus()
		}
	}
	return nil
}

// tabCommandDetails cycles across visible detail inputs.
func (w *McpServerWizard) tabCommandDetails(reverse bool) tea.Cmd {
	order := w.detailOrderKeys()
	idx := 0
	for i, k := range order {
		if w.isFocusedKey(k) {
			idx = i
			break
		}
	}
	if reverse {
		idx--
		if idx < 0 {
			idx = len(order) - 1
		}
	} else {
		idx = (idx + 1) % len(order)
	}
	w.focusDetailKey(order[idx])
	return nil
}

// tabArgsEnv toggles focus between args and env.
func (w *McpServerWizard) tabArgsEnv(reverse bool) tea.Cmd {
	if reverse {
		if w.envInput.Focused() {
			w.argsInput.Focus()
			w.envInput.Blur()
		} else {
			w.argsInput.Blur()
			w.envInput.Focus()
		}
	} else {
		if w.argsInput.Focused() {
			w.argsInput.Blur()
			w.envInput.Focus()
		} else {
			w.envInput.Blur()
			w.argsInput.Focus()
		}
	}
	return nil
}

// tabName has a single input; nothing to cycle.
func (w *McpServerWizard) tabName(_ bool) tea.Cmd { return nil }

func (w *McpServerWizard) detailOrderKeys() []inputKey {
	switch w.chosenMethod {
	case commandMethods.Image.ID:
		return []inputKey{inImage, inCommand}
	case commandMethods.Build.ID:
		// Build uses file picker, no text inputs to cycle through
		return []inputKey{}
	case commandMethods.Command.ID:
		return []inputKey{inCommand}
	}
	return []inputKey{inCommand}
}

func (w *McpServerWizard) inputModel(k inputKey) *textinput.Model {
	switch k {
	case inImage:
		return &w.imageInput
	case inPkg:
		return &w.pkgInput
	case inCommand:
		return &w.commandInput
	default:
		return nil
	}
}

func (w *McpServerWizard) isFocusedKey(k inputKey) bool {
	m := w.inputModel(k)
	if m == nil {
		return false
	}
	return m.Focused()
}

func (w *McpServerWizard) focusDetailKey(k inputKey) {
	// blur all
	w.imageInput.Blur()
	w.pkgInput.Blur()
	w.commandInput.Blur()
	if m := w.inputModel(k); m != nil {
		m.Focus()
	}
}

func (w *McpServerWizard) labeled(label, view string) string {
	return lipgloss.JoinHorizontal(lipgloss.Left, theme.StatusStyle().Render(label+": "), view)
}

type detailRow struct{ label, view string }

func (w *McpServerWizard) renderRows(rows []detailRow) string {
	if len(rows) == 0 {
		return w.errorView()
	}
	parts := make([]string, 0, len(rows)+2)
	parts = append(parts, "\n")
	for _, r := range rows {
		parts = append(parts, w.labeled(r.label, r.view))
	}
	parts = append(parts, "\n", w.errorView())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (w *McpServerWizard) renderCommandDetails() string {
	switch w.chosenMethod {
	case commandMethods.Image.ID:
		return w.renderRows([]detailRow{
			{label: "Container image", view: w.imageInput.View()},
			{label: "Command (optional)", view: w.commandInput.View()},
		})
	case commandMethods.Build.ID:
		// Show file picker for Build method
		var content strings.Builder
		content.WriteString("\n")
		if w.buildPath == "" {
			content.WriteString(theme.StatusStyle().Render("Select kmcp.yaml file:"))
		} else {
			content.WriteString(theme.StatusStyle().Render("Selected: ") + w.buildPath)
		}
		content.WriteString("\n\n")
		content.WriteString(w.filePicker.View())
		content.WriteString("\n")
		return content.String()
	case commandMethods.Command.ID:
		if w.commandMode == commandModes.Npx.ID || w.commandMode == commandModes.Uvx.ID {
			return w.renderRows([]detailRow{{label: "Package", view: w.pkgInput.View()}})
		}
		return w.renderRows([]detailRow{{label: "Command", view: w.commandInput.View()}})
	}
	return ""
}

func (w *McpServerWizard) renderHeader() string {
	idx := 1
	var total int
	if w.chosenType == serverTypes.Remote.ID || w.step == stepRemoteURL {
		// remote flow
		if v, ok := wizardFlows.Remote.StepPositions[w.step]; ok {
			idx = v
		}
		total = wizardFlows.Remote.TotalSteps
	} else {
		// command flow (default)
		if v, ok := wizardFlows.Command.StepPositions[w.step]; ok {
			idx = v
		}
		total = wizardFlows.Command.TotalSteps
	}
	title := fmt.Sprintf("Add MCP Server  —  Step %d/%d", idx, total)
	return theme.HeadingStyle().Render(title)
}

func (w *McpServerWizard) errorView() string {
	if strings.TrimSpace(w.errMsg) == "" {
		return ""
	}
	return theme.ErrorStyle().Render("\nError: " + w.errMsg)
}

func (w *McpServerWizard) close() tea.Cmd {
	return tea.Quit
}

// prevStep moves the wizard back by one logical step based on current state.
func (w *McpServerWizard) prevStep() {
	switch w.step {
	case stepRemoteURL:
		w.step = stepPickType
	case stepRemoteHeaders:
		w.step = stepRemoteURL
	case stepCommandMethod:
		w.step = stepPickType
	case stepCommandMode:
		w.step = stepCommandMethod
	case stepCommandDetails:
		if w.chosenMethod == commandMethods.Command.ID {
			w.step = stepCommandMode
		} else {
			w.step = stepCommandMethod
		}
	case stepArgsEnv:
		w.step = stepCommandDetails
	case stepName:
		if w.chosenType == serverTypes.Remote.ID {
			w.step = stepRemoteHeaders
		} else {
			w.step = stepArgsEnv
		}
	default:
		w.step = stepPickType
	}
}

// renderHeadersStep displays the headers input interface with current headers.
func (w *McpServerWizard) renderHeadersStep() string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(theme.StatusStyle().Render("Add HTTP headers (optional)"))
	sb.WriteString("\n\n")

	// Show existing headers
	if len(w.headers) > 0 {
		sb.WriteString(theme.StatusStyle().Render("Current headers:"))
		sb.WriteString("\n")
		for k, v := range w.headers {
			// Mask sensitive values but show pattern
			displayValue := v
			if strings.Contains(strings.ToLower(k), "auth") || strings.Contains(strings.ToLower(k), "token") || strings.Contains(strings.ToLower(k), "key") {
				if len(v) > 10 {
					displayValue = v[:7] + "***"
				}
			}
			sb.WriteString(fmt.Sprintf("  • %s: %s\n", k, displayValue))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(w.labeled("Header name", w.headerKeyInput.View()))
	sb.WriteString("\n")
	sb.WriteString(w.labeled("Header value", w.headerValueInput.View()))
	sb.WriteString("\n\n")
	sb.WriteString(theme.StatusStyle().Render("💡 Tip: Use ${VAR_NAME} for environment variables (e.g., Bearer ${API_KEY})"))
	sb.WriteString("\n")
	sb.WriteString(theme.StatusStyle().Render("   Press Enter with both fields empty to continue"))
	sb.WriteString("\n")
	sb.WriteString(w.errorView())

	return sb.String()
}

// choice list items
type choiceItem struct{ label string }

func (i choiceItem) Title() string       { return i.label }
func (i choiceItem) Description() string { return "" }
func (i choiceItem) FilterValue() string { return i.label }

type choiceDelegate struct{}

func (d choiceDelegate) Height() int                             { return 1 }
func (d choiceDelegate) Spacing() int                            { return 0 }
func (d choiceDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d choiceDelegate) Render(w io.Writer, m list.Model, index int, it list.Item) {
	i, ok := it.(choiceItem)
	if !ok {
		return
	}
	str := fmt.Sprintf("%d. %s", index+1, i.Title())
	normal := lipgloss.NewStyle().PaddingLeft(2)
	selected := lipgloss.NewStyle().PaddingLeft(1).Foreground(theme.ColorPrimary)
	if index == m.Index() {
		_, _ = w.Write([]byte(selected.Render("> " + str)))
	} else {
		_, _ = w.Write([]byte(normal.Render(str)))
	}
}

// lineCount counts lines in a string (>=1 if non-empty)
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// splitCSV splits comma-separated values, trimming whitespace and skipping empties
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// maxInt returns the maximum of two ints
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
