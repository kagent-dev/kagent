package e2e_test

import (
	"context"
	"fmt"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/exported"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"strings"
	"time"
)

const (
	GlobalUserID = "admin@kagent.dev"
)

var _ = Describe("E2e", func() {
	It("installs istio, the bookinfo application, and sets up routing", func() {

		// initialize autogen http client
		// assumes fresh cluster setup + port forwarding started
		wsURL := "ws://localhost:8081/api/ws"
		client := autogen_client.New("http://localhost:8081/api", wsURL)

		teams, err := client.ListTeams(GlobalUserID)
		Expect(err).NotTo(HaveOccurred())

		// start a session with the istio agent
		var istioTeam *autogen_client.Team
		for _, team := range teams {
			if team.Component.Label == "istio-agent" {
				istioTeam = team
				break
			}
		}
		Expect(istioTeam).NotTo(BeNil())

		sess, err := client.CreateSession(&autogen_client.CreateSession{
			UserID: GlobalUserID,
			TeamID: istioTeam.Id,
			Name:   "e2e-test-istio-" + time.Now().String(),
		})
		Expect(err).NotTo(HaveOccurred())

		run, err := client.CreateRun(&autogen_client.CreateRunRequest{
			SessionID: sess.ID,
			UserID:    GlobalUserID,
		})
		Expect(err).NotTo(HaveOccurred())

		wsClient, err := exported.NewWebsocketClient(wsURL, run.ID, exported.DefaultConfig)
		Expect(err).NotTo(HaveOccurred())

		testShell := &TestShell{
			Done: make(chan struct{}),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		go func() {
			defer GinkgoRecover()
			err := wsClient.StartInteractive(
				ctx,
				testShell,
				istioTeam,
				`List all the pods in the cluster. When you are finished, end your reply with "Report created successfully"`,
			)
			Expect(err).NotTo(HaveOccurred())
		}()

		// sleep 2s to allow the websocket client to connect
		select {
		case <-testShell.Done:
		case <-ctx.Done():
			Fail("Timed out waiting for the websocket client to finish")
		}

		for _, expectedSubstring := range []string{
			"kagent-",
			"coredns-",
			"coredns-",
			"etcd-kagent-control-plane",
			"kindnet-",
			"kube-apiserver-kagent-control-plane",
			"kube-controller-manager-kagent-control-plane",
			"kube-proxy-",
			"kube-scheduler-kagent-control-plane",
			"local-path-provisioner-",
		} {
			Expect(testShell.OutputText).To(ContainSubstring(expectedSubstring))
		}
	})
})

// test shell simulates a user shell interface
type TestShell struct {
	InputText  []string
	OutputText string
	Done       chan struct{}
	Finished   bool
}

func (t *TestShell) ReadLineErr() (string, error) {
	// pop the first element from the input text
	if len(t.InputText) == 0 {
		return "", nil
	}
	val := t.InputText[0]
	t.InputText = t.InputText[1:]
	return val, nil
}

func (t *TestShell) Println(val ...interface{}) {
	t.OutputText += fmt.Sprintln(val...)
}

func (t *TestShell) Printf(format string, val ...interface{}) {
	t.OutputText += fmt.Sprintf(format, val...)

	fmt.Println(t.OutputText)
	if strings.Contains(t.OutputText, "Report created successfully") && !t.Finished {
		t.Finished = true
		close(t.Done)
	}
}
