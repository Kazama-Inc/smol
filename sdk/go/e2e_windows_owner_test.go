//go:build windows

package smol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A command running as a non-root user can write to the workspace mount. A
// Windows host has no Unix owner to give, so the share used to reach the
// guest as root:root and only root could write to it; the engine now asks
// libkrun for a non-root owner, which the agent maps onto the workload user.
func TestE2EWindowsMountNonRoot(t *testing.T) {
	requireE2E(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "from-host.txt"), []byte("host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newE2EMachine(t, MachineConfig{
		Mounts:    []MountSpec{{Source: dir, Target: "/workspace"}},
		Resources: ResourceSpec{CPUs: 1, MemoryMB: 512},
	})
	for _, user := range []string{"1000:1000", "nobody"} {
		name := strings.NewReplacer(":", "-").Replace(user)
		script := strings.Join([]string{
			"set -e",
			"echo guest > /workspace/new-" + name,
			"mkdir /workspace/dir-" + name,
			"echo more >> /workspace/from-host.txt",
			"echo deep > /workspace/sub/deep-" + name,
			"test \"$(stat -c %u /workspace)\" = \"$(id -u)\"",
			"test \"$(stat -c %u /workspace/new-" + name + ")\" = \"$(id -u)\"",
			"id -u",
		}, "; ")
		r, err := m.Exec(t.Context(), []string{"sh", "-c", script + " 2>&1"}, &ExecOptions{User: user})
		if err != nil || !r.Success() {
			t.Fatalf("as %s: %+v, %v", user, r, err)
		}
		if uid := strings.TrimSpace(r.Stdout); uid == "0" {
			t.Fatalf("as %s: ran as root", user)
		}
		for _, f := range []string{"new-" + name, filepath.Join("sub", "deep-"+name)} {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Errorf("as %s: host does not see %s: %v", user, f, err)
			}
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "from-host.txt")); string(b) != "host\nmore\nmore\n" {
		t.Errorf("from-host.txt = %q", b)
	}

	// Root still writes everywhere, and what it creates is root's.
	r, err := m.Exec(t.Context(), []string{"sh", "-c", "echo r > /workspace/by-root && stat -c %u /workspace/by-root"}, nil)
	if err != nil || strings.TrimSpace(r.Stdout) != "0" {
		t.Fatalf("as root: %+v, %v", r, err)
	}
}
