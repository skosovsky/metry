package propagation

// stringMapCarrier is an internal W3C TextMapCarrier backed by string keys.
type stringMapCarrier map[string]string

func (c stringMapCarrier) Get(key string) string {
	if c == nil {
		return ""
	}
	return c[key]
}

func (c stringMapCarrier) Set(key, value string) {
	if c == nil {
		return
	}
	c[key] = value
}

func (c stringMapCarrier) Keys() []string {
	if len(c) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
