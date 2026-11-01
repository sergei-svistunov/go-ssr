package node

// Esc names the escaper that generated code writes a {{ }} value with: one per HTML context
// a value can land in.
type Esc uint8

const (
	// EscText is element text, including the text of <title> and <textarea>.
	EscText Esc = iota
	// EscAttr is a quoted attribute value that is not a URL.
	EscAttr
	// EscURL is a whole URL attribute value.
	EscURL
	// EscURLPart is a value in a URL after a fixed scheme or path, before any ? or #.
	EscURLPart
	// EscURLPathStart is a value right after the lone leading "/" of a URL.
	EscURLPathStart
	// EscURLQuery is a value in a URL after ? or #.
	EscURLQuery
)

// Func returns the pkg/render function of e, as generated code calls it.
func (e Esc) Func() string {
	switch e {
	case EscAttr:
		return "render.Attr"
	case EscURL:
		return "render.URL"
	case EscURLPart:
		return "render.URLPart"
	case EscURLPathStart:
		return "render.URLPathStart"
	case EscURLQuery:
		return "render.URLQuery"
	}
	return "render.Text"
}

// String returns the render function of e, as Func does.
func (e Esc) String() string { return e.Func() }
