package smol

import (
	"reflect"
	"testing"
)

// AllowList hands its host entries to the engine as written: the engine gives
// github.com, *.github.com and =github.com the same meaning they have in
// ResourceSpec.AllowHosts, so the SDK must not rewrite them.
func TestAllowListHostsReachTheEngineAsWritten(t *testing.T) {
	hosts := []string{"github.com", "*.example.com", "=api.example.org"}
	network, cidrs, got, err := egressPolicy(AllowList(hosts, []string{"10.0.0.0/8"}))
	if err != nil {
		t.Fatal(err)
	}
	if !network || !reflect.DeepEqual(got, hosts) || !reflect.DeepEqual(cidrs, []string{"10.0.0.0/8"}) {
		t.Fatalf("network %v, cidrs %v, hosts %v", network, cidrs, got)
	}
	got[0] = "changed"
	if hosts[0] != "github.com" {
		t.Fatal("the engine's copy aliases the caller's slice")
	}

	// An allow list naming nothing allows nothing rather than everything.
	_, cidrs, got, err = egressPolicy(AllowList(nil, nil))
	if err != nil || len(got) != 0 || !reflect.DeepEqual(cidrs, []string{"127.0.0.0/8", "::1/128"}) {
		t.Fatalf("empty allow list: cidrs %v, hosts %v, err %v", cidrs, got, err)
	}
}
