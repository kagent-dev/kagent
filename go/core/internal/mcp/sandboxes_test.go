package mcp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math/rand/v2"
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
	t.Run("detailed image is shrunk to the result budget", func(t *testing.T) {
		noise := image.NewGray(image.Rect(0, 0, sandboxImageMaxPixels, sandboxImageMaxPixels))
		random := rand.New(rand.NewPCG(1, 2))
		for i := range noise.Pix {
			noise.Pix[i] = uint8(random.Uint32())
		}
		var b bytes.Buffer
		require.NoError(t, png.Encode(&b, noise))
		content := readFile(t, b.Bytes(), 1, 10)
		require.Len(t, content, 2)
		scaled := content[1].(*mcp.ImageContent)
		require.LessOrEqual(t, len(scaled.Data), sandboxImageResultBytes)
		config, _, err := image.DecodeConfig(bytes.NewReader(scaled.Data))
		require.NoError(t, err)
		require.Less(t, config.Width, sandboxImageMaxPixels)
		require.Equal(t, fmt.Sprintf("image/png, 2000×2000 px, shown at %d×%d", config.Width, config.Height), content[0].(*mcp.TextContent).Text)
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
