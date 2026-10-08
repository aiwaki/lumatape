package diagnostics

import "regexp"

// Error strings can contain paths as well as dedicated path fields. Remove the
// complete absolute path, including paths copied from another operating system.
// Spaces inside paths are valid; an error's colon remains useful. Quoted paths
// are handled first. This deliberately prefers removing extra text to leaking a
// user's directory name in an ambiguous unquoted error message.
var quotedPath = regexp.MustCompile(`"(?:[A-Za-z]:[\\/]|\\\\|/)[^"\r\n]*"|'(?:[A-Za-z]:[\\/]|\\\\|/)[^'\r\n]*'`)
var windowsPath = regexp.MustCompile(`(?i)(?:[a-z]:[\\/]|\\\\)[^\r\n"<>|:*?]*`)
var unixPath = regexp.MustCompile(`(?:^|[\s(=:])/(?:[^\r\n"'<>:]+)`)
var replacedHome = regexp.MustCompile(`\[home\](?:[\\/][^\r\n"'<>:]+)?`)

func redactUserPaths(s string) string {
	s = quotedPath.ReplaceAllString(s, "[path]")
	s = windowsPath.ReplaceAllString(s, "[path]")
	s = unixPath.ReplaceAllStringFunc(s, func(path string) string {
		if path[0] == '/' {
			return "[path]"
		}
		return path[:1] + "[path]"
	})
	return replacedHome.ReplaceAllString(s, "[path]")
}
