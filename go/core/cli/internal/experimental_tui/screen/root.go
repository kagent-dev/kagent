package screen

import tea "github.com/charmbracelet/bubbletea"

type PushScreenMessage struct {
	Screen tea.Model
}

type PopScreenMessage struct{}

type RootModel struct {
	stack []tea.Model
}

func NewRootModel(initialScreen tea.Model) *RootModel {
	return &RootModel{
		stack: []tea.Model{initialScreen},
	}
}

func (m RootModel) Init() tea.Cmd {
	if len(m.stack) == 0 {
		return nil
	}

	return m.stack[len(m.stack)-1].Init()
}

func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case PushScreenMessage:
		m.stack = append(m.stack, msg.Screen)
		return m, msg.Screen.Init()
	case PopScreenMessage:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		return m, nil
	}

	if len(m.stack) == 0 {
		return m, nil
	}

	topIndex := len(m.stack) - 1
	top := m.stack[topIndex]

	updatedTop, cmd := top.Update(msg)
	m.stack[topIndex] = updatedTop

	return m, cmd
}

func (m RootModel) View() string {
	if len(m.stack) == 0 {
		return ""
	}
	return m.stack[len(m.stack)-1].View()
}
