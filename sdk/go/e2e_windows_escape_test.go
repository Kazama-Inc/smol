//go:build windows

package smol

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The mount part of the escape set (kusa-desktop DESIGN 10.4): nothing in the
// guest may read or write host files outside the mounted directory through
// links the host placed inside it. NTFS junctions are the case that matters on
// Windows: pnpm lays node_modules out with them, and `mklink /J` needs no
// privilege, so any workspace can contain one pointing anywhere.
func TestE2EWindowsMountEscapes(t *testing.T) {
	requireE2E(t)
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{filepath.Join(ws, "sub"), filepath.Join(ws, "inner"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(outside, "secret.txt"), "TOPSECRET\n")
	write(filepath.Join(ws, "inner", "file.txt"), "inner\n")

	junction := func(link, target string) {
		t.Helper()
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J %s %s: %v: %s", link, target, err, out)
		}
	}
	junction(filepath.Join(ws, "j_out"), outside)
	junction(filepath.Join(ws, "sub", "j_deep"), outside)
	junction(filepath.Join(ws, "j_in"), filepath.Join(ws, "inner"))
	// Host symlinks need Developer Mode or elevation; test them where possible.
	symlinks := os.Symlink(outside, filepath.Join(ws, "l_out")) == nil &&
		os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(ws, "l_file")) == nil

	m := newE2EMachine(t, MachineConfig{
		Mounts:    []MountSpec{{Source: ws, Target: "/workspace"}},
		Resources: ResourceSpec{CPUs: 1, MemoryMB: 512},
	})
	run := func(script string) string {
		t.Helper()
		r, err := m.Exec(t.Context(), []string{"sh", "-c", script + " 2>&1"}, nil)
		if err != nil {
			t.Fatalf("exec %q: %v", script, err)
		}
		return r.Stdout
	}
	// noSecret fails the test if the output shows the outside file's contents
	// or, for a listing, its name.
	noSecret := func(what, script string) {
		t.Helper()
		out := run(script)
		listing := strings.HasPrefix(script, "ls") || strings.HasPrefix(script, "find")
		if strings.Contains(out, "TOPSECRET") || listing && strings.Contains(out, "secret.txt") {
			t.Errorf("%s reached outside the mount: %q", what, out)
		}
	}

	noSecret("read through a junction", "cat /workspace/j_out/secret.txt")
	noSecret("list through a junction", "ls -la /workspace/j_out/")
	noSecret("read through a nested junction", "cat /workspace/sub/j_deep/secret.txt")
	noSecret("find across junctions", "find /workspace -name 'secret*'")
	run("echo PWNED > /workspace/j_out/pwned.txt; mkdir /workspace/j_out/pwned_dir; " +
		"echo PWNED > /workspace/sub/j_deep/pwned_deep.txt; touch /workspace/j_out/secret.txt")
	for _, name := range []string{"pwned.txt", "pwned_dir", "pwned_deep.txt"} {
		if _, err := os.Lstat(filepath.Join(outside, name)); err == nil {
			t.Errorf("guest created %s outside the mount through a junction", name)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(b) != "TOPSECRET\n" {
		t.Errorf("guest changed a file outside the mount: %q", b)
	}
	// A Linux file name may contain a backslash, which Windows reads as a
	// separator, and a colon: "..\x" or "C:\x" must stay one odd name, not
	// leave the mount. The mount is <root>\ws, so "..\outside" is <root>\outside.
	run(`cd /workspace && echo PWNED > a && mv a '..\outside\renamed.txt'; ` +
		`echo PWNED > b && ln b '..\outside\linked.txt'; ` +
		`echo PWNED > '..\outside\created.txt'; mkdir '..\outside\made_dir'; ` +
		`ln -s x '..\outside\symlinked'`)
	run("cd /workspace && echo PWNED > c && mv c '" + filepath.Join(outside, "absolute.txt") + "'")
	for _, name := range []string{"renamed.txt", "linked.txt", "created.txt", "made_dir", "symlinked", "absolute.txt"} {
		if _, err := os.Lstat(filepath.Join(outside, name)); err == nil {
			t.Errorf("a guest file name with a backslash or a drive reached outside the mount: %s", name)
		}
	}
	// Removing the junction from the guest must not delete what it points at.
	run("rm -rf /workspace/j_out")
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Errorf("rm -rf of a junction in the guest deleted its target's contents: %v", err)
	}
	if symlinks {
		noSecret("read through a host directory symlink", "cat /workspace/l_out/secret.txt")
		noSecret("read through a host file symlink", "cat /workspace/l_file")
	}

	// A junction that stays inside the mount keeps working, as a symlink.
	if out := run("cat /workspace/j_in/file.txt"); out != "inner\n" {
		t.Errorf("junction inside the mount: %q; want its target's file", out)
	}
	if out := run("test -L /workspace/j_in && echo link"); strings.TrimSpace(out) != "link" {
		t.Errorf("junction inside the mount is not shown as a symlink: %q", out)
	}
}
