package smol

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A proxy listening only on the host's loopback serves the machine through
// MachineConfig.Proxy, and no other port of the host's loopback opens with it.
func TestE2EProxyOnHostLoopback(t *testing.T) {
	requireE2E(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.IsAbs() {
			http.Error(w, "not a proxy request", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "via-proxy %s", r.URL.Host)
	}))
	defer proxy.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "direct")
	}))
	defer other.Close()

	m := newE2EMachine(t, MachineConfig{
		Proxy:     proxy.URL,
		Resources: ResourceSpec{CPUs: 1, MemoryMB: 512, Network: Bool(true)},
	})
	otherPort := other.URL[strings.LastIndex(other.URL, ":")+1:]
	script := fmt.Sprintf(`echo "proxy=$http_proxy"
wget -q -T 10 -O- http://example.invalid/; echo
wget -q -T 3 -Y off -O- http://%s:%s/ || echo other-port-closed`, GuestHostAddress, otherPort)
	r, err := m.Exec(t.Context(), []string{"sh", "-c", script}, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	out := r.Stdout + r.Stderr
	proxyPort := proxy.URL[strings.LastIndex(proxy.URL, ":")+1:]
	wantProxy := "proxy=http://" + GuestHostAddress + ":" + proxyPort
	for _, want := range []string{wantProxy, "via-proxy example.invalid", "other-port-closed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
