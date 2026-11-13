package smol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBlockPrivateReachesTheEngine(t *testing.T) {
	for _, c := range []struct {
		spec ResourceSpec
		want string // the block_private member of the native config, "" when absent
	}{
		{ResourceSpec{AllowHosts: []string{"example.com"}}, ""},
		{ResourceSpec{AllowHosts: []string{"example.com"}, BlockPrivate: true}, `"block_private":[]`},
		{ResourceSpec{AllowPrivate: []string{"192.168.1.0/24"}}, `"block_private":["192.168.1.0/24"]`},
	} {
		b, err := json.Marshal(toNativeConfig("m", MachineConfig{Resources: c.spec}))
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		if c.want == "" && strings.Contains(got, "block_private") || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%+v -> %s; want %s", c.spec, got, c.want)
		}
		// Blocking private ranges never turns networking on by itself.
		if c.spec.AllowHosts == nil && strings.Contains(got, `"network":true`) {
			t.Errorf("%+v enabled networking: %s", c.spec, got)
		}
	}
}
