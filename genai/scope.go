package genai

// Scope carries declarative GenAI operation metadata through context.
type Scope struct {
	Purpose   string
	Provider  string
	Model     string
	Operation string
}
