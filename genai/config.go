package genai

import "github.com/skosovsky/metry/internal/genaiconfig"

const defaultMaxContextLength = genaiconfig.DefaultMaxContextLength

func currentConfig() *genaiconfig.RuntimeConfig {
	return genaiconfig.Load()
}

func setRuntimeConfigForTest(maxContextLength int, recordPayloads bool) func() {
	prev := genaiconfig.Load()
	genaiconfig.Store(genaiconfig.New(maxContextLength, recordPayloads))
	return func() {
		genaiconfig.Store(prev)
	}
}

func resetRuntimeConfigForTest() func() {
	return setRuntimeConfigForTest(defaultMaxContextLength, false)
}
