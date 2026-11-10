package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// Exercise the runtime configuration through agent construction and the runner,
// inspecting the tools actually advertised to the model for each agent.
func TestBuiltinToolsIndependentOfSkills(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	for _, skillsMode := range []string{"none", "empty", "installed"} {
		for _, disableBash := range []bool{false, true} {
			for _, disableFileSearch := range []bool{false, true} {
				t.Run(fmt.Sprintf("skills=%s/disable_bash=%t/disable_file_search=%t", skillsMode, disableBash, disableFileSearch), func(t *testing.T) {
					requests := make(chan []string, 2)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var request struct {
							Tools []struct {
								Function struct {
									Name string `json:"name"`
								} `json:"function"`
							} `json:"tools"`
						}
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Errorf("decode model request: %v", err)
							http.Error(w, "invalid request", http.StatusBadRequest)
							return
						}
						var names []string
						for _, tool := range request.Tools {
							names = append(names, tool.Function.Name)
						}
						requests <- names
						w.Header().Set("Content-Type", "application/json")
						_, err := w.Write([]byte(`{"id":"test","object":"chat.completion","model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
						if err != nil {
							t.Errorf("write model response: %v", err)
						}
					}))
					t.Cleanup(server.Close)

					var skillsDir string
					if skillsMode != "none" {
						skillsDir = t.TempDir()
					}
					if skillsMode == "installed" {
						require.NoError(t, os.Mkdir(filepath.Join(skillsDir, "demo"), 0755))
						require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "demo", "SKILL.md"), []byte("---\nname: demo\ndescription: Demo skill.\n---\nRead files.\n"), 0644))
					}

					model := &configv1.ADKModelConfig{Model: "gpt-4o", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{BaseUrl: server.URL}}}
					cfg := &configv1.ADKConfig{
						Runtime: &configv1.ADKRuntimeConfig{BuiltinTools: &configv1.BuiltinToolsConfig{DisableBash: disableBash, DisableFileSearch: disableFileSearch}},
						Agent: &configv1.ADKAgentConfig{Model: model, SkillsDirectory: skillsDir, Stream: new(false),
							SubAgents: []*configv1.ADKAgentConfig{{Name: "child", Model: model, SkillsDirectory: skillsDir, Stream: new(false)}},
						},
					}
					if !disableBash && !disableFileSearch {
						cfg.Runtime = nil // Exercise absent tool configuration defaults.
					}
					root, err := CreateGoogleADKAgent(t.Context(), cfg, "root", nil)
					require.NoError(t, err)
					for _, agent := range []adkagent.Agent{root, root.SubAgents()[0]} {
						t.Run(agent.Name(), func(t *testing.T) {
							sessions := adksession.InMemoryService()
							r, err := runner.New(runner.Config{AppName: "test", Agent: agent, SessionService: sessions})
							require.NoError(t, err)
							sess, err := sessions.Create(t.Context(), &adksession.CreateRequest{AppName: "test", UserID: "user"})
							require.NoError(t, err)
							for _, err := range r.Run(t.Context(), "user", sess.Session.ID(), genai.NewContentFromText("List your tools.", genai.RoleUser), adkagent.RunConfig{}) {
								require.NoError(t, err)
							}
							select {
							case names := <-requests:
								for _, name := range []string{"read_file", "write_file", "edit_file"} {
									require.Contains(t, names, name)
								}
								for _, name := range []string{"load_skill", "load_skill_resource", "list_skills"} {
									require.Equal(t, skillsMode == "installed", slices.Contains(names, name), name)
								}
								require.Equal(t, !disableBash, slices.Contains(names, "bash"))
								for _, name := range []string{"list_files", "grep_file"} {
									require.Equal(t, !disableFileSearch, slices.Contains(names, name), name)
								}
							case <-time.After(time.Second):
								t.Fatal("model received no request")
							}
						})
					}
				})
			}
		}
	}
}
