package mux

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type guardOK struct{}

func (*guardOK) Guard(context.Context, *Request) error { return nil }

type guardOKValue struct{}

func (guardOKValue) Guard(context.Context, *Request) error { return nil }

type guardNone struct{}

type guardExtraArg struct{}

func (*guardExtraArg) Guard(context.Context, *Request, ResponseWriter) error { return nil }

type guardNoCtx struct{}

func (*guardNoCtx) Guard(*Request) error { return nil }

type guardBool struct{}

func (guardBool) Guard(context.Context, *Request) bool { return true }

func TestCheckGuard(t *testing.T) {
	for _, dp := range []any{&guardOK{}, guardOKValue{}, &guardOKValue{}, &guardNone{}, guardNone{}} {
		t.Run(fmt.Sprintf("%T passes", dp), func(t *testing.T) {
			CheckGuard(dp, "/notes")
		})
	}

	for _, dp := range []any{&guardExtraArg{}, &guardNoCtx{}, guardBool{}, &guardBool{}, guardOK{}} {
		t.Run(fmt.Sprintf("%T panics", dp), func(t *testing.T) {
			defer func() {
				msg := fmt.Sprint(recover())
				if !strings.Contains(msg, "E21") || !strings.Contains(msg, "/notes/_id_") || !strings.Contains(msg, fmt.Sprintf("%T", dp)) {
					t.Errorf("panic = %q, want E21 naming %T and /notes/_id_", msg, dp)
				}
			}()
			CheckGuard(dp, "/notes/_id_")
		})
	}
}
