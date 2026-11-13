//go:build windows

package smol

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

// nativeLoadFlags confine where the loader looks for smol_go.dll's own
// dependencies: the directory smol_go.dll is in (the runtime bundle, which also
// holds krun.dll and libkrunfw.dll) and System32. Neither the current
// directory nor PATH is searched, so a DLL planted in either cannot be picked
// up in place of a bundle or system library.
const nativeLoadFlags = windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR | windows.LOAD_LIBRARY_SEARCH_SYSTEM32

var hardenOnce sync.Once

// hardenDllSearch drops the current directory and PATH from the default DLL
// search path of the whole process (SetDefaultDllDirectories): later
// LoadLibrary calls, including ones the engine makes, search only the
// application directory, System32 and directories added with AddDllDirectory.
// The engine starts every machine in a separate boot helper process that sets
// the same policy for itself. Set SMOLMACHINES_KEEP_DLL_SEARCH=1 to leave the
// process's search path alone (a host program that relies on loading DLLs
// from PATH).
func hardenDllSearch() {
	hardenOnce.Do(func() {
		if envFlag("SMOLMACHINES_KEEP_DLL_SEARCH") {
			return
		}
		_ = windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)
	})
}

// bindNative loads smol_go.dll and binds its four symbols. purego's call path
// works on Windows without cgo, so CGO_ENABLED=0 builds keep working.
func bindNative(path string) error {
	hardenDllSearch()
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	h, err := windows.LoadLibraryEx(abs, 0, nativeLoadFlags)
	if err != nil {
		return fmt.Errorf("load %s: %w", abs, err)
	}
	lib := uintptr(h)
	purego.RegisterLibFunc(&smolABIVersion, lib, "smol_abi_version")
	purego.RegisterLibFunc(&smolCall, lib, "smol_call")
	purego.RegisterLibFunc(&smolFreeString, lib, "smol_free_string")
	purego.RegisterLibFunc(&smolFreeBytes, lib, "smol_free_bytes")
	return nil
}
