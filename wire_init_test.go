package metry

import (
	"testing"

	"github.com/skosovsky/metry/internal/metrytestwire"
)

func TestWireInit_MetryTestWireHooksNonNil(t *testing.T) {
	if metrytestwire.NewProviderFromDeps == nil {
		t.Fatal("metrytestwire.NewProviderFromDeps is nil after init")
	}
}
