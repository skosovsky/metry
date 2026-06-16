// Package attrconv converts typed observability attributes to OpenTelemetry types for internal packages.
//
//nolint:cyclop // ToOTel mirrors metry.Attribute.toOTel with a single kind switch.
package attrconv

import (
	"go.opentelemetry.io/otel/attribute"
)

// AttributeKind mirrors metry.AttributeKind numeric values without importing metry.
const (
	AttributeKindString = iota
	AttributeKindFloat
	AttributeKindBool
	AttributeKindInt
)

// OTelAttribute is implemented by metry.Attribute for OTel conversion without an import cycle.
type OTelAttribute interface {
	Key() string
	OTelKind() int
	Float64Value() (float64, bool)
	BoolValue() (bool, bool)
	Int64Value() (int64, bool)
	StringValue() (string, bool)
}

// ToOTel converts a typed attribute to an OpenTelemetry key-value pair.
func ToOTel(a OTelAttribute) attribute.KeyValue {
	if a == nil || a.Key() == "" {
		return attribute.KeyValue{}
	}
	switch a.OTelKind() {
	case AttributeKindFloat:
		if v, ok := a.Float64Value(); ok {
			return attribute.Float64(a.Key(), v)
		}
	case AttributeKindBool:
		if v, ok := a.BoolValue(); ok {
			return attribute.Bool(a.Key(), v)
		}
	case AttributeKindInt:
		if v, ok := a.Int64Value(); ok {
			return attribute.Int64(a.Key(), v)
		}
	case AttributeKindString:
		if v, ok := a.StringValue(); ok {
			return attribute.String(a.Key(), v)
		}
	}
	return attribute.KeyValue{}
}
