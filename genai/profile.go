package genai

import "slices"

const (
	// InstrumentationProfileID identifies the supported metry observation schema.
	InstrumentationProfileID  = "metry.genai.presence"
	defaultSeriesBudget       = 256
	maxSeriesBudget           = 4096
	minSeriesBudget           = 16
	maxProfileEntries         = 128
	maxProfileLabelBytes      = 80
	profileRawPolicy          = "raw"
	profileRedactPolicy       = "redact"
	profileHashPolicy         = "hash"
	profileInclusiveMapping   = "inclusive"
	profileChatOperation      = "chat"
	profileRetrievalOperation = "retrieval"
	profileCurrencyUSD        = "USD"
	profileModelBoundary      = "model"
	// EncodingString declares a string field's wire form.
	EncodingString = "string"
	// EncodingInteger declares an integer field's wire form.
	EncodingInteger = "integer"
	// EncodingFloat declares a finite floating point field.
	EncodingFloat = "float"
	// EncodingBoolean declares a boolean field.
	EncodingBoolean = "boolean"
	// EncodingFloatArray declares a bounded array of finite floats.
	EncodingFloatArray = "float_array"
	// EncodingJSON declares structurally bounded JSON.
	EncodingJSON = "bounded_json"
)

// Stability describes a field's guarantee within a profile, not its transport.
type Stability string

const (
	StabilityStable      Stability = "stable"
	StabilityDevelopment Stability = "development"
	StabilityExtension   Stability = "extension"
)

// ProfileField declares an exported field and its interpretation.
type ProfileField struct {
	Name      string
	Unit      Unit
	Stability Stability
	Required  bool
	Encoding  string
}

// Boundary declares exactly one instrumentation owner for an operation boundary.
type Boundary struct {
	Name       string
	Operation  string
	Accounting bool
}

// Dimension declares a finite allowlist; other values become unknown.
type Dimension struct {
	Key    string
	Values []string
}

// Profile is caller-owned configuration. Constructors take a deep copy.
// SeriesBudget includes semantic-preserving overflow series per instrument.
type Profile struct {
	ID              string
	Mapping         string
	PayloadPolicyID string
	AllowRaw        bool
	SeriesBudget    int
	Fields          []ProfileField
	Boundaries      []Boundary
	Dimensions      []Dimension
}

// DefaultProfile returns a fresh independent declaration for metry's schema.
//
//nolint:exhaustruct_v5 // Optional unit and required fields intentionally use their zero values in the field catalogue.
func DefaultProfile() Profile {
	return Profile{
		ID:              InstrumentationProfileID,
		Mapping:         profileInclusiveMapping,
		PayloadPolicyID: profileRedactPolicy,
		AllowRaw:        false,
		SeriesBudget:    defaultSeriesBudget,
		Fields: completeProfileFields([]ProfileField{
			{Name: ProviderName, Stability: StabilityDevelopment, Encoding: EncodingString},
			{Name: OperationName, Stability: StabilityDevelopment, Encoding: EncodingString, Required: true},
			{Name: RequestModel, Stability: StabilityDevelopment, Encoding: EncodingString},
			{Name: ResponseModel, Stability: StabilityDevelopment, Encoding: EncodingString},
			{Name: ServerAddress, Stability: StabilityStable, Encoding: EncodingString},
			{Name: ServerPort, Stability: StabilityStable, Encoding: EncodingInteger},
			{Name: ErrorType, Stability: StabilityStable, Encoding: EncodingString},
			{Name: InputTokens, Unit: UnitTokens, Stability: StabilityDevelopment, Encoding: EncodingInteger},
			{Name: OutputTokens, Unit: UnitTokens, Stability: StabilityDevelopment, Encoding: EncodingInteger},
			{Name: CacheReadInputTokens, Unit: UnitTokens, Stability: StabilityDevelopment, Encoding: EncodingInteger},
			{
				Name:      CacheCreationInputTokens,
				Unit:      UnitTokens,
				Stability: StabilityDevelopment,
				Encoding:  EncodingInteger,
			},
			{
				Name:      UsageReasoningOutputTokens,
				Unit:      UnitTokens,
				Stability: StabilityExtension,
				Encoding:  EncodingInteger,
			},
			{Name: UsageCost, Unit: UnitCurrency, Stability: StabilityExtension, Encoding: EncodingFloat},
			{Name: InputMessages, Stability: StabilityDevelopment, Encoding: EncodingJSON},
			{Name: OutputMessages, Stability: StabilityDevelopment, Encoding: EncodingJSON},
			{Name: SystemInstructions, Stability: StabilityDevelopment, Encoding: EncodingJSON},
		}),
		Boundaries: []Boundary{
			{Name: profileModelBoundary, Operation: profileChatOperation, Accounting: true},
			{Name: ToolMetricLabelTool, Operation: profileToolOperation, Accounting: true},
			{Name: profileRetrievalOperation, Operation: profileRetrievalOperation, Accounting: false},
			{Name: string(IdentityLogical), Operation: string(IdentityLogical), Accounting: false},
			{Name: profileEvaluationOperation, Operation: profileEvaluationOperation, Accounting: false},
		},
		Dimensions: []Dimension{
			{Key: ProviderName, Values: nil},
			{
				Key:    OperationName,
				Values: []string{profileChatOperation, profileToolOperation, profileRetrievalOperation},
			},
			{Key: RequestModel, Values: nil},
			{Key: ResponseModel, Values: nil},
			{Key: ServerAddress, Values: nil},
			{Key: ServerPort, Values: nil},
			{Key: ToolMetricLabelTool, Values: nil},
			{
				Key:    OperationPurpose,
				Values: []string{PurposeGeneration, PurposeGuardEvaluation, PurposeQualityEvaluation},
			},
			{Key: CostCurrency, Values: []string{profileCurrencyUSD, "EUR"}},
			{
				Key: ErrorType,
				Values: []string{
					"unknown",
					OperationStatusError,
					string(ErrorClassCancelled),
					string(ErrorClassDeadlineExceeded),
				},
			},
			{
				Key: ToolMetricLabelErrorType,
				Values: []string{
					"unknown",
					"cancelled",
					string(ErrorClassDeadlineExceeded),
					"paused",
					"stream_aborted",
				},
			},
		},
	}
}

// Validate rejects unsupported schema and unsafe configuration before use.
func (p Profile) Validate() error {
	if p.ID != InstrumentationProfileID {
		return invalidObservation("profile", "unsupported")
	}
	if p.Mapping != profileInclusiveMapping {
		return invalidObservation("mapping", "unsupported")
	}
	if p.PayloadPolicyID == "" || len(p.PayloadPolicyID) > maxProfileLabelBytes {
		return invalidObservation("policy", "invalid_identity")
	}
	if p.SeriesBudget < minSeriesBudget || p.SeriesBudget > maxSeriesBudget {
		return invalidObservation("series_budget", "out_of_bounds")
	}
	if len(p.Fields) > maxProfileEntries || len(p.Dimensions) > maxProfileEntries ||
		len(p.Boundaries) > maxProfileEntries {
		return invalidObservation("profile", "too_large")
	}
	if err := validateProfileFields(p.Fields); err != nil {
		return err
	}
	if err := validateDimensions(p.Dimensions); err != nil {
		return err
	}
	if err := p.validateCurrencyBudget(); err != nil {
		return err
	}
	return validateBoundaries(p.Boundaries)
}

func validateDimensions(dimensions []Dimension) error {
	seen := make(map[string]struct{}, len(dimensions))
	for _, dimension := range dimensions {
		if !isMetricDimension(dimension.Key) || len(dimension.Values) > maxProfileEntries {
			return invalidObservation("dimension", "unsupported")
		}
		if _, duplicate := seen[dimension.Key]; duplicate {
			return invalidObservation("dimension", "duplicate")
		}
		seen[dimension.Key] = struct{}{}
		for _, value := range dimension.Values {
			if value == "" || len(value) > maxProfileLabelBytes {
				return invalidObservation("dimension", "invalid_value")
			}
		}
	}
	return nil
}

func (p Profile) validateCurrencyBudget() error {
	for _, dimension := range p.Dimensions {
		if dimension.Key == CostCurrency && len(dimension.Values)*provenanceCount >= p.SeriesBudget {
			return invalidObservation("series_budget", "insufficient_semantic_capacity")
		}
	}
	return nil
}

func validateBoundaries(boundaries []Boundary) error {
	seen := make(map[string]struct{}, len(boundaries))
	canonical := make(map[string]Boundary)
	for _, boundary := range DefaultProfile().Boundaries {
		canonical[boundary.Name] = boundary
	}
	for _, boundary := range boundaries {
		if boundary.Name == "" || len(boundary.Name) > maxProfileLabelBytes || boundary.Operation == "" ||
			len(boundary.Operation) > maxProfileLabelBytes {
			return invalidObservation("boundary", "invalid_declaration")
		}
		if _, duplicate := seen[boundary.Name]; duplicate {
			return invalidObservation("boundary", "duplicate")
		}
		if declared, known := canonical[boundary.Name]; !known || declared != boundary {
			return invalidObservation("boundary", "incompatible")
		}
		seen[boundary.Name] = struct{}{}
	}
	if len(seen) != len(canonical) {
		return invalidObservation("boundary", "missing")
	}
	return nil
}

func validateProfileFields(fields []ProfileField) error {
	if len(fields) == 0 {
		return invalidObservation("profile_field", "missing")
	}
	seen := make(map[string]struct{}, len(fields))
	canonical := make(map[string]ProfileField)
	for _, field := range DefaultProfile().Fields {
		canonical[field.Name] = field
	}
	for _, field := range fields {
		if field.Name == "" || len(field.Name) > maxProfileEntries || field.Encoding == "" {
			return invalidObservation("profile_field", "invalid_declaration")
		}
		if field.Stability != StabilityStable && field.Stability != StabilityDevelopment &&
			field.Stability != StabilityExtension {
			return invalidObservation("profile_field", "invalid_stability")
		}
		if _, duplicate := seen[field.Name]; duplicate {
			return invalidObservation("profile_field", "duplicate")
		}
		seen[field.Name] = struct{}{}
		declared, known := canonical[field.Name]
		if !known {
			return invalidObservation("profile_field", "unsupported")
		}
		if field != declared {
			return invalidObservation("profile_field", "incompatible")
		}
	}
	if len(seen) != len(canonical) {
		return invalidObservation("profile_field", "missing")
	}
	return nil
}

func isMetricDimension(key string) bool {
	return slices.Contains(
		[]string{
			ProviderName,
			OperationName,
			RequestModel,
			ResponseModel,
			ServerAddress,
			ServerPort,
			ToolMetricLabelTool,
			OperationPurpose,
			CostCurrency,
			ErrorType,
			ToolMetricLabelErrorType,
		},
		key,
	)
}

func cloneProfile(profile Profile) Profile {
	profile.Fields = slices.Clone(profile.Fields)
	profile.Boundaries = slices.Clone(profile.Boundaries)
	profile.Dimensions = slices.Clone(profile.Dimensions)
	for index := range profile.Dimensions {
		profile.Dimensions[index].Values = slices.Clone(profile.Dimensions[index].Values)
	}
	return profile
}

// WithProfile configures the immutable instrumentation declaration.
func WithProfile(profile Profile) Option {
	snapshot := cloneProfile(profile)
	return func(config *runtimeConfig) { config.profile = cloneProfile(snapshot) }
}

// Profile returns a detached copy of this tracker's instrumentation declaration.
func (t *Tracker) Profile() Profile { return cloneProfile(t.cfg.profile) }

// CheckPolicy resolves an externally carried policy identity locally. It cannot
// turn on payloads or install a policy, and never falls back to raw mode.
func (t *Tracker) CheckPolicy(identity string) error {
	if identity == "" || identity != t.cfg.profile.PayloadPolicyID {
		return invalidObservation("policy", "unresolved")
	}
	return nil
}
