package translator

import (
	"strings"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestRevisionDigestIncludesSandboxClass(t *testing.T) {
	revision := &Revision{Namespace: "agents", AgentName: "helper"}
	original, err := revision.Digest()
	require.NoError(t, err)

	revision.SandboxClass = atev1alpha1.SandboxClassGvisor
	gvisor, err := revision.Digest()
	require.NoError(t, err)
	require.Equal(t, original, gvisor, "explicit gVisor must preserve the existing default revision")
	require.Equal(t, atev1alpha1.SandboxClassGvisor, revision.SandboxClass, "hashing must not mutate the revision")

	revision.SandboxClass = atev1alpha1.SandboxClassMicroVM
	microvm, err := revision.Digest()
	require.NoError(t, err)
	require.NotEqual(t, gvisor, microvm, "changing sandbox class must create a new immutable revision")
	repeated, err := revision.Digest()
	require.NoError(t, err)
	require.Equal(t, microvm, repeated)

	revision.SandboxClass = "unsupported"
	invalid, err := revision.Digest()
	require.EqualError(t, err, `unsupported sandbox class "unsupported"`)
	require.True(t, invalid.IsZero())
}

func TestRevisionDigestIncludesProvenance(t *testing.T) {
	revision := &Revision{Namespace: "agents", AgentName: "helper", Provenance: []byte(`[{"kind":"ConfigMap","hash":"first"}]`)}
	first, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	revision.Provenance = []byte(`[{"kind":"ConfigMap","hash":"second"}]`)
	second, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("configuration change did not change runtime revision")
	}
	if len(first.Short()) != 12 || !strings.HasPrefix(first.String(), first.Short()) {
		t.Fatalf("short revision %q is not a prefix of %q", first.Short(), first.String())
	}
}

func TestRevisionDigestIncludesConfig(t *testing.T) {
	revision := &Revision{Namespace: "agents", AgentName: "helper", ConfigJSON: []byte(`{"version":5,"runtime_telemetry":{"capture_content":false}}`)}
	first, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	revision.ConfigJSON = []byte(`{"version":5,"runtime_telemetry":{"capture_content":true}}`)
	second, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("compiled configuration change did not change runtime revision")
	}
}

func TestCompilationWarningsDoNotAffectRevisionDigest(t *testing.T) {
	compilation := &CompileResult{Revision: Revision{Namespace: "agents", AgentName: "helper"}}
	first, err := compilation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	compilation.Warnings = []string{"partial MCP selection is not enforced"}
	if len(compilation.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one warning", compilation.Warnings)
	}
	second, err := compilation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("non-behavioral warning changed runtime revision")
	}
}

func TestRevisionDigestIncludesCommand(t *testing.T) {
	revision := &Revision{Namespace: "agents", AgentName: "helper", Command: []string{"/agent"}}
	first, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	revision.Command = []string{"/other-agent"}
	second, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("command change did not change runtime revision")
	}
}

func TestRevisionDigestIncludesBinaryAgentCard(t *testing.T) {
	card := &a2apb.AgentCard{Name: "assistant"}
	revision := &Revision{AgentCard: card}
	first, err := revision.Digest()
	require.NoError(t, err)
	card.Name = "changed"
	second, err := revision.Digest()
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	card.ProtoReflect().SetUnknown(protowire.AppendString(protowire.AppendTag(nil, 1000, protowire.BytesType), "future"))
	third, err := revision.Digest()
	require.NoError(t, err)
	require.NotEqual(t, second, third)
	data, err := proto.Marshal(card)
	require.NoError(t, err)
	revision.AgentCard = &a2apb.AgentCard{}
	require.NoError(t, proto.Unmarshal(data, revision.AgentCard))
	roundTrip, err := revision.Digest()
	require.NoError(t, err)
	require.Equal(t, third, roundTrip)
	revision.AgentCard.Name = string([]byte{0xff})
	_, err = revision.Digest()
	require.Error(t, err)
}
