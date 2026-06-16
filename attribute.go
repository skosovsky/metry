package metry

import (
	"log/slog"
	"strconv"

	"go.opentelemetry.io/otel/attribute"

	"github.com/skosovsky/metry/internal/attrconv"
)

const baggageAttrTypeKey = "metry.attr.type"

// AttributeKind describes the typed value stored in an Attribute.
type AttributeKind int

const (
	AttributeKindString AttributeKind = iota
	AttributeKindFloat
	AttributeKindBool
	AttributeKindInt
)

// Attribute is a typed observability key-value pair for Enrich and linked outcomes.
type Attribute struct {
	key   string
	value string
	f64   *float64
	b     *bool
	i64   *int64
}

// Key returns the attribute key (for slog and span attribute names).
func (a Attribute) Key() string { return a.key }

// Value returns the string representation of the attribute value.
func (a Attribute) Value() string { return attributeStringValue(a) }

func (a Attribute) attrType() string {
	switch a.Kind() {
	case AttributeKindFloat:
		return "float"
	case AttributeKindBool:
		return "bool"
	case AttributeKindInt:
		return "int"
	default:
		return "string"
	}
}

// Kind returns the typed kind of the attribute value.
func (a Attribute) Kind() AttributeKind {
	switch {
	case a.f64 != nil:
		return AttributeKindFloat
	case a.b != nil:
		return AttributeKindBool
	case a.i64 != nil:
		return AttributeKindInt
	default:
		return AttributeKindString
	}
}

// OTelKind returns Kind as int for internal attrconv.OTelAttribute.
func (a Attribute) OTelKind() int { return int(a.Kind()) }

// StringValue returns the string value when Kind is AttributeKindString.
func (a Attribute) StringValue() (string, bool) {
	if a.Kind() != AttributeKindString || a.value == "" {
		return "", false
	}
	return a.value, true
}

// Float64Value returns the float value when Kind is AttributeKindFloat.
func (a Attribute) Float64Value() (float64, bool) {
	if a.f64 == nil {
		return 0, false
	}
	return *a.f64, true
}

// BoolValue returns the bool value when Kind is AttributeKindBool.
func (a Attribute) BoolValue() (bool, bool) {
	if a.b == nil {
		return false, false
	}
	return *a.b, true
}

// Int64Value returns the int value when Kind is AttributeKindInt.
func (a Attribute) Int64Value() (int64, bool) {
	if a.i64 == nil {
		return 0, false
	}
	return *a.i64, true
}

func attributeStringValue(a Attribute) string {
	if a.f64 != nil {
		return strconv.FormatFloat(*a.f64, 'g', -1, 64)
	}
	if a.b != nil {
		return strconv.FormatBool(*a.b)
	}
	if a.i64 != nil {
		return strconv.FormatInt(*a.i64, 10)
	}
	return a.value
}

func attributeToSlogAttr(key, value, attrType string) slog.Attr {
	switch attrType {
	case "float":
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return slog.Float64(key, f)
		}
	case "bool":
		if b, err := strconv.ParseBool(value); err == nil {
			return slog.Bool(key, b)
		}
	case "int":
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return slog.Int64(key, i)
		}
	}
	return slog.String(key, value)
}

func newAttribute(key, value string) Attribute {
	if key == "" || value == "" {
		return Attribute{}
	}
	return Attribute{key: key, value: value} //nolint:exhaustruct // string-only variant
}

func (a Attribute) toOTel() attribute.KeyValue {
	return attrconv.ToOTel(a)
}

// TenantID records a tenant identifier in unified observability context.
func TenantID(id string) Attribute { return newAttribute("tenant_id", id) }

// SubjectID records a subject identifier in unified observability context.
func SubjectID(id string) Attribute { return newAttribute("subject_id", id) }

// DoctorID records a doctor identifier in unified observability context.
func DoctorID(id string) Attribute { return newAttribute("doctor_id", id) }

// PatientID records a patient identifier in unified observability context.
func PatientID(id string) Attribute { return newAttribute("patient_id", id) }

// StringAttribute records an arbitrary string key-value pair in observability context.
func StringAttribute(key, value string) Attribute { return newAttribute(key, value) }

// FloatAttribute records a float64 attribute.
func FloatAttribute(key string, value float64) Attribute {
	if key == "" {
		return Attribute{}
	}
	v := value
	return Attribute{key: key, f64: &v} //nolint:exhaustruct // float variant
}

// BoolAttribute records a boolean attribute.
func BoolAttribute(key string, value bool) Attribute {
	if key == "" {
		return Attribute{}
	}
	v := value
	return Attribute{key: key, b: &v} //nolint:exhaustruct // bool variant
}

// IntAttribute records an int64 attribute.
func IntAttribute(key string, value int64) Attribute {
	if key == "" {
		return Attribute{}
	}
	v := value
	return Attribute{key: key, i64: &v} //nolint:exhaustruct // int variant
}

// GenAI baggage key constants shared with genai scope propagation.
const (
	GenAIBaggageProviderKey  = "metry.genai.provider"
	GenAIBaggageModelKey     = "metry.genai.model"
	GenAIBaggageOperationKey = "metry.genai.operation"
	GenAIBaggagePurposeKey   = "metry.genai.purpose"
)

// GenAIProvider records a GenAI provider identifier in observability context.
func GenAIProvider(provider string) Attribute { return newAttribute(GenAIBaggageProviderKey, provider) }

// GenAIModel records a GenAI model identifier in observability context.
func GenAIModel(model string) Attribute { return newAttribute(GenAIBaggageModelKey, model) }

// GenAIOperation records a GenAI operation name in observability context.
func GenAIOperation(operation string) Attribute {
	return newAttribute(GenAIBaggageOperationKey, operation)
}

// GenAIPurpose records a GenAI purpose in observability context.
func GenAIPurpose(purpose string) Attribute {
	return newAttribute(GenAIBaggagePurposeKey, purpose)
}
