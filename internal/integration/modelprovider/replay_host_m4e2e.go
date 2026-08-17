//go:build m4e2e

package modelprovider

import "strings"

// This exception only exists in the M4 E2E binary. Production builds cannot
// enable it through configuration.
func isE2EReplayHost(host string) bool {
	return host == "argus-replay-model" || strings.HasPrefix(host, "argus-replay-model.")
}
