package mcp

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

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
		if tool["name"] == "read_sandbox_file" {
			require.Nil(t, tool["outputSchema"])
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

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewGray(image.Rect(0, 0, width, height))))
	return b.Bytes()
}

func readFile(t *testing.T, data []byte, offset, limit int) []mcp.Content {
	t.Helper()
	// Byte-at-a-time reads split lines and runes across reads.
	content, err := readFileContent(iotest.OneByteReader(bytes.NewReader(data)), offset, limit)
	require.NoError(t, err)
	return content
}

func TestReadFileContent(t *testing.T) {
	small := encodePNG(t, 3, 2)
	t.Run("read error", func(t *testing.T) {
		failure := errors.New("guest unavailable")
		_, err := readFileContent(io.MultiReader(strings.NewReader("alpha\n"), iotest.ErrReader(failure)), 1, 10)
		require.ErrorIs(t, err, failure)
	})
	t.Run("image", func(t *testing.T) {
		require.Equal(t, []mcp.Content{&mcp.ImageContent{Data: small, MIMEType: "image/png"}}, readFile(t, small, 1, 10))
	})
	lines := "alpha\nbéta\ngamma\ndelta"
	for _, test := range []struct {
		name          string
		data          []byte
		offset, limit int
		want          string
	}{
		{"image over the limit", append(small, make([]byte, maxSandboxImageBytes)...), 1, 10, "Image exceeds maximum allowed size (10485760 bytes). Write a smaller copy in the sandbox and read that instead."},
		{"whole text", []byte(lines), 1, 10, "1\talpha\n2\tbéta\n3\tgamma\n4\tdelta\n"},
		{"first page", []byte(lines), 1, 2, "1\talpha\n2\tbéta\n(lines 1–2; continue with offset=3)"},
		{"last page", []byte(lines), 3, 2, "3\tgamma\n4\tdelta\n"},
		{"past the end", []byte(lines), 9, 2, "(the file has 4 lines)"},
		{"long line", []byte(strings.Repeat("x", maxSandboxLineBytes+1) + "\nnext\n"), 1, 10,
			"1\t" + strings.Repeat("x", maxSandboxLineBytes) + " [line cut]\n2\tnext\n"},
		{"text beyond the 1 MiB limit", []byte(strings.Repeat("line\n", sandboxToolBytes)), 1, 1, "1\tline\n(lines 1–1; continue with offset=2)"},
		{"line that only fits", []byte(strings.Repeat("x", maxSandboxLineBytes) + "\nnext\n"), 1, 10,
			"1\t" + strings.Repeat("x", maxSandboxLineBytes) + "\n2\tnext\n"},
		{"crlf line endings", []byte("alpha\r\nbeta\r\n"), 1, 10, "1\talpha\n2\tbeta\n"},
		{"empty file", nil, 1, 10, "(the file has 0 lines)"},
		{"binary", []byte{0x00, 0xff}, 1, 10, "binary file, application/octet-stream"},
		{"binary beyond its first bytes", make([]byte, sandboxToolBytes+1), 1, 10, "binary file, application/octet-stream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, textContent("%s", test.want), readFile(t, test.data, test.offset, test.limit))
		})
	}
}
