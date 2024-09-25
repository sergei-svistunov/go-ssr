package gobuf

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"go/format"
	"sort"
	"strings"
)

type GoBuf struct {
	outBuf          *bytes.Buffer
	printStringsBuf *bytes.Buffer
	vars            map[string][]byte
}

var quotesReplacer = strings.NewReplacer(
	`"`, `\"`,
	"\n", `\n`,
	"\r", `\r`,
)

func New() *GoBuf {
	return &GoBuf{&bytes.Buffer{}, &bytes.Buffer{}, map[string][]byte{}}
}

func (b *GoBuf) WriteString(s string) {
	b.flushPrintStrings()

	b.outBuf.WriteString(s)
}

func (b *GoBuf) WriteStringLn(s string) {
	b.flushPrintStrings()

	b.outBuf.WriteString(s)
	b.outBuf.WriteRune('\n')
}

func (b *GoBuf) WriteQuotedString(s string, suffixes ...string) {
	b.flushPrintStrings()

	b.outBuf.WriteRune('"')
	b.outBuf.WriteString(strings.NewReplacer(
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
	).Replace(s))
	b.outBuf.WriteRune('"')
	for _, suffix := range suffixes {
		b.outBuf.WriteString(suffix)
	}
}

func (b *GoBuf) WritePrintString(s string) {
	b.printStringsBuf.WriteString(s)
}

func (b *GoBuf) Formatted() ([]byte, error) {
	b.flushPrintStrings()

	return format.Source(b.outBuf.Bytes())
}

func (b *GoBuf) String() string {
	b.flushPrintStrings()

	return b.outBuf.String()
}

func (b *GoBuf) flushPrintStrings() {
	if b.printStringsBuf.Len() == 0 {
		return
	}
	str := b.printStringsBuf.String()
	b.printStringsBuf.Reset()

	//b.WriteString("if _, err := w.Write([]byte(")
	//if len(str) > 80 {
	//	b.WriteQuotedString(str[:80], "+\n")
	//	str = str[80:]
	//	for len(str) > 100 {
	//		b.WriteQuotedString(str[:100], "+\n")
	//		str = str[100:]
	//	}
	//	b.WriteQuotedString(str, ",\n")
	//} else {
	//	b.WriteQuotedString(str)
	//}
	//b.WriteStringLn(")); err != nil {")

	b.WriteString("if _, err := w.Write(")
	b.WriteString(b.getVar([]byte(str)))
	b.WriteStringLn("); err != nil {")

	b.WriteStringLn("return err")
	b.WriteStringLn("}")
}

func (b *GoBuf) WriteVars() {
	b.WriteStringLn("var (")
	varNames := make([]string, 0, len(b.vars))
	for name := range b.vars {
		varNames = append(varNames, name)
	}
	sort.Strings(varNames)

	for _, vName := range varNames {
		b.WriteString(vName)
		b.WriteStringLn("=[]byte{")

		vData := b.vars[vName]
		for len(vData) > 24 {
			writeHexCharsLn(b, vData[:24])
			vData = vData[24:]
		}

		writeHexCharsLn(b, vData)
		b.WriteStringLn("}")
	}
	b.WriteStringLn(")")
}

var varNameEncoding = base32.NewEncoding("0123456789abcdefghijklmnopqrstuv").WithPadding(base32.NoPadding)

func (b *GoBuf) getVar(data []byte) string {
	hs := sha256.Sum256(data)
	varName := "_" + varNameEncoding.EncodeToString(hs[:])
	b.vars[varName] = data

	return varName
}

func writeHexCharsLn(b *GoBuf, chars []byte) {
	for _, ch := range chars {
		b.WriteString(fmt.Sprintf("0x%02x,", ch))
	}
	b.WriteString("\n")
}
