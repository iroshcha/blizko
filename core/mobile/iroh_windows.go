//go:build windows && iroh

package mobile

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var nativeStringLength = windows.NewLazySystemDLL("kernel32.dll").NewProc("lstrlenA")

var windowsBridge struct {
	once       sync.Once
	dll        *windows.DLL
	call, free *windows.Proc
	err        error
}

func nativeCall(raw string) string {
	windowsBridge.once.Do(func() {
		executable, err := os.Executable()
		if err != nil {
			windowsBridge.err = err
			return
		}
		library := filepath.Join(filepath.Dir(executable), "blizko_iroh.dll")
		// Explicit override allows tests to load the built library from their
		// temporary executable directory. Normal builds always use a full path.
		if override := os.Getenv("BLIZKO_IROH_DLL"); override != "" {
			library, err = filepath.Abs(override)
			if err != nil {
				windowsBridge.err = err
				return
			}
		}
		windowsBridge.dll, windowsBridge.err = windows.LoadDLL(library)
		if windowsBridge.err != nil {
			return
		}
		windowsBridge.call, windowsBridge.err = windowsBridge.dll.FindProc("blizko_iroh_call")
		if windowsBridge.err != nil {
			return
		}
		windowsBridge.free, windowsBridge.err = windowsBridge.dll.FindProc("blizko_iroh_free")
	})
	if windowsBridge.err != nil {
		return `{"error":"iroh_not_linked"}`
	}
	input := append([]byte(raw), 0)
	output, _, _ := windowsBridge.call.Call(uintptr(unsafe.Pointer(&input[0])))
	runtime.KeepAlive(input)
	if output == 0 {
		return `{"error":"native_failure"}`
	}
	defer windowsBridge.free.Call(output)
	length, _, _ := nativeStringLength.Call(output)
	if length == 0 || length > 100000 {
		return `{"error":"invalid_native_response"}`
	}
	buffer := make([]byte, length)
	var read uintptr
	if err := windows.ReadProcessMemory(windows.CurrentProcess(), output, &buffer[0], length, &read); err != nil || read != length {
		return `{"error":"invalid_native_response"}`
	}
	return string(buffer)
}
