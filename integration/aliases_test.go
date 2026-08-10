package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableAliases(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"exact alias created", testAliasExact},
		{"wrong payload decides", testAliasWrong},
		{"occupied path decides", testAliasOccupied},
		{"dangling exact stays", testAliasDangling},
		{"file to alias transition", testAliasFileToAlias},
		{"alias to file transition", testAliasAliasToFile},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// writeRoutes writes one routes file declaring the tool alias.
func writeRoutes(t *testing.T, env execEnv, source string) {
	t.Helper()
	writeFile(t, filepath.Join(env.repo, "_routes.toml"), []byte(source))
}

// toolRoutes is the standard fixture route declaration.
const toolRoutes = `version = 1

[symlinks.all]
".config/tool" = ["bin/tool"]
`

// readLink reads one HOME-relative symlink payload.
func readLink(t *testing.T, env execEnv, relative string) string {
	t.Helper()
	content, err := os.Readlink(filepath.Join(env.home, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("readlink %s: %v", relative, err)
	}
	return content
}

func testAliasExact(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeRoutes(t, env, toolRoutes)
	env.source(t, ".config/tool", "content")
	if err := os.MkdirAll(filepath.Join(env.home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if payload := readLink(t, env, "bin/tool"); payload != "../.config/tool" {
		t.Fatalf("payload = %q, want ../.config/tool", payload)
	}
}

func testAliasWrong(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeRoutes(t, env, toolRoutes)
	env.source(t, ".config/tool", "content")
	if err := os.MkdirAll(filepath.Join(env.home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", filepath.Join(env.home, "bin", "tool")); err != nil {
		t.Fatal(err)
	}
	result := env.runPty(t, []string{"overwrite"}, "apply")
	if result.Code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if payload := readLink(t, env, "bin/tool"); payload != "../.config/tool" {
		t.Fatalf("payload = %q, want the corrected link", payload)
	}
}

func testAliasOccupied(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeRoutes(t, env, toolRoutes)
	env.source(t, ".config/tool", "content")
	if err := os.MkdirAll(filepath.Join(env.home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, "bin", "tool"), []byte("intruder"))
	result := env.runPty(t, []string{"overwrite"}, "apply")
	if result.Code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if payload := readLink(t, env, "bin/tool"); payload != "../.config/tool" {
		t.Fatalf("payload = %q, want the replaced link", payload)
	}
}

func testAliasDangling(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeRoutes(t, env, toolRoutes)
	env.source(t, ".config/tool", "content")
	if err := os.MkdirAll(filepath.Join(env.home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.config/tool", filepath.Join(env.home, "bin", "tool")); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if payload := readLink(t, env, "bin/tool"); payload != "../.config/tool" {
		t.Fatalf("payload = %q, want the exact link kept", payload)
	}
}

func testAliasFileToAlias(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/tool", "content")
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("file apply: %+v", result)
	}
	if string(env.target(t, ".config/tool")) != "content" {
		t.Fatal("the file must deploy first")
	}
	writeRoutes(t, env, toolRoutes)
	if err := os.MkdirAll(filepath.Join(env.home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("transition apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if payload := readLink(t, env, "bin/tool"); payload != "../.config/tool" {
		t.Fatalf("payload = %q, want the alias after the transition", payload)
	}
}

func testAliasAliasToFile(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeRoutes(t, env, toolRoutes)
	env.source(t, ".config/tool", "content")
	if err := os.MkdirAll(filepath.Join(env.home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("alias apply: %+v", result)
	}
	env.source(t, "x/tool", "content")
	writeRoutes(t, env, "version = 1\n\n[symlinks.all]\n")
	result := env.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("transition apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if string(env.target(t, "tool")) != "content" {
		t.Fatal("the file must replace the alias after the transition")
	}
}
