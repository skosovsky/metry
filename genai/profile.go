package genai

import "slices"

const (
	// InstrumentationProfileID identifies the supported metry observation schema.
	InstrumentationProfileID  = "metry.genai.semconv.e07f4eb.v1"
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
	PayloadPolicyID string
	AllowRaw        bool
	SeriesBudget    int
	Dimensions      []Dimension
}

// DefaultProfile returns detached runtime settings for the fixed schema.
func DefaultProfile() Profile {
	return Profile{
		PayloadPolicyID: profileRedactPolicy,
		AllowRaw:        false,
		SeriesBudget:    defaultSeriesBudget,
		Dimensions: []Dimension{
			{Key: ProviderName, Values: nil},
			{
				Key:    OperationName,
				Values: []string{profileChatOperation, profileToolOperation, profileRetrievalOperation},
			},
			{Key: RequestModel, Values: nil},
			{Key: ResponseModel, Values: nil},
			{Key: OperationStatus, Values: []string{OperationStatusOK, OperationStatusError, OperationStatusUnset}},
			{Key: ServerAddress, Values: nil},
			{Key: ServerPort, Values: nil},
			{Key: ToolName, Values: nil},
			{
				Key:    OperationPurpose,
				Values: []string{PurposeGeneration, PurposeGuardEvaluation, PurposeQualityEvaluation},
			},
			{Key: CostCurrency, Values: []string{profileCurrencyUSD, "EUR"}},
			{
				Key: ErrorType,
				Values: []string{
					string(ErrorClassUnknown),
					OperationStatusError,
					string(ErrorClassCancelled),
					string(ErrorClassDeadlineExceeded),
					string(ErrorClassStreamAborted),
					"timeout", "rate_limit", "quota_exceeded", "validation", "unavailable",
					"permission_denied", "unauthenticated", "not_found", "conflict", "internal", "paused",
				},
			},
		},
	}
}

// Validate rejects unsafe runtime configuration before use.
func (p Profile) Validate() error {
	if p.PayloadPolicyID == "" || len(p.PayloadPolicyID) > maxProfileLabelBytes {
		return invalidObservation("policy", "invalid_identity")
	}
	if p.SeriesBudget < minSeriesBudget || p.SeriesBudget > maxSeriesBudget {
		return invalidObservation("series_budget", "out_of_bounds")
	}
	if len(p.Dimensions) > maxProfileEntries {
		return invalidObservation("profile", "too_large")
	}
	if err := validateDimensions(p.Dimensions); err != nil {
		return err
	}
	if err := p.validateCurrencyBudget(); err != nil {
		return err
	}
	return nil
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

func isMetricDimension(key string) bool {
	return slices.Contains(
		[]string{
			ProviderName,
			OperationName,
			RequestModel,
			ResponseModel,
			ServerAddress,
			ServerPort,
			ToolName,
			OperationStatus,
			OperationPurpose,
			CostCurrency,
			ErrorType,
		},
		key,
	)
}

func cloneProfile(profile Profile) Profile {
	profile.Dimensions = slices.Clone(profile.Dimensions)
	for index := range profile.Dimensions {
		profile.Dimensions[index].Values = slices.Clone(profile.Dimensions[index].Values)
	}
	return profile
}

// WithProfile snapshots caller-owned runtime settings.
func WithProfile(profile Profile) Option {
	snapshot := cloneProfile(profile)
	return func(config *recorderConfig) { config.profile = cloneProfile(snapshot) }
}

// Profile returns a detached copy of this tracker's runtime settings.
func (t *Tracker) Profile() Profile { return cloneProfile(t.cfg.profile) }

// CheckPolicy resolves an externally carried policy identity locally. It cannot
// turn on payloads or install a policy, and never falls back to raw mode.
func (t *Tracker) CheckPolicy(identity string) error {
	if identity == "" || identity != t.cfg.profile.PayloadPolicyID {
		return invalidObservation("policy", "unresolved")
	}
	return nil
}
