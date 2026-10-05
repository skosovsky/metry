package genai

// Contract is an immutable snapshot of effective instrumentation settings. It
// carries declarations and resolved limits, never policy callbacks or payloads.
// Its zero value is invalid; use NewContract or Tracker.Contract.
type Contract struct {
	profile      Profile
	contextBytes int
	eventBytes   int
	raw          bool
}

// NewContract validates the same options as the tracker without creating SDK instruments.
func NewContract(opts ...Option) (Contract, error) {
	cfg := buildRuntimeConfig(opts...)
	if err := validateRuntimeConfig(cfg); err != nil {
		return Contract{}, err
	}
	return contractFromConfig(cfg), nil
}

func contractFromConfig(cfg runtimeConfig) Contract {
	_, raw := cfg.PayloadPolicy().(rawPayloadPolicy)
	return Contract{profile: cloneProfile(cfg.profile), contextBytes: cfg.maxContextLength,
		eventBytes: cfg.maxEventLength, raw: cfg.recordPayloads && raw}
}

// Contract returns the actual settings used by this tracker as a detached snapshot.
func (t *Tracker) Contract() Contract {
	if t == nil {
		return Contract{}
	}
	return contractFromConfig(t.cfg)
}

// Profile returns detached caller declarations.
func (c Contract) Profile() Profile { return cloneProfile(c.profile) }

// RawEnabled reports whether this runtime permits raw payload recording.
func (c Contract) RawEnabled() bool { return c.raw }

// PayloadChannels returns detached channel declarations with resolved budgets.
func (c Contract) PayloadChannels() []PayloadChannel {
	channels := c.profile.PayloadChannels()
	for index := range channels {
		channels[index].OutputByteLimit = c.contextBytes
		if channels[index].BudgetSetting == "max_event_length" {
			channels[index].OutputByteLimit = c.eventBytes
		}
	}
	return channels
}

// Validate rejects an invalid or unsupported effective snapshot.
func (c Contract) Validate() error {
	if err := validateBudgets(c.contextBytes, c.eventBytes); err != nil {
		return err
	}
	if err := c.profile.Validate(); err != nil {
		return err
	}
	if c.raw && (!c.profile.AllowRaw || c.profile.PayloadPolicyID != profileRawPolicy) {
		return invalidObservation("policy", "raw_not_allowed")
	}
	return nil
}

func validateRuntimeConfig(cfg runtimeConfig) error {
	if err := validateBudgets(cfg.maxContextLength, cfg.maxEventLength); err != nil {
		return err
	}
	if err := cfg.profile.Validate(); err != nil {
		return err
	}
	if _, raw := cfg.PayloadPolicy().(rawPayloadPolicy); raw && !cfg.profile.AllowRaw {
		return invalidObservation("policy", "raw_not_allowed")
	}
	return validatePolicyDeclaration(cfg)
}

func validateBudgets(contextBytes, eventBytes int) error {
	if contextBytes < len(truncatedJSONMarker) || contextBytes > maxPayloadInputBytes {
		return invalidObservation("payload_budget", "out_of_bounds")
	}
	if eventBytes < 1 || eventBytes > maxPayloadInputBytes {
		return invalidObservation("event_budget", "out_of_bounds")
	}
	return nil
}

func (c Contract) payloadChannel(name string) (PayloadChannel, bool) {
	for _, channel := range c.PayloadChannels() {
		if channel.Attribute == name {
			return channel, true
		}
	}
	return PayloadChannel{}, false
}
