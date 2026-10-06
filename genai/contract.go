package genai

// Contract is an immutable snapshot of effective instrumentation settings. It
// carries declarations and resolved limits, never policy callbacks or payloads.
// Its zero value is invalid; use NewContract or Tracker.Contract.
type Contract struct {
	profile      Profile
	contextBytes int
	eventBytes   int
	raw          bool
	referenceRaw bool
}

// NewContract validates the same options as the tracker without creating SDK instruments.
func NewContract(opts ...Option) (Contract, error) {
	cfg := buildRecorderConfig(opts...)
	if err := validateRecorderConfig(cfg); err != nil {
		return Contract{}, err
	}
	return contractFromConfig(cfg), nil
}

func contractFromConfig(cfg recorderConfig) Contract {
	_, raw := cfg.PayloadPolicy().(rawPayloadPolicy)
	return Contract{profile: cloneProfile(cfg.profile), contextBytes: cfg.payloadByteLimit,
		eventBytes: cfg.eventTextByteLimit, raw: cfg.recordPayloads && raw, referenceRaw: raw}
}

// Contract returns the actual settings used by this tracker as a detached snapshot.
func (t *Tracker) Contract() Contract {
	if t == nil {
		return Contract{}
	}
	return contractFromConfig(t.cfg)
}

// Schema returns the immutable fixed descriptor used by this contract.
func (c Contract) Schema() SchemaDescriptor { return Schema() }

// PayloadByteLimit returns the resolved payload output budget in bytes.
func (c Contract) PayloadByteLimit() int { return c.contextBytes }

// EventTextByteLimit returns the resolved event text output budget in bytes.
func (c Contract) EventTextByteLimit() int { return c.eventBytes }

// Profile returns detached effective runtime settings.
func (c Contract) Profile() Profile { return cloneProfile(c.profile) }

// RawEnabled reports whether this recorder permits raw payload recording.
func (c Contract) RawEnabled() bool { return c.raw }

// ReferenceRawEnabled reports raw reference permission independently of content capture.
func (c Contract) ReferenceRawEnabled() bool { return c.referenceRaw }

// PayloadChannels returns detached channel declarations with resolved budgets.
func (c Contract) PayloadChannels() []PayloadChannel {
	channels := c.Schema().PayloadChannels()
	for index := range channels {
		channels[index].OutputByteLimit = c.contextBytes
		if channels[index].BudgetSetting == "event_text_byte_limit" {
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
	if (c.raw || c.referenceRaw) && (!c.profile.AllowRaw || c.profile.PayloadPolicyID != profileRawPolicy) {
		return invalidObservation("policy", "raw_not_allowed")
	}
	return nil
}

func validateRecorderConfig(cfg recorderConfig) error {
	if err := validateBudgets(cfg.payloadByteLimit, cfg.eventTextByteLimit); err != nil {
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
