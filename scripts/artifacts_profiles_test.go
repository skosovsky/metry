//go:build integration || live

package scripts_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

const (
	libraryPath      = "github.com/skosovsky/metry"
	candidateVersion = "v0.0.999"
)

func ownModule(path string) bool {
	return path == libraryPath || strings.HasPrefix(path, libraryPath+"/")
}

func sourceCandidate(t *testing.T, root string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "source")
	// Copy package inputs, including new Go tests, excluding hidden trees and non-package file types.
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "bin" ||
				entry.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".mod", ".sum", ".json", ".yaml", ".txt":
		default:
			if entry.Name() != "LICENSE" {
				return nil
			}
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		write(t, filepath.Join(target, relative), read(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(target, "Makefile"), read(t, filepath.Join(root, "Makefile")))
	for dir := range strings.FieldsSeq(command(t, target, nil, "make", "--no-print-directory", "-s", "modules")) {
		path := filepath.Join(target, dir, "go.mod")
		manifest, parseErr := modfile.Parse(path, read(t, path), nil)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, requirement := range manifest.Require {
			if ownModule(requirement.Mod.Path) {
				if err := manifest.AddRequire(requirement.Mod.Path, candidateVersion); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, replacement := range append([]*modfile.Replace(nil), manifest.Replace...) {
			if ownModule(replacement.Old.Path) {
				if err := manifest.DropReplace(replacement.Old.Path, replacement.Old.Version); err != nil {
					t.Fatal(err)
				}
			}
		}
		data, formatErr := manifest.Format()
		if formatErr != nil {
			t.Fatal(formatErr)
		}
		write(t, path, data)
	}
	return target
}

func artifactProxy(t *testing.T, source, proxy string) []string {
	t.Helper()
	dirs := strings.Fields(command(t, source, nil, "make", "--no-print-directory", "-s", "modules"))
	paths := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		base := filepath.Join(source, dir)
		data := read(t, filepath.Join(base, "go.mod"))
		manifest, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			t.Fatal(err)
		}
		path := manifest.Module.Mod.Path
		paths = append(paths, path)
		for _, replacement := range manifest.Replace {
			if ownModule(replacement.Old.Path) {
				t.Fatalf("development replacement in %s", path)
			}
		}
		for _, requirement := range manifest.Require {
			if ownModule(requirement.Mod.Path) && requirement.Mod.Version != candidateVersion {
				t.Fatalf("wrong candidate dependency %s", requirement.Mod)
			}
		}
		escaped, err := module.EscapePath(path)
		if err != nil {
			t.Fatal(err)
		}
		var archive bytes.Buffer
		if err := modzip.CreateFromDir(
			&archive,
			module.Version{Path: path, Version: candidateVersion},
			base,
		); err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(proxy, escaped, "@v")
		write(t, filepath.Join(destination, candidateVersion+".zip"), archive.Bytes())
		write(t, filepath.Join(destination, candidateVersion+".mod"), data)
		write(t, filepath.Join(destination, candidateVersion+".info"),
			fmt.Appendf(nil, `{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, candidateVersion))
		write(t, filepath.Join(destination, "list"), []byte(candidateVersion+"\n"))
	}
	return paths
}

func verifyGraph(t *testing.T, dir string, env []string, paths []string, version string) {
	t.Helper()
	graph := command(t, dir, env, "go", "list", "-m", "-json", "all")
	decoder := json.NewDecoder(strings.NewReader(graph))
	seen := map[string]bool{}
	for decoder.More() {
		var dependency struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
			Replace any    `json:"Replace"`
		}
		if err := decoder.Decode(&dependency); err != nil {
			t.Fatal(err)
		}
		if dependency.Replace != nil {
			t.Fatalf("replacement in consumer graph: %+v", dependency)
		}
		for _, path := range paths {
			if dependency.Path == path {
				if dependency.Version != version {
					t.Fatalf("wrong resolved dependency: %+v", dependency)
				}
				seen[path] = true
			}
		}
	}
	if len(seen) != len(paths) {
		t.Fatal("missing candidate modules")
	}
}

func consumeMapping(t *testing.T, root string, env []string, version string, paths []string) {
	t.Helper()
	dir := t.TempDir()
	command(t, dir, env, "go", "mod", "init", "consumer.invalid/observation-mapping")
	for _, path := range paths {
		command(t, dir, env, "go", "mod", "edit", "-require="+path+"@"+version)
	}
	entries, err := filepath.Glob(filepath.Join(root, "examples/observation_mapping/*.go"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("mapping fixtures: %v", err)
	}
	for _, path := range entries {
		write(t, filepath.Join(dir, filepath.Base(path)), read(t, path))
	}
	command(t, dir, env, "go", "mod", "tidy")
	// Retain test-only nested modules in the graph after tidy.
	for _, path := range paths {
		command(t, dir, env, "go", "mod", "edit", "-require="+path+"@"+version)
	}
	command(t, dir, env, "go", "mod", "download", "all")
	verifyGraph(t, dir, env, paths, version)
	command(t, dir, env, "go", "test", "-race", "-count=1", "./...")
	command(t, dir, env, "go", "run", ".")
	for _, path := range paths {
		// Compile every public package without recursively executing infrastructure tests.
		command(t, dir, env, "go", "test", "-mod=mod", "-run=^$", path+"/...")
		if path != libraryPath {
			command(
				t,
				dir,
				env,
				"go",
				"test",
				"-mod=mod",
				"-race",
				"-count=1",
				"-tags=integration",
				"-run=^TestIntegration",
				path+"/...",
			)
		}
	}
	verifyGraph(t, dir, env, paths, version)
}

// Reuse downloaded ZIPs as an optional mirror; consumers still use a fresh module
// cache and Go checksum verification. Missing artifacts fall back to the public proxy.
func cachedProxy(t *testing.T) string {
	t.Helper()
	cache := command(t, repoRoot(t), nil, "go", "env", "GOMODCACHE")
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(cache, "cache/download"))}).String()
}

func TestIntegrationCandidateArtifacts(t *testing.T) {
	// Arrange: actual Go module ZIPs and prepared manifests for the current source.
	root := repoRoot(t)
	source := sourceCandidate(t, root)
	proxy := filepath.Join(t.TempDir(), "proxy")
	paths := artifactProxy(t, source, proxy)
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}).String()
	env := []string{
		"GOENV=off", "GOFLAGS=-modcacherw", "GOWORK=off", "GOPRIVATE=", "GONOPROXY=none",
		"GOPATH=" + filepath.Join(t.TempDir(), "gopath"),
		"GOPROXY=" + uri + "," + cachedProxy(t) + ",https://proxy.golang.org", "GOSUMDB=sum.golang.org",
		"GONOSUMDB=" + libraryPath + "," + libraryPath + "/*",
		"GOMODCACHE=" + filepath.Join(t.TempDir(), "modcache"),
	}
	// Act / Assert: resolve all modules and run semantic mapping, gRPC and OTLP fixtures.
	consumeMapping(t, root, env, candidateVersion, paths)
}

func TestIntegrationRunnableExamples(t *testing.T) {
	// Arrange: discover executable examples instead of maintaining a second inventory.
	root := repoRoot(t)
	packages := strings.Fields(
		command(t, root, nil, "go", "list", "-f", `{{if eq .Name "main"}}{{.ImportPath}}{{end}}`, "./examples/..."),
	)
	if len(packages) == 0 {
		t.Fatal("no executable examples discovered")
	}
	// Act / Assert: every example must complete successfully.
	for _, path := range packages {
		t.Run(path, func(t *testing.T) {
			command(t, root, nil, "go", "run", path)
		})
	}
}
