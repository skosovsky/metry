package metry

// Labels are typed metric label key-value pairs for MetricsRegistry instruments.
type Labels map[string]string

// LabelsOf returns a defensive copy of labels suitable for recording.
func LabelsOf(src map[string]string) Labels {
	if len(src) == 0 {
		return nil
	}
	out := make(Labels, len(src))
	for k, v := range src {
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func copyLabels(labels Labels) Labels {
	if len(labels) == 0 {
		return nil
	}
	out := make(Labels, len(labels))
	for k, v := range labels {
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}
