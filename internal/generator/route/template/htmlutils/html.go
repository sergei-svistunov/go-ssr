package htmlutils

var VoidElements = map[string]bool{
	"area":   true,
	"base":   true,
	"br":     true,
	"col":    true,
	"embed":  true,
	"hr":     true,
	"img":    true,
	"input":  true,
	"keygen": true, // "keygen" has been removed from the spec, but are kept here for backwards compatibility.
	"link":   true,
	"meta":   true,
	"param":  true,
	"source": true,
	"track":  true,
	"wbr":    true,
}

var LiteralElements = map[string]bool{
	"iframe":    true,
	"noembed":   true,
	"noframes":  true,
	"noscript":  true,
	"plaintext": true,
	"script":    true,
	"style":     true,
	"xmp":       true,
}
