package egress

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveAccess(t *testing.T) {
	binding := Credential{Origin: "https://model.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team/model/token"}
	base := Access{Destinations: []string{binding.Origin}, Credentials: []Credential{binding}}
	conflict := binding
	conflict.URI = "ate-secret://k8s.io/default/team/other/token"
	tooMany := Access{}
	for i := range 256 {
		tooMany.Destinations = append(tooMany.Destinations, fmt.Sprintf("https://host%d.example.com:443", i))
	}
	for _, test := range []struct {
		name         string
		base, grants Access
		origins      []string
		withheld     bool
	}{
		{name: "no grants", base: base},
		{name: "additive", base: base, grants: Access{Destinations: []string{"https://tickets.example.com:443"}}},
		{name: "sandbox", grants: Access{Destinations: []string{"https://tickets.example.com:443"}}},
		{name: "duplicate", base: base, grants: base},
		{name: "credentials conflict", base: base, grants: Access{Destinations: base.Destinations, Credentials: []Credential{conflict}}, withheld: true},
		{name: "token passthrough", base: Access{Destinations: base.Destinations}, grants: base, origins: base.Destinations, withheld: true},
		{name: "rule limit", base: base, grants: tooMany, withheld: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			access, err := EffectiveAccess(test.base, test.grants, test.origins)
			if test.withheld {
				require.Error(t, err)
				require.Equal(t, test.base, access)
			} else {
				require.NoError(t, err)
				for _, origin := range append(append([]string{}, test.base.Destinations...), test.grants.Destinations...) {
					require.Contains(t, access.Destinations, origin)
				}
			}
		})
	}
}
