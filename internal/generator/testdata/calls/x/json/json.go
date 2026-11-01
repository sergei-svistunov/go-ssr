// Package json is not encoding/json: a route that imports it as json still
// gets encoding/json for a call type written json.RawMessage.
package json

type RawMessage string
