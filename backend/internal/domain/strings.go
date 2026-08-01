package domain

import "unicode/utf8"

// TruncateStr caps s at n bytes, backing up to a UTF-8 rune boundary so a
// multibyte rune is never cut in half (which would produce invalid UTF-8 in
// JSON output).
func TruncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
