package mux

import (
	"bytes"
	"testing"
)

func TestWriteHtmlEscaped(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"plain text", "hello", "hello"},
		{"ampersand", "a&b", "a&amp;b"},
		{"less than", "a<b", "a&lt;b"},
		{"greater than", "a>b", "a&gt;b"},
		{"double quote", `a"b`, "a&#34;b"},
		{"single quote", "a'b", "a&#39;b"},
		{"carriage return", "a\rb", "a&#13;b"},
		{"multiple specials", `<script>alert("xss")</script>`, "&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt;"},
		{"empty string", "", ""},
		{"no specials", "hello world 123", "hello world 123"},
		{"all specials together", "&'<>\"\r", "&amp;&#39;&lt;&gt;&#34;&#13;"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := WriteHtmlEscaped(&buf, tc.input); err != nil {
				t.Fatal(err)
			}
			if buf.String() != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, buf.String())
			}
		})
	}
}

func TestWriteHtmlEscaped_Types(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"int", 42, "42"},
		{"int negative", -7, "-7"},
		{"int8", int8(8), "8"},
		{"int16", int16(16), "16"},
		{"int32", int32(32), "32"},
		{"int64", int64(64), "64"},
		{"uint", uint(10), "10"},
		{"uint8", uint8(8), "8"},
		{"uint16", uint16(16), "16"},
		{"uint32", uint32(32), "32"},
		{"uint64", uint64(64), "64"},
		{"float32", float32(3.14), "3.14"},
		{"float64", float64(2.718), "2.718"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := WriteHtmlEscaped(&buf, tc.input); err != nil {
				t.Fatal(err)
			}
			if buf.String() != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, buf.String())
			}
		})
	}
}

func TestWriteRaw(t *testing.T) {
	var buf bytes.Buffer
	if _, err := WriteRaw(&buf, "<b>bold</b>"); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "<b>bold</b>" {
		t.Fatalf("expected raw HTML, got %q", buf.String())
	}
}

func TestTernaryIf(t *testing.T) {
	if TernaryIf(true, "yes", "no") != "yes" {
		t.Fatal("TernaryIf(true) should return first value")
	}
	if TernaryIf(false, "yes", "no") != "no" {
		t.Fatal("TernaryIf(false) should return second value")
	}
	if TernaryIf(true, 1, 2) != 1 {
		t.Fatal("TernaryIf(true) should return first int")
	}
}
