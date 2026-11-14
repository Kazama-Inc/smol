//go:build windows

package smol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A new machine's storage and overlay disks are copies of the bundle's
// pre-formatted templates, not blank files the guest has to format on first
// boot. A copy keeps the template's ext4 UUID; a guest-side mkfs makes a new
// one. A bundle ships the templates zstd-compressed and the engine expands each
// once, beside the archive or in its cache directory.
func TestE2EWindowsDisksStartFromTemplates(t *testing.T) {
	requireE2E(t)
	bundle := os.Getenv("SMOLMACHINES_RUNTIME_DIR")
	if bundle == "" {
		t.Skip("set SMOLMACHINES_RUNTIME_DIR to an unpacked bundle")
	}
	disks := map[string]string{"storage.raw": "storage-template.ext4", "overlay.raw": "overlay-template.ext4"}
	for _, template := range disks {
		if !fileExists(filepath.Join(bundle, template)) && !fileExists(filepath.Join(bundle, template+".zst")) {
			t.Skipf("bundle %s has no %s", bundle, template)
		}
	}
	m := newE2EMachine(t, MachineConfig{Resources: ResourceSpec{CPUs: 1, MemoryMB: 512, Network: Bool(false)}})
	dir := machineDataDir(t, m.Name())
	for disk, template := range disks {
		want := ext4UUID(t, expandedTemplate(t, bundle, template))
		if got := ext4UUID(t, filepath.Join(dir, disk)); got != want {
			t.Errorf("%s has filesystem %s, %s has %s: the disk was not made from the template", disk, got, template, want)
		}
	}
}

// expandedTemplate is the plain template the engine used: in the bundle, or
// expanded from its .zst into the engine's cache directory.
func expandedTemplate(t *testing.T, bundle, name string) string {
	t.Helper()
	for _, path := range []string{filepath.Join(bundle, name), filepath.Join(engineCacheDir(t), "smolvm", name)} {
		if fileExists(path) {
			return path
		}
	}
	t.Fatalf("%s was not expanded beside the bundle or in the cache", name)
	return ""
}

// machineDataDir is the engine's per-machine directory:
// <cache>\smolvm\vms\<first 8 bytes of sha256(name), hex>.
func machineDataDir(t *testing.T, name string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(name))
	dir := filepath.Join(engineCacheDir(t), "smolvm", "vms", hex.EncodeToString(sum[:8]))
	if b, err := os.ReadFile(filepath.Join(dir, "name")); err != nil || strings.TrimSpace(string(b)) != name {
		t.Fatalf("%s does not belong to %q: %q, %v", dir, name, b, err)
	}
	return dir
}

// engineCacheDir is SMOLVM_DATA_DIR\cache, else %LOCALAPPDATA%.
func engineCacheDir(t *testing.T) string {
	t.Helper()
	if root := os.Getenv("SMOLVM_DATA_DIR"); root != "" {
		return filepath.Join(root, "cache")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// ext4UUID reads the filesystem UUID from an ext4 image's superblock.
func ext4UUID(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sb := make([]byte, 0x78)
	if _, err := f.ReadAt(sb, 1024); err != nil {
		t.Fatalf("read superblock of %s: %v", path, err)
	}
	if !bytes.Equal(sb[0x38:0x3a], []byte{0x53, 0xef}) {
		t.Fatalf("%s is not ext4", path)
	}
	return hex.EncodeToString(sb[0x68:0x78])
}
