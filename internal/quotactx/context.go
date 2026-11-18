// Package quotactx carries validated product classification into per-attempt
// upstream quota admission without coupling the quota manager to the executor.
package quotactx

import "context"

type key struct{}

func WithProduct(ctx context.Context, product string) context.Context {
	return context.WithValue(ctx, key{}, product)
}
func Product(ctx context.Context) string {
	p, _ := ctx.Value(key{}).(string)
	return p
}
