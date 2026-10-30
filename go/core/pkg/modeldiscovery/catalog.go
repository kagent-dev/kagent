// Package modeldiscovery exposes the read-only model inventory to application consumers.
package modeldiscovery

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// Snapshots and their model records use Kagent-owned, typed OpenAI-compatible metadata.
// Returned values belong to the caller; changing them never changes the catalog.
type Snapshot struct {
	Ref         *apiv1.ResourceReference
	Uid         string
	Generation  int64
	Type        string
	Endpoint    string
	State       apiv1.DiscoveryState
	Error       string
	LastSuccess *timestamppb.Timestamp
	Models      []*Model
}

// Clone returns an independent copy, including optional model metadata.
func (s *Snapshot) Clone() *Snapshot {
	if s == nil {
		return nil
	}
	result := *s
	if s.Ref != nil {
		result.Ref = proto.Clone(s.Ref).(*apiv1.ResourceReference)
	}
	if s.LastSuccess != nil {
		result.LastSuccess = proto.Clone(s.LastSuccess).(*timestamppb.Timestamp)
	}
	if s.Models != nil {
		result.Models = make([]*Model, len(s.Models))
		for i, model := range s.Models {
			if model != nil {
				result.Models[i] = proto.Clone(model).(*Model)
			}
		}
	}
	return &result
}

type Model = apiv1.DiscoveredModel

// Reader supplies local inventory. Changes is a coalesced notification: subscribe
// before listing, then reread after every notification. It includes invalidation,
// deletion and initial synchronization, not just changes to model entries.
// Cancel releases the subscription. Consumers must not infer empty inventory
// until Synced is true and each source reports CURRENT.
type Reader interface {
	Get(types.NamespacedName) *Snapshot
	List() []*Snapshot
	Synced() bool
	Subscribe() (changes <-chan struct{}, cancel func())
}
