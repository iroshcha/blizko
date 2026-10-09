//go:build iroh && !windows

package mobile

/*
#cgo android,arm64 LDFLAGS: ${SRCDIR}/../iroh/lib/aarch64-linux-android/libblizko_iroh.a -ldl -lm -llog
#cgo android,amd64 LDFLAGS: ${SRCDIR}/../iroh/lib/x86_64-linux-android/libblizko_iroh.a -ldl -lm -llog
#cgo linux,!android LDFLAGS: ${SRCDIR}/../iroh/lib/host/libblizko_iroh.a -ldl -lm -lpthread
#cgo ios,arm64 LDFLAGS: ${SRCDIR}/../iroh/lib/aarch64-apple-ios/libblizko_iroh.a -framework Security -framework SystemConfiguration -framework Network -lresolv
#include <stdlib.h>
char *blizko_iroh_call(const char *input);
void blizko_iroh_free(char *value);
*/
import "C"
import "unsafe"

func nativeCall(raw string) string {
	input := C.CString(raw)
	defer C.free(unsafe.Pointer(input))
	output := C.blizko_iroh_call(input)
	if output == nil {
		return `{"error":"native_failure"}`
	}
	defer C.blizko_iroh_free(output)
	return C.GoString(output)
}
