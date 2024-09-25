package ctxdata

import "context"

type ctxData struct{}

var ctxDataKey ctxData

type Data struct {
	PageTitle string
}

func ToContext(ctx context.Context, data *Data) context.Context {
	return context.WithValue(ctx, ctxDataKey, data)
}

func FromContext(ctx context.Context) *Data {
	v := ctx.Value(ctxDataKey)
	if v == nil {
		return nil
	}
	return v.(*Data)
}
