// Pure helpers exposed for the submit dispatch in app/update_submit.go.
package input

import "strings"

// IsExitRequest reports whether `raw` is the case-insensitive "exit"
// shortcut, which quits the app instead of sending to the agent.
func IsExitRequest(raw string) bool {
	return strings.EqualFold(raw, "exit")
}
