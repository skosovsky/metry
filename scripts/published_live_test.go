//go:build live

package scripts_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLivePublishedBaseline(t *testing.T) {
	// Arrange: an explicit historical published set; it does not certify current source.
	version := os.Getenv("METRY_PUBLISHED_VERSION")
	if version == "" {
		version = "v0.10.0"
	}
	env := []string{
		"GOENV=off", "GOFLAGS=-modcacherw", "GOWORK=off", "GOPRIVATE=", "GONOPROXY=none",
		"GOPATH=" + filepath.Join(t.TempDir(), "gopath"),
		"GOPROXY=" + cachedProxy(t) + ",https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GONOSUMDB=",
		"GOMODCACHE=" + filepath.Join(t.TempDir(), "modcache"),
	}
	// v0.10.0 predates publication of the OTLP fixture module.
	paths := []string{libraryPath, libraryPath + "/middleware/grpc"}
	// Act / Assert: public resolution, checksums and semantic consumer fixtures.
	consumeMapping(t, repoRoot(t), env, version, paths)
}
