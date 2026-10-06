package genai

const defaultPayloadByteLimit = 65536

// defaultEventTextByteLimit is the default cap for text stored on span events (e.g. evaluation reasoning).
// Many backends enforce stricter quotas on event attribute payloads than on span attributes (~4–8KB).
const defaultEventTextByteLimit = 4096

// Option configures tracker recorder behavior.
type Option func(*recorderConfig)

type recorderConfig struct {
	profile                 Profile
	recordPayloads          bool
	payloadPolicy           PayloadPolicy
	payloadByteLimit        int
	eventTextByteLimit      int
	toolErrorClassifier     ToolErrorClassifier
	allowedToolErrorClasses map[ErrorClass]struct{}
}

func defaultRecorderConfig() recorderConfig {
	return recorderConfig{
		profile:                 DefaultProfile(),
		recordPayloads:          false,
		payloadPolicy:           RedactPayloadPolicy(),
		payloadByteLimit:        defaultPayloadByteLimit,
		eventTextByteLimit:      defaultEventTextByteLimit,
		toolErrorClassifier:     defaultToolErrorClassifier{},
		allowedToolErrorClasses: defaultAllowedToolErrorClasses(),
	}
}

func buildRecorderConfig(opts ...Option) recorderConfig {
	cfg := defaultRecorderConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.toolErrorClassifier == nil {
		cfg.toolErrorClassifier = defaultToolErrorClassifier{}
	}
	if len(cfg.allowedToolErrorClasses) == 0 {
		cfg.allowedToolErrorClasses = defaultAllowedToolErrorClasses()
	}
	cfg.profile = cloneProfile(cfg.profile)
	return cfg
}

func (c recorderConfig) RecordPayloads() bool {
	return c.recordPayloads
}

func (c recorderConfig) PayloadPolicy() PayloadPolicy {
	if c.payloadPolicy == nil {
		return RedactPayloadPolicy()
	}
	return c.payloadPolicy
}

func (c recorderConfig) PayloadByteLimit() int {
	return c.payloadByteLimit
}

// EventTextByteLimit returns the max byte length for text attached to span events (e.g. evaluation reasoning).
func (c recorderConfig) EventTextByteLimit() int {
	return c.eventTextByteLimit
}

func (c recorderConfig) ToolErrorClassifier() ToolErrorClassifier {
	if c.toolErrorClassifier == nil {
		return defaultToolErrorClassifier{}
	}
	return c.toolErrorClassifier
}

func (c recorderConfig) NormalizeToolErrorClass(value ErrorClass) ErrorClass {
	allowed := c.allowedToolErrorClasses
	if len(allowed) == 0 {
		allowed = defaultAllowedToolErrorClasses()
	}
	return normalizeErrorClass(value, allowed)
}

// WithPayloadPolicy enables payload recording with an explicit sanitizer policy.
func WithPayloadPolicy(policy PayloadPolicy) Option {
	return func(c *recorderConfig) {
		if policy == nil {
			return
		}
		c.recordPayloads = true
		c.payloadPolicy = policy
		switch policy.(type) {
		case rawPayloadPolicy:
			c.profile.PayloadPolicyID = profileRawPolicy
		case hashPayloadPolicy:
			c.profile.PayloadPolicyID = profileHashPolicy
		case redactPayloadPolicy:
			c.profile.PayloadPolicyID = profileRedactPolicy
		}
	}
}

// WithRawPayloads enables raw payload export explicitly.
func WithRawPayloads() Option {
	return func(c *recorderConfig) {
		c.recordPayloads = true
		c.payloadPolicy = RawPayloadPolicy()
		c.profile.AllowRaw = true
		c.profile.PayloadPolicyID = profileRawPolicy
	}
}

// WithPayloadByteLimit sets the payload byte budget. Values below the minimum
// JSON envelope size are rejected by the constructor, not silently defaulted.
func WithPayloadByteLimit(bytes int) Option {
	return func(c *recorderConfig) {
		c.payloadByteLimit = bytes
	}
}

// WithEventTextByteLimit sets the max byte length for text stored on span events (e.g. LLM-judge reasoning on evaluation events).
// Backends and collectors often apply tighter limits to event attributes than to span attributes (commonly on the order of 4–8KB);
// the default is 4096 bytes. Explicit values outside [1, maximum input bytes] are rejected by the constructor.
func WithEventTextByteLimit(bytes int) Option {
	return func(c *recorderConfig) {
		c.eventTextByteLimit = bytes
	}
}

// WithToolErrorClassifier sets the policy used to map tool errors onto bounded classes.
func WithToolErrorClassifier(classifier ToolErrorClassifier) Option {
	return func(c *recorderConfig) {
		if classifier != nil {
			c.toolErrorClassifier = classifier
		}
	}
}

// WithAllowedToolErrorClasses extends the bounded set accepted for tool error metric labels.
func WithAllowedToolErrorClasses(classes ...ErrorClass) Option {
	return func(c *recorderConfig) {
		if len(c.allowedToolErrorClasses) == 0 {
			c.allowedToolErrorClasses = defaultAllowedToolErrorClasses()
		}
		for _, class := range classes {
			normalized := sanitizeErrorClassLabel(class)
			if normalized != "" {
				c.allowedToolErrorClasses[normalized] = struct{}{}
			}
		}
	}
}
