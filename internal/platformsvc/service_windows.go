//go:build windows

package platformsvc

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	serviceWin32OwnProcess = 0x10
	serviceStopped         = 1
	serviceStartPending    = 2
	serviceStopPending     = 3
	serviceRunning         = 4
	acceptStop             = 1
	acceptShutdown         = 4
	controlStop            = 1
	controlInterrogate     = 4
	controlShutdown        = 5
	serviceSpecificError   = 1066
)

type serviceTableEntry struct {
	name *uint16
	proc uintptr
}
type serviceStatus struct{ serviceType, currentState, controlsAccepted, win32ExitCode, serviceSpecificExitCode, checkPoint, waitHint uint32 }

var (
	advapi          = syscall.NewLazyDLL("advapi32.dll")
	startDispatcher = advapi.NewProc("StartServiceCtrlDispatcherW")
	registerHandler = advapi.NewProc("RegisterServiceCtrlHandlerExW")
	setStatus       = advapi.NewProc("SetServiceStatus")
)

// RunService is explicitly entered through --service by an SCM-registered
// service. It does not silently continue in console mode if dispatcher setup
// fails. The SCM receives real START_PENDING/RUNNING/STOP_PENDING/STOPPED
// transitions and a nonzero failure status for unexpected shutdowns.
func RunService(name string, runner Runner) error {
	if name == "" {
		return errors.New("windows service name required")
	}
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	// Go callbacks are invoked on system threads; callback functions and the
	// runner must remain reachable until StartServiceCtrlDispatcherW returns.
	var runError error
	callback := syscall.NewCallback(func(_ uintptr, _ uintptr) uintptr {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var handle uintptr
		var statusMu sync.Mutex
		status := serviceStatus{serviceType: serviceWin32OwnProcess, currentState: serviceStartPending, checkPoint: 1, waitHint: 30000}
		update := func(state, accepted, win32Err, specific uint32) {
			statusMu.Lock()
			defer statusMu.Unlock()
			status.currentState = state
			status.controlsAccepted = accepted
			status.win32ExitCode = win32Err
			status.serviceSpecificExitCode = specific
			if state == serviceStartPending || state == serviceStopPending {
				status.checkPoint++
				status.waitHint = 30000
			} else {
				status.checkPoint = 0
				status.waitHint = 0
			}
			if handle != 0 {
				_, _, _ = setStatus.Call(handle, uintptr(unsafe.Pointer(&status)))
			}
		}
		stopCallback := syscall.NewCallback(func(code, eventType, eventData, context uintptr) uintptr {
			switch code {
			case controlStop, controlShutdown:
				update(serviceStopPending, 0, 0, 0)
				cancel()
			case controlInterrogate:
			}
			return 0
		})
		handle, _, _ = registerHandler.Call(uintptr(unsafe.Pointer(namePtr)), stopCallback, 0)
		if handle == 0 {
			runError = errors.New("RegisterServiceCtrlHandlerExW failed")
			return 0
		}
		update(serviceStartPending, 0, 0, 0)
		ready := func() {
			if ctx.Err() == nil {
				update(serviceRunning, acceptStop|acceptShutdown, 0, 0)
			}
		}
		runError = runner(ctx, ready)
		update(serviceStopPending, 0, 0, 0)
		if runError != nil {
			update(serviceStopped, 0, serviceSpecificError, 1)
		} else {
			update(serviceStopped, 0, 0, 0)
		}
		runtime.KeepAlive(stopCallback)
		return 0
	})
	entries := [2]serviceTableEntry{{name: namePtr, proc: callback}, {}}
	ok, _, callErr := startDispatcher.Call(uintptr(unsafe.Pointer(&entries[0])))
	runtime.KeepAlive(callback)
	runtime.KeepAlive(entries)
	if ok == 0 {
		return fmt.Errorf("StartServiceCtrlDispatcherW: %w (run without --service for console mode)", callErr)
	}
	return runError
}
func notifyReady(_ context.Context) {}
