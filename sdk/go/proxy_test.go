package smol

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestProxyOnLoopbackOpensItsPortAndSetsTheEnvironment(t *testing.T) {
	cfg, err := withProxy(MachineConfig{Proxy: "http://localhost:7890"})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range proxyVars {
		if cfg.Env[k] != "http://"+GuestHostAddress+":7890" {
			t.Errorf("%s = %q", k, cfg.Env[k])
		}
	}
	if cfg.Env["NO_PROXY"] != defaultNoProxy || cfg.Env["no_proxy"] != defaultNoProxy {
		t.Errorf("NO_PROXY = %q, no_proxy = %q", cfg.Env["NO_PROXY"], cfg.Env["no_proxy"])
	}
	if !slices.Equal(cfg.Resources.HostLoopbackPorts, []uint16{7890}) {
		t.Errorf("HostLoopbackPorts = %v", cfg.Resources.HostLoopbackPorts)
	}
	if cfg.Proxy != "" {
		t.Errorf("Proxy left set: %q", cfg.Proxy)
	}
	native, _ := json.Marshal(toNativeConfig("m", cfg))
	if !strings.Contains(string(native), `"host_loopback_ports":[7890]`) {
		t.Errorf("native config %s", native)
	}
}

func TestProxyKeepsEnvTheCallerSetAndOpensNothingForARemoteProxy(t *testing.T) {
	in := MachineConfig{
		Proxy:     "http://proxy.corp.example:3128",
		Env:       map[string]string{"HTTPS_PROXY": "http://other:1", "no_proxy": ".corp"},
		Resources: ResourceSpec{HostLoopbackPorts: []uint16{9000}},
	}
	cfg, err := withProxy(in)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env["HTTPS_PROXY"] != "http://other:1" || cfg.Env["HTTP_PROXY"] != "http://proxy.corp.example:3128" {
		t.Errorf("env = %v", cfg.Env)
	}
	if _, set := cfg.Env["NO_PROXY"]; set || cfg.Env["no_proxy"] != ".corp" {
		t.Errorf("caller's no_proxy not kept alone: %v", cfg.Env)
	}
	if !slices.Equal(cfg.Resources.HostLoopbackPorts, []uint16{9000}) {
		t.Errorf("HostLoopbackPorts = %v", cfg.Resources.HostLoopbackPorts)
	}
	if len(in.Env) != 2 {
		t.Errorf("caller's Env map was modified: %v", in.Env)
	}
}

func TestProxyOnLoopbackRefusesTSI(t *testing.T) {
	_, err := withProxy(MachineConfig{Proxy: "http://127.0.0.1:7890", Resources: ResourceSpec{NetworkBackend: NetworkBackendTSI}})
	if err == nil {
		t.Fatal("a loopback proxy was accepted with TSI")
	}
}

func TestProxyNeedsAHostAndAPort(t *testing.T) {
	for _, p := range []string{"127.0.0.1", "http://127.0.0.1", "http://:8080", "http://127.0.0.1:0", "http://127.0.0.1:99999"} {
		if _, err := withProxy(MachineConfig{Proxy: p}); err == nil {
			t.Errorf("withProxy(%q) accepted", p)
		}
	}
}
