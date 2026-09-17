package config

import "strings"

// joinArgv renders an argument list for humans: arguments containing spaces or
// quotes are double-quoted. The result is for display only and is never
// executed.
func joinArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\"'") {
			parts[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
			continue
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}
