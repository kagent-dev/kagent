package dialogs

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Dialog represents a modal-like component.
type Dialog interface {
	tea.Model
	ID() string
	// Fullscreen indicates whether the dialog should take the entire screen.
	Fullscreen() bool
}

// OpenMsg opens the provided dialog.
type OpenMsg struct{ Model Dialog }

// CloseMsg closes the topmost dialog.
type CloseMsg struct{}

// Manager maintains a stack of dialogs and forwards messages to the active one.
type Manager struct {
	width, height int
	stack         []Dialog
}

func NewManager() *Manager { return &Manager{stack: make([]Dialog, 0, 2)} }

func (m *Manager) HasDialogs() bool { return len(m.stack) > 0 }

func (m *Manager) Active() Dialog {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

// Handle processes messages and routes them to the dialog stack. It returns a Cmd to be run by Bubble Tea.
func (m *Manager) Handle(msg tea.Msg) tea.Cmd {
	switch t := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = t.Width, t.Height
		// propagate to active dialog for reflow
		if d := m.Active(); d != nil {
			u, cmd := d.Update(msg)
			if dd, ok := u.(Dialog); ok {
				m.stack[len(m.stack)-1] = dd
			}
			return cmd
		}
		return nil
	case OpenMsg:
		// do not stack duplicates as topmost
		if d := m.Active(); d != nil && d.ID() == t.Model.ID() {
			return nil
		}
		m.stack = append(m.stack, t.Model)
		// initialize and send size immediately
		initCmd := t.Model.Init()
		_, sizeCmd := t.Model.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		if initCmd != nil && sizeCmd != nil {
			return tea.Batch(initCmd, sizeCmd)
		}
		if initCmd != nil {
			return initCmd
		}
		return sizeCmd
	case CloseMsg:
		if len(m.stack) == 0 {
			return nil
		}
		m.stack = m.stack[:len(m.stack)-1]
		return nil
	}

	if d := m.Active(); d != nil {
		u, cmd := d.Update(msg)
		if dd, ok := u.(Dialog); ok {
			m.stack[len(m.stack)-1] = dd
		}
		return cmd
	}
	return nil
}

// ViewOverlay returns the view of the active dialog if present, otherwise empty string.
func (m *Manager) ViewOverlay() (string, bool) {
	if d := m.Active(); d != nil {
		return d.View(), true
	}
	return "", false
}
