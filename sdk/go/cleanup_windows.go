//go:build windows

package smol

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// installSignalCleanup is the Windows counterpart of the unix version. Ctrl+C
// and Ctrl+Break arrive as os.Interrupt; closing the console, logoff and
// shutdown as syscall.SIGTERM (Go maps CTRL_CLOSE/LOGOFF/SHUTDOWN_EVENT to
// it). Owned machines are stopped gracefully, then the process exits: Windows
// has no way to re-raise a console event, and returning from the handler of a
// close event would let the system kill the process anyway.
func installSignalCleanup() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		signal.Stop(ch)
		stopAllLocal()
		os.Exit(1)
	}()
}

// A process killed with TerminateProcess (Task Manager, a supervisor's kill)
// runs no handler at all, and the engine starts each machine's boot helper as
// a detached process. To keep machines from outliving this process however it
// dies, the process joins a job object with KILL_ON_JOB_CLOSE before the
// engine starts any helper: children inherit the job, and when this process
// exits the kernel closes its handle to the job and kills every process still
// in it. The engine's own parent watchdog in the boot helper shuts the guest
// down cleanly when it can; the job is the backstop that needs no cooperation
// from the helper.
//
// The job covers every child this process starts, not only machines, and a
// machine killed this way gets no guest flush. Set SMOLMACHINES_NO_JOB=1 to
// opt out; that is also what lets a Detach machine outlive the process.
var (
	jobOnce  sync.Once
	killJob  windows.Handle // kept for the life of the process: closing it kills the machines
	jobArmed bool
)

func armProcessReaper() {
	jobOnce.Do(func() {
		if envFlag("SMOLMACHINES_NO_JOB") {
			return
		}
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(job)
			return
		}
		if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
			windows.CloseHandle(job)
			return
		}
		killJob, jobArmed = job, true
	})
}

// detachSupported reports whether a Detach machine can outlive this process.
func detachSupported() bool {
	armProcessReaper()
	return !jobArmed
}
