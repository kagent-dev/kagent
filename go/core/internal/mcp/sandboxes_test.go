package mcp

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestSandboxToolsRegisteredAndValidateBeforeDispatch(t *testing.T) {
	// A service without dependencies proves invalid input never reaches I/O.
	handler, err := New(testSessionService(), testCheckpointService(),
		&a2asrv.InterceptedHandler{Handler: &fakeGateway{}}, &sandbox.Service{}, nil)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	list := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false)
	registered := map[string]bool{}
	for _, value := range list["result"].(map[string]any)["tools"].([]any) {
		tool := value.(map[string]any)
		registered[tool["name"].(string)] = true
		if name := tool["name"]; name == "read_sandbox_outputs" || name == "read_sandbox_file" {
			require.Nil(t, tool["outputSchema"], name)
		}
	}
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"create_sandbox", map[string]any{"namespace": "team-a", "template": "scratch", "request_id": ""}},
		{"list_sandboxes", map[string]any{"page_size": -1}},
		{"get_sandbox", map[string]any{"sandbox_id": "invalid"}},
		{"suspend_sandbox", map[string]any{"sandbox_id": "invalid"}},
		{"resume_sandbox", map[string]any{"sandbox_id": "invalid"}},
		{"delete_sandbox", map[string]any{"sandbox_id": "invalid"}},
		{"start_sandbox_process", map[string]any{"sandbox_id": "invalid", "command": []string{}}},
		{"get_sandbox_process", map[string]any{"sandbox_id": "invalid", "process_id": "invalid"}},
		{"kill_sandbox_process", map[string]any{"sandbox_id": "invalid", "process_id": "invalid"}},
		{"read_sandbox_outputs", map[string]any{"sandbox_id": "invalid", "process_id": "invalid"}},
		{"read_sandbox_file", map[string]any{"sandbox_id": "invalid", "path": "/data/workspace/file"}},
		{"write_sandbox_file", map[string]any{"sandbox_id": "invalid", "path": "/data/workspace/file", "data_base64": ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.True(t, registered[test.name])
			result := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": test.name, "arguments": test.args}, false)
			require.Nil(t, result["error"], "tool failures must not become protocol errors")
			require.Equal(t, true, result["result"].(map[string]any)["isError"])
		})
	}
}

func TestDecodeText(t *testing.T) {
	for _, test := range []struct {
		name     string
		data     []byte
		text     string
		consumed int
	}{
		{"empty", nil, "", 0},
		{"text", []byte("héllo\n"), "héllo\n", 7},
		{"read cut a rune short", []byte("h\xc3"), "h", 1},
		{"read cut a four-byte rune short", []byte("ok\xf0\x9f\x98"), "ok", 2},
		{"remainder of a cut rune alone", []byte("\xc3"), "\uFFFD", 1},
		{"invalid bytes", []byte("a\xffb"), "a\uFFFDb", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			text, consumed := decodeText(test.data)
			require.Equal(t, test.text, text)
			require.Equal(t, test.consumed, consumed)
		})
	}
}

func TestProcessOutputsRender(t *testing.T) {
	for _, test := range []struct {
		name string
		read processOutputs
		want string
	}{
		{"nothing new", processOutputs{stdoutOffset: 4, stderrOffset: 2}, "stdout_offset=4 stderr_offset=2\nno new output"},
		{"both streams", processOutputs{stdout: []byte("out\n"), stderr: []byte("err\n")},
			"stdout_offset=4 stderr_offset=4\n--- stdout ---\nout\n\n--- stderr ---\nerr\n"},
		{"more to read, holding back a cut rune", processOutputs{stdout: []byte("ab\xc3"), stdoutOffset: 10, more: true},
			"stdout_offset=12 stderr_offset=0 (more output: read again from these offsets)\n--- stdout ---\nab"},
		{"invalid bytes", processOutputs{stderr: []byte("\xff")}, "stdout_offset=0 stderr_offset=1\n--- stderr ---\n\uFFFD"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.read.render())
		})
	}
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewGray(image.Rect(0, 0, width, height))))
	return b.Bytes()
}

func readFile(t *testing.T, data []byte, offset, limit int) []mcp.Content {
	t.Helper()
	reader := fileReader{page: linePage{offset: offset, limit: limit}}
	// Small chunks split lines and runes across writes.
	for chunk := range slices.Chunk(data, 7) {
		if err := reader.write(chunk); err != nil {
			require.ErrorIs(t, err, errReadDone)
			break
		}
	}
	return reader.content()
}

func TestFileReader(t *testing.T) {
	small := encodePNG(t, 3, 2)
	t.Run("image", func(t *testing.T) {
		require.Equal(t, []mcp.Content{&mcp.TextContent{Text: "image/png, 3×2 px"}, &mcp.ImageContent{Meta: sandboxImageMeta, Data: small, MIMEType: "image/png"}}, readFile(t, small, 1, 10))
	})
	t.Run("webp image", func(t *testing.T) {
		webp, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
		require.NoError(t, err)
		require.Equal(t, []mcp.Content{&mcp.TextContent{Text: "image/webp, 1×1 px"}, &mcp.ImageContent{Meta: sandboxImageMeta, Data: webp, MIMEType: "image/webp"}}, readFile(t, webp, 1, 10))
	})
	t.Run("oversized image is scaled down", func(t *testing.T) {
		content := readFile(t, encodePNG(t, 2*sandboxImageMaxPixels, 4), 1, 10)
		require.Len(t, content, 2)
		require.Equal(t, "image/png, 4000×4 px, shown at 2000×2", content[0].(*mcp.TextContent).Text)
		scaled := content[1].(*mcp.ImageContent)
		require.Equal(t, "image/jpeg", scaled.MIMEType)
		config, _, err := image.DecodeConfig(bytes.NewReader(scaled.Data))
		require.NoError(t, err)
		require.Equal(t, image.Config{ColorModel: config.ColorModel, Width: sandboxImageMaxPixels, Height: 2}, config)
	})
	lines := "alpha\nbéta\ngamma\ndelta"
	for _, test := range []struct {
		name          string
		data          []byte
		offset, limit int
		want          string
	}{
		{"image over 16 MiB", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, sandboxImageBytes)...), 1, 10, "image/png image over 16 MiB, too large to read"},
		{"whole text", []byte(lines), 1, 10, "1: alpha\n2: béta\n3: gamma\n4: delta\n"},
		{"first page", []byte(lines), 1, 2, "1: alpha\n2: béta\n(lines 1–2; continue with offset=3)"},
		{"last page", []byte(lines), 3, 2, "3: gamma\n4: delta\n"},
		{"past the end", []byte(lines), 9, 2, "(the file has 4 lines)"},
		{"long line", []byte(strings.Repeat("x", sandboxFileLineBytes+1) + "\nnext\n"), 1, 10,
			"1: " + strings.Repeat("x", sandboxFileLineBytes) + " [line cut]\n2: next\n"},
		{"text beyond the 1 MiB limit", []byte(strings.Repeat("line\n", sandboxToolBytes)), 1, 1, "1: line\n(lines 1–1; continue with offset=2)"},
		{"binary", []byte{0x00, 0xff}, 1, 10, "binary file, application/octet-stream"},
		{"binary beyond its first bytes", make([]byte, sandboxToolBytes+1), 1, 10, "binary file, application/octet-stream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, textContent("%s", test.want), readFile(t, test.data, test.offset, test.limit))
		})
	}
}
