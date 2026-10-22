package core

import (
	"regexp"
	"strings"
)

// otpWords say that a text holds a one-time code.
var otpWords = []string{
	"code", "otp", "verification", "verify", "passcode", "password", "pin", "tan",
	"验证码", "校验码", "动态码", "确认码", "认证码", "密码",
	"bestätigungscode", "sicherheitscode", "kennwort",
}

// otpNumber is a code of 4 to 8 digits, maybe in two groups as in
// "123-456" or "123 456", and not part of a longer number.
var otpNumber = regexp.MustCompile(`(?:^|[^\d])(\d{3,4}[- ]\d{3,4}|\d{4,8})(?:[^\d]|$)`)

// FindCode returns the one-time code in a text, such as the 6 digits of a
// verification SMS, or "" when the text names no code. The digits come
// without separators.
func FindCode(text string) string {
	lower := strings.ToLower(text)
	hasWord := false
	for _, w := range otpWords {
		if strings.Contains(lower, w) {
			hasWord = true
			break
		}
	}
	if !hasWord {
		return ""
	}
	for _, m := range otpNumber.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[2], m[3]
		// Part of a phone number or another number in groups.
		if nextTo(text[:start], true) || nextTo(text[end:], false) {
			continue
		}
		code := strings.NewReplacer("-", "", " ", "").Replace(text[start:end])
		// A year in the text is not a code.
		if len(code) == 4 && strings.Contains(text, code+"年") {
			continue
		}
		return code
	}
	return ""
}

// nextTo tells whether a digit or a "+" comes right before (before) or after
// a number, past one space or dash.
func nextTo(s string, before bool) bool {
	if before {
		s = strings.TrimRight(s, " -")
		return s != "" && strings.ContainsAny(s[len(s)-1:], "0123456789+")
	}
	s = strings.TrimLeft(s, " -")
	return s != "" && strings.ContainsAny(s[:1], "0123456789")
}
