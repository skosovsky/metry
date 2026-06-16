package metrytest

import "fmt"

func mustAs[T any](v any, hook string) T {
	if v == nil {
		panic(fmt.Sprintf("metrytest: %s wire hook returned nil (init order?)", hook))
	}
	out, ok := v.(T)
	if !ok {
		panic(fmt.Sprintf("metrytest: %s wire hook returned %T, want %T", hook, v, *new(T)))
	}
	return out
}
