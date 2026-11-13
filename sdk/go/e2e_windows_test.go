//go:build windows

package smol

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows-only end-to-end tests. Besides SMOL_E2E=1 they want WHP enabled,
// SMOLMACHINES_RUNTIME_DIR pointing at an unpacked windows-x64 bundle and,
// to run without network access, SMOL_E2E_IMAGE set to a `docker save`
// archive of a small image with busybox (alpine works):
//
//	crane pull --platform linux/amd64 alpine:3.20 alpine-3.20.tar
//
// A process launched from an MSIX-packaged app inherits its AppData
// redirection, under which AF_UNIX sockets in %LOCALAPPDATA% cannot connect;
// run these from a plain terminal, or point SMOLVM_DATA_DIR at a short
// directory outside AppData.

func TestE2EWindowsHostProbe(t *testing.T) {
	requireE2E(t)
	if a := CheckLocal(); a.Cause != "" {
		t.Fatalf("available host reports cause %q", a.Cause)
	}
}

// A machine created from a host-side archive boots with no network at all.
func TestE2EWindowsLocalArchiveWithoutNetwork(t *testing.T) {
	requireE2E(t)
	image := os.Getenv("SMOL_E2E_IMAGE")
	if image == "" {
		t.Skip("set SMOL_E2E_IMAGE to a docker save archive")
	}
	m := newE2EMachine(t, MachineConfig{Image: image, Resources: ResourceSpec{CPUs: 1, MemoryMB: 512, Network: Bool(false)}})
	r, err := m.Exec(t.Context(), []string{"sh", "-c", "uname -s; test -f /etc/os-release && echo has-os-release"}, nil)
	if err != nil || r.Stdout != "Linux\nhas-os-release\n" {
		t.Fatalf("exec = %q, %v (stderr %q)", r.Stdout, err, r.Stderr)
	}
	r, err = m.Exec(t.Context(), []string{"sh", "-c", "wget -q -T 3 -O /dev/null http://example.com && echo online || echo offline"}, nil)
	if err != nil || strings.TrimSpace(r.Stdout) != "offline" {
		t.Fatalf("network probe = %q, %v; want offline", r.Stdout, err)
	}
}

// Output streams as it is produced, not when the command ends.
func TestE2EWindowsStreamIsIncremental(t *testing.T) {
	requireE2E(t)
	m := newE2EMachine(t, MachineConfig{Resources: ResourceSpec{CPUs: 1, MemoryMB: 512}})
	start := time.Now()
	var arrivals []time.Duration
	for ev, err := range m.ExecStream(t.Context(), []string{"sh", "-c", "for i in 1 2 3; do echo line$i; sleep 0.5; done"}, nil) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		if ev.Kind == EventStdout {
			arrivals = append(arrivals, time.Since(start))
		}
	}
	if len(arrivals) < 2 {
		t.Fatalf("got %d stdout chunks; want one per line", len(arrivals))
	}
	if spread := arrivals[len(arrivals)-1] - arrivals[0]; spread < 600*time.Millisecond {
		t.Fatalf("chunks arrived within %s of each other; output was buffered", spread)
	}
}

// The workspace mount is shared both ways.
func TestE2EWindowsMount(t *testing.T) {
	requireE2E(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "from-host.txt"), []byte("host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newE2EMachine(t, MachineConfig{
		Mounts:    []MountSpec{{Source: dir, Target: "/workspace"}},
		Resources: ResourceSpec{CPUs: 1, MemoryMB: 512},
	})
	r, err := m.Exec(t.Context(), []string{"sh", "-c", "cat /workspace/from-host.txt && echo guest > /workspace/from-guest.txt"}, nil)
	if err != nil || r.Stdout != "host\n" || !r.Success() {
		t.Fatalf("exec = %+v, %v", r, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "from-guest.txt")); err != nil || string(b) != "guest\n" {
		t.Fatalf("host sees %q, %v", b, err)
	}
}

// Killing the owning process outright (no handler runs) still takes its
// machines down: the job object reaps the boot helper.
func TestE2EWindowsMachineDiesWithOwner(t *testing.T) {
	requireE2E(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestE2EWindowsOwnerHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "SMOL_E2E_OWNER_HELPER=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	ready := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "OWNER-READY "); ok {
				ready <- name
			}
		}
		close(ready)
	}()
	var name string
	select {
	case name = <-ready:
	case <-time.After(3 * time.Minute):
		t.Fatal("owner helper did not create its machine")
	}
	if name == "" {
		t.Fatal("owner helper exited before creating its machine")
	}
	helpers := descendants(t, uint32(cmd.Process.Pid), "smol-vmm.exe")
	if len(helpers) == 0 {
		t.Fatal("no smol-vmm.exe under the owner process")
	}
	if err := cmd.Process.Kill(); err != nil { // TerminateProcess: no handler runs
		t.Fatal(err)
	}
	// The job object kills the helper at once. Without it (SMOLMACHINES_NO_JOB)
	// only the engine's parent watchdog is left, which first gives the guest
	// up to 20 seconds to shut down cleanly.
	limit := 10 * time.Second
	if envFlag("SMOLMACHINES_NO_JOB") {
		limit = 30 * time.Second
	}
	killed := time.Now()
	for _, pid := range helpers {
		if !waitExit(pid, limit-time.Since(killed)) {
			t.Fatalf("smol-vmm.exe %d still running %s after its owner was killed", pid, limit)
		}
	}
	t.Logf("boot helpers gone %s after the owner was killed", time.Since(killed).Round(10*time.Millisecond))
	// The machine is gone with it; clean its record up.
	if m, err := Connect(context.Background(), name, &ConnectOptions{Target: TargetLocal, NoSignalHandling: true}); err == nil {
		_ = m.Delete(context.Background())
	}
}

// TestE2EWindowsOwnerHelper is the owner process for the test above.
func TestE2EWindowsOwnerHelper(t *testing.T) {
	if os.Getenv("SMOL_E2E_OWNER_HELPER") != "1" {
		t.Skip("helper process for TestE2EWindowsMachineDiesWithOwner")
	}
	cfg := MachineConfig{Image: os.Getenv("SMOL_E2E_IMAGE"), Resources: ResourceSpec{CPUs: 1, MemoryMB: 512}}
	m, err := Create(context.Background(), cfg, &ConnectOptions{Target: TargetLocal})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	fmt.Printf("OWNER-READY %s\n", m.Name())
	time.Sleep(10 * time.Minute)
}

// descendants lists the pids of processes named exe in pid's process tree.
func descendants(t *testing.T, root uint32, exe string) []uint32 {
	t.Helper()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(snap)
	parent := map[uint32]uint32{}
	name := map[uint32]string{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		parent[e.ProcessID] = e.ParentProcessID
		name[e.ProcessID] = windows.UTF16ToString(e.ExeFile[:])
	}
	var found []uint32
	for pid := range parent {
		if !strings.EqualFold(name[pid], exe) {
			continue
		}
		for p, hops := parent[pid], 0; hops < 64 && p != 0; p, hops = parent[p], hops+1 {
			if p == root {
				found = append(found, pid)
				break
			}
		}
	}
	return found
}

func waitExit(pid uint32, timeout time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return true // already gone
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, uint32(max(timeout, 0)/time.Millisecond))
	return ev == windows.WAIT_OBJECT_0
}
