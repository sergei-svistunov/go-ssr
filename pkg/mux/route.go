package mux

import (
	"context"
	"fmt"
	"io"
	"reflect"
)

// Route is one folder of the site. The mux asks it for a fresh RouteState
// on every request whose path runs through it.
//
// A route that has routes below it is a layout or a gate. A layout renders
// the page below it in place of its content; opened itself, it redirects to
// its DefaultRoute. A gate renders only its own page; for the pages below it
// only its Guard runs.
type Route interface {
	NewState() RouteState
	// Layout reports whether the route renders the route below it.
	Layout() bool
}

// RouteState is one route's part of one request. The mux runs each step,
// root to leaf, before the next step: Guard on every route on the path; on a
// POST, the form token check; then, on the routes that render the page (every
// layout on the path and the last route), InitForms, on a POST SubmitForm
// until one route owns the form, Data, and finally Write on the first of
// them, which writes each child through its Frame. A hook's error stops the
// request and goes to Options.ErrorHandler.
//
// Implementations embed Frame.
type RouteState interface {
	// Guard decides whether the request may reach this route at all. It runs
	// before any other hook of any route on the path.
	Guard(ctx context.Context, r *Request) error
	// InitForms fills this route's forms with their initial values.
	InitForms(ctx context.Context, r *Request, w ResponseWriter) error
	// SubmitForm handles the submitted form id if this route renders it and
	// reports whether it did.
	SubmitForm(ctx context.Context, r *Request, w ResponseWriter, id string) (bool, error)
	// Data loads what this route's template shows.
	Data(ctx context.Context, r *Request, w ResponseWriter) error
	// DefaultRoute names the child to redirect to when the request ends at
	// this route, a layout with routes below it.
	DefaultRoute(ctx context.Context, r *Request) (string, error)
	// Write renders this route's template.
	Write(w io.Writer) error
	SetChild(child Child)
	frame() *Frame
}

// Child is what a route renders in place of its content: the route below it.
type Child interface{ Write(w io.Writer) error }

// Frame is the part of a RouteState the mux wires up: the route's child and
// the asset tags its page needs.
type Frame struct {
	child  Child
	assets []string
}

func (f *Frame) frame() *Frame { return f }

// SetChild sets what WriteChild writes.
func (f *Frame) SetChild(c Child) { f.child = c }

// SetAssets sets this route's asset tags.
func (f *Frame) SetAssets(tags []string) { f.assets = tags }

// WriteChild writes the route below this one, or nothing at the leaf.
func (f *Frame) WriteChild(w io.Writer) error {
	if f.child == nil {
		return nil
	}
	return f.child.Write(w)
}

// WriteAssets writes the asset tags of this route and every route below it,
// each tag once.
func (f *Frame) WriteAssets(w io.Writer) error {
	written := map[string]struct{}{}
	for fr := f; fr != nil; fr = fr.next() {
		for _, a := range fr.assets {
			if _, ok := written[a]; ok {
				continue
			}
			written[a] = struct{}{}
			if _, err := io.WriteString(w, a); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *Frame) next() *Frame {
	if c, ok := f.child.(interface{ frame() *Frame }); ok {
		return c.frame()
	}
	return nil
}

// Guarded is the optional Guard of a route's data provider.
type Guarded interface {
	Guard(ctx context.Context, r *Request) error
}

// CheckGuard panics when dp has a method named Guard that does not satisfy
// Guarded, so a misspelt guard never leaves the route at path open. A Guard
// declared on *T while dp is a T counts as misspelt: it would never run.
func CheckGuard(dp any, path string) {
	if _, ok := dp.(Guarded); ok {
		return
	}
	t := reflect.TypeOf(dp)
	if t == nil {
		return
	}
	_, onValue := t.MethodByName("Guard")
	_, onPointer := reflect.PointerTo(t).MethodByName("Guard")
	if !onValue && !onPointer {
		return
	}
	detail := ""
	if reflect.PointerTo(t).Implements(reflect.TypeFor[Guarded]()) {
		detail = fmt.Sprintf(" (it is declared on %v; pass a pointer)", reflect.PointerTo(t))
	}
	panic(fmt.Sprintf("E21: route %s: data provider %T has a Guard method that mux.Guarded does not accept, so the route would be left open%s; give Guard the signature Guard(ctx context.Context, r *mux.Request) error",
		path, dp, detail))
}
