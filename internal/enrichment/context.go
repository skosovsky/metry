// Package enrichment stores provenance independently from transport baggage.
package enrichment

import (
	"context"
	"math"
	"strconv"
)

const TypeKey = "metry.attr.type"

type Value struct {
	Key, Text, Type string
	Local           bool
}
type contextKey struct{}

// Values returns a detached snapshot of trusted context values.
func Values(ctx context.Context) []Value {
	values, _ := ctx.Value(contextKey{}).([]Value)
	return append([]Value(nil), values...)
}

// Put returns a context copy. Remote values never replace local values.
func Put(ctx context.Context, incoming ...Value) context.Context {
	values := Values(ctx)
	for _, v := range incoming {
		found := false
		for i, old := range values {
			if old.Key != v.Key {
				continue
			}
			found = true
			if v.Local || !old.Local {
				values[i] = v
			}
			break
		}
		if !found {
			values = append(values, v)
		}
	}
	return context.WithValue(ctx, contextKey{}, values)
}

func String(ctx context.Context, key string) string {
	for _, v := range Values(ctx) {
		if v.Key == key && v.Type == "string" {
			return v.Text
		}
	}
	return ""
}

// ValidScalar rejects invalid typed markers rather than degrading to strings.
func ValidScalar(v Value) bool {
	switch v.Type {
	case "string":
		return true
	case "int":
		_, err := strconv.ParseInt(v.Text, 10, 64)
		return err == nil
	case "bool":
		_, err := strconv.ParseBool(v.Text)
		return err == nil
	case "float":
		n, err := strconv.ParseFloat(v.Text, 64)
		return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	default:
		return false
	}
}
