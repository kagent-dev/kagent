package experimentaltui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kagent-dev/kagent/go/core/cli/internal/experimental_tui/domain"
	"github.com/kagent-dev/kagent/go/core/cli/internal/experimental_tui/screen"
)

var demoAgents = []domain.Agent{
	{ID: "1", Name: "Agent Alpha hogehogehogehogehogehogehogehogehogehoge", Status: domain.AgentStatusRunning, Provider: NewDemoProvider()},
	{ID: "2", Name: "Agent Beta", Status: domain.AgentStatusError, Provider: NewDemoProvider()},
	{ID: "3", Name: "Agent Gamma", Status: domain.AgentStatusFinished, Provider: NewDemoProvider()},
	{ID: "4", Name: "Agent Delta", Status: domain.AgentStatusStopped, Provider: NewDemoProvider()},
}

func Run() error {
	m := screen.NewRootModel(screen.NewAgentListModel(demoAgents))

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
		os.Exit(1)
	}

	return nil
}
