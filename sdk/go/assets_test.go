package smol

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

type tarEntry struct {
	name, body, link string
}

func makeTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.link != "" {
			h = &tar.Header{Name: e.name, Linkname: e.link, Typeflag: tar.TypeSymlink}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.link == "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// serveBundle publishes archive as this platform's bundle on a fake release
// server, listed in the checksum file with sum, and counts bundle downloads.
func serveBundle(t *testing.T, archive []byte, sum string) *atomic.Int32 {
	t.Helper()
	slug, err := platformSlug()
	if err != nil {
		t.Skip(err)
	}
	name := fmt.Sprintf("smol-go-runtime-%s-%s.tar.gz", Version, slug)
	var downloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v" + Version + "/" + name:
			downloads.Add(1)
			_, _ = w.Write(archive)
		case "/v" + Version + "/" + bundleChecksums:
			fmt.Fprintf(w, "%s  %s\n%s  smol-go-runtime-other.tar.gz\n", sum, name, strings.Repeat("0", 64))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := releases
	releases = srv.URL
	t.Cleanup(func() { releases = old })
	t.Setenv("SMOLMACHINES_CACHE_DIR", t.TempDir())
	t.Setenv("SMOLMACHINES_NO_DOWNLOAD", "")
	return &downloads
}

func TestBundleDownloadIsVerifiedExtractedAndCached(t *testing.T) {
	entries := []tarEntry{
		{name: "./" + libraryName(), body: "lib"},
		{name: "./" + helperName(), body: "helper"},
	}
	// Windows bundles carry no symlinks (creating one needs Developer Mode or
	// elevation), and Windows has no execute bit to check.
	unix := runtime.GOOS != "windows"
	if unix {
		entries = append(entries,
			tarEntry{name: "./libkrunfw.5.dylib", body: "fw"},
			tarEntry{name: "./libkrunfw.dylib", link: "libkrunfw.5.dylib"})
	}
	archive := makeTarGz(t, entries)
	downloads := serveBundle(t, archive, sha(archive))

	dir, err := ensureBundle()
	if err != nil {
		t.Fatalf("ensureBundle: %v", err)
	}
	helper := filepath.Join(dir, helperName())
	if b, err := os.ReadFile(helper); err != nil || string(b) != "helper" {
		t.Fatalf("%s = %q, %v", helperName(), b, err)
	}
	if unix {
		if info, err := os.Stat(helper); err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("smol-vmm is not executable: %v %v", info, err)
		}
		if target, err := os.Readlink(filepath.Join(dir, "libkrunfw.dylib")); err != nil || target != "libkrunfw.5.dylib" {
			t.Fatalf("symlink = %q, %v", target, err)
		}
	}
	again, err := ensureBundle()
	if err != nil || again != dir {
		t.Fatalf("second ensureBundle = %q, %v; want cached %q", again, err, dir)
	}
	if n := downloads.Load(); n != 1 {
		t.Fatalf("downloaded %d times; want once, then the cache", n)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), "*.partial-*"))
	if len(leftovers) != 0 {
		t.Fatalf("partial unpack dirs left behind: %v", leftovers)
	}

	t.Setenv("SMOLMACHINES_RUNTIME_DIR", dir)
	assets, err := resolveRuntimeAssets()
	if err != nil {
		t.Fatalf("resolveRuntimeAssets: %v", err)
	}
	if assets.library != filepath.Join(dir, libraryName()) || assets.bootBinary != helper || assets.libDir != dir ||
		assets.templateDir != dir {
		t.Fatalf("assets = %+v", assets)
	}
}

func TestBundleChecksumMismatchIsRefused(t *testing.T) {
	archive := makeTarGz(t, []tarEntry{{name: libraryName(), body: "lib"}})
	serveBundle(t, archive, strings.Repeat("a", 64))
	if _, err := ensureBundle(); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("ensureBundle = %v; want a checksum mismatch", err)
	}
}

func TestBundleNotListedIsRefused(t *testing.T) {
	err := verify("smol-go-runtime-x.tar.gz", []byte("x"), []byte(sha([]byte("x"))+"  other.tar.gz\n"))
	if err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("verify = %v; want not listed", err)
	}
}

func TestBundleEscapingEntriesAreRefused(t *testing.T) {
	for _, entries := range [][]tarEntry{
		{{name: "../evil", body: "x"}},
		{{name: "ok/../../evil", body: "x"}},
		{{name: "link", link: "/etc/passwd"}},
		{{name: "link", link: "../../etc/passwd"}},
		{{name: "link", link: `\Windows\win.ini`}},
		{{name: "link", link: `C:\Windows\win.ini`}},
		{{name: "link", link: "C:win.ini"}},
		{{name: "link", link: `\\server\share`}},
		{{name: "link", link: `lib\..\..\evil`}},
	} {
		if err := extract(makeTarGz(t, entries), t.TempDir()); err == nil || !strings.Contains(err.Error(), "escapes") {
			t.Errorf("extract(%+v) = %v; want an escape refusal", entries, err)
		}
	}
}

func TestBundleNoDownloadIsRespected(t *testing.T) {
	archive := makeTarGz(t, []tarEntry{{name: libraryName(), body: "lib"}})
	downloads := serveBundle(t, archive, sha(archive))
	t.Setenv("SMOLMACHINES_NO_DOWNLOAD", "1")
	if _, err := ensureBundle(); err == nil || !strings.Contains(err.Error(), "downloads are disabled") {
		t.Fatalf("ensureBundle = %v; want downloads refused", err)
	}
	if downloads.Load() != 0 {
		t.Fatal("downloaded despite SMOLMACHINES_NO_DOWNLOAD")
	}
}

// TestE2EDownloadedBundle boots a machine from a real runtime tarball made by
// scripts/bundle-runtime.sh, served as a release download. Run it alone, in
// its own process: the native core loads once per process.
func TestE2EDownloadedBundle(t *testing.T) {
	path := os.Getenv("SMOL_E2E_BUNDLE")
	if os.Getenv("SMOL_E2E") != "1" || path == "" {
		t.Skip("set SMOL_E2E=1 and SMOL_E2E_BUNDLE to a bundle tarball")
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	serveBundle(t, archive, sha(archive))
	t.Setenv("SMOLMACHINES_RUNTIME_DIR", "")
	m := newE2EMachine(t, MachineConfig{Resources: ResourceSpec{CPUs: 1, MemoryMB: 512}})
	r, err := m.Exec(t.Context(), []string{"uname", "-s"}, nil)
	if err != nil || strings.TrimSpace(r.Stdout) != "Linux" {
		t.Fatalf("uname = %q, %v", r.Stdout, err)
	}
}

func TestLinkEscapes(t *testing.T) {
	for target, want := range map[string]bool{
		"libkrunfw.5.dylib": false,
		"lib/libkrunfw.so":  false,
		"a..b":              false,
		"":                  true,
		"/etc/passwd":       true,
		"../x":              true,
		"a/../../x":         true,
		`a\..\..\x`:         true,
		`\x`:                true,
		`\\host\share`:      true,
		"C:x":               true,
		`C:\x`:              true,
	} {
		if got := linkEscapes(target); got != want {
			t.Errorf("linkEscapes(%q) = %v; want %v", target, got, want)
		}
	}
}
