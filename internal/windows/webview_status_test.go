//go:build windows && (amd64 || arm64)

package windows

import "testing"

// TestNavigationFailureReported checks which COREWEBVIEW2_WEB_ERROR_STATUS
// values navigationCompleted reports: 14 is a navigation another one replaced
// and stays silent; 13 is a host that does not resolve, which is a failure the
// app must hear about.
func TestNavigationFailureReported(t *testing.T) {
	for status, want := range map[int32]bool{
		0:  true,  // unknown
		6:  true,  // server unreachable
		12: true,  // cannot connect
		13: true,  // host name not resolved
		14: false, // operation cancelled
		15: true,  // redirect failed
	} {
		if got := navigationFailureReported(status); got != want {
			t.Errorf("navigationFailureReported(%d) = %v, want %v", status, got, want)
		}
	}
}
