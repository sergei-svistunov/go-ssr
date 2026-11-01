package render

import (
	"errors"
	"strings"
)

// SafeHTML is markup that a template writes as is with {{$ expr }}. {{ expr }} escapes it
// like any other value.
type SafeHTML struct{ s string }

// stringConstant is unexported so that callers cannot name it.
type stringConstant string

// HTMLConst returns markup written in the source code. Call it directly with a string
// constant: a string variable passed to it does not compile.
func HTMLConst(markup stringConstant) SafeHTML { return SafeHTML{string(markup)} }

// EscapeHTML returns markup that shows exactly text.
func EscapeHTML(text string) SafeHTML {
	var b strings.Builder
	_, _ = escaper.WriteString(&b, text)
	return SafeHTML{b.String()}
}

// JoinHTML concatenates markup.
func JoinHTML(parts ...SafeHTML) SafeHTML {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.s)
	}
	return SafeHTML{b.String()}
}

// String returns the markup.
func (h SafeHTML) String() string { return h.s }

var errDecode = errors.New("render: markup cannot be decoded from input")

// UnmarshalJSON always fails: no JSON a client sends becomes markup.
func (*SafeHTML) UnmarshalJSON([]byte) error { return errDecode }

// UnmarshalText always fails: no text a client sends becomes markup.
func (*SafeHTML) UnmarshalText([]byte) error { return errDecode }
