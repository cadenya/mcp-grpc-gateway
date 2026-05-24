package forwardmetadata

import (
	"context"

	"google.golang.org/grpc/metadata"
)

type contextKey struct{}

func NewContext(ctx context.Context, md metadata.MD) context.Context {
	return context.WithValue(ctx, contextKey{}, md.Copy())
}

func FromContext(ctx context.Context) metadata.MD {
	md, _ := ctx.Value(contextKey{}).(metadata.MD)
	return md.Copy()
}

func AppendToOutgoingContext(ctx context.Context) context.Context {
	md := FromContext(ctx)
	if len(md) == 0 {
		return ctx
	}
	pairs := make([]string, 0, len(md)*2)
	for key, values := range md {
		for _, value := range values {
			pairs = append(pairs, key, value)
		}
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}
