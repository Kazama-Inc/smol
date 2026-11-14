//go:build windows

package smol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A non-root workload can write to the workspace mount. A Windows host has no
// Unix owner to give, so the share used to reach the guest as root:root and
// only root could write to it. The engine now asks libkrun to report it as
// uid 1000, and the agent maps that onto the workload user the way it maps a
// macOS or Linux host user: through an id-mapped mount made when the image
// workload starts, for the machine's user (MachineConfig.User or the image's
// USER). A per-command ExecOptions.User other than that one is not mapped.
func TestE2EWindowsMountNonRoot(t *testing.T) {
	requireE2E(t)
	for _, tc := range []struct{ name, machineUser, execUser string }{
		{"uid 1000, no mapping needed", "", "1000:1000"},
		{"machine user 1001, id-mapped", "1001:1001", "1001:1001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
				User:      tc.machineUser,
			})
			script := strings.Join([]string{
				"set -e",
				"echo guest > /workspace/new.txt",
				"mkdir /workspace/dir",
				"echo more >> /workspace/from-host.txt",
				"echo deep > /workspace/sub/deep.txt",
				`test "$(stat -c %u /workspace)" = "$(id -u)"`,
				`test "$(stat -c %u /workspace/new.txt)" = "$(id -u)"`,
				"id -u",
			}, "; ")
			r, err := m.Exec(t.Context(), []string{"sh", "-c", script + " 2>&1"}, &ExecOptions{User: tc.execUser})
			if err != nil || !r.Success() {
				t.Fatalf("exec: %+v, %v", r, err)
			}
			if want := strings.SplitN(tc.execUser, ":", 2)[0]; strings.TrimSpace(r.Stdout) != want {
				t.Fatalf("ran as %q, want %s", r.Stdout, want)
			}
			for _, f := range []string{"new.txt", "dir", filepath.Join("sub", "deep.txt")} {
				if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
					t.Errorf("host does not see %s: %v", f, err)
				}
			}
			if b, _ := os.ReadFile(filepath.Join(dir, "from-host.txt")); string(b) != "host\nmore\n" {
				t.Errorf("from-host.txt = %q", b)
			}

			// Root still writes, and what it creates is root's.
			r, err = m.Exec(t.Context(), []string{"sh", "-c", "echo r > /workspace/by-root && stat -c %u /workspace/by-root"}, &ExecOptions{User: "0"})
			if err != nil || strings.TrimSpace(r.Stdout) != "0" {
				t.Fatalf("as root: %+v, %v", r, err)
			}
		})
	}
}
