package a2a

import (
	"crypto/sha256"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"google.golang.org/protobuf/proto"
)

// SendRequestHash compares the complete sanitized request, including send
// configuration. Callers normalize context before hashing and assign task IDs
// only after hashing a new input.
func SendRequestHash(request *a2a.SendMessageRequest) ([]byte, error) {
	wire, err := pbconv.ToProtoSendMessageRequest(request)
	if err != nil {
		return nil, err
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(wire)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	return hash[:], nil
}
