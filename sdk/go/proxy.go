package smol

import (
	"maps"
	"net"
	"net/url"
	"slices"
	"strconv"
)

// GuestHostAddress is where a machine reaches the host's loopback ports
// listed in ResourceSpec.HostLoopbackPorts: its virtio-net gateway, which the
// engine dials as host loopback (100.96.0.1 is the engine's default guest
// subnet gateway; the SDK does not change the subnet).
const GuestHostAddress = "100.96.0.1"

// The variables MachineConfig.Proxy sets, and what NO_PROXY defaults to.
var (
	proxyVars      = []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"}
	noProxyVars    = []string{"NO_PROXY", "no_proxy"}
	defaultNoProxy = "localhost,127.0.0.1,::1"
)

// withProxy expands cfg.Proxy into the machine's environment and, for a proxy
// on the host's loopback, into Resources.HostLoopbackPorts with the address
// rewritten to GuestHostAddress. Variables cfg.Env already sets are kept. The
// returned config no longer has Proxy set.
func withProxy(cfg MachineConfig) (MachineConfig, error) {
	if cfg.Proxy == "" {
		return cfg, nil
	}
	u, err := url.Parse(cfg.Proxy)
	if err != nil || u.Hostname() == "" || u.Port() == "" {
		return cfg, invalidConfig("Proxy must be a URL with a host and a port, for example http://127.0.0.1:7890.")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || port == 0 {
		return cfg, invalidConfig("Proxy has an invalid port: " + u.Port() + ".")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); host == "localhost" || ip != nil && ip.IsLoopback() {
		if cfg.Resources.NetworkBackend == NetworkBackendTSI {
			return cfg, invalidConfig("Proxy on the host's loopback needs the virtio-net backend, not TSI.")
		}
		u.Host = net.JoinHostPort(GuestHostAddress, u.Port())
		if !slices.Contains(cfg.Resources.HostLoopbackPorts, uint16(port)) {
			cfg.Resources.HostLoopbackPorts = append(slices.Clip(cfg.Resources.HostLoopbackPorts), uint16(port))
		}
	}
	env := maps.Clone(cfg.Env)
	if env == nil {
		env = map[string]string{}
	}
	for _, k := range proxyVars {
		if _, set := env[k]; !set {
			env[k] = u.String()
		}
	}
	_, upper := env["NO_PROXY"]
	_, lower := env["no_proxy"]
	if !upper && !lower {
		for _, k := range noProxyVars {
			env[k] = defaultNoProxy
		}
	}
	cfg.Env = env
	cfg.Proxy = ""
	return cfg, nil
}
