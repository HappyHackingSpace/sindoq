package langdetect

import (
	"strings"
	"testing"
)

func TestGetRuntimeInfo(t *testing.T) {
	tests := []struct {
		language    string
		wantRuntime string
		wantOK      bool
	}{
		{"Python", "python3", true},
		{"JavaScript", "node", true},
		{"TypeScript", "ts-node", true},
		{"Go", "go", true},
		{"Java", "java", true},
		{"Rust", "rustc", true},
		{"Ruby", "ruby", true},
		{"PHP", "php", true},
		{"C", "gcc", true},
		{"C++", "g++", true},
		{"Shell", "bash", true},
		{"R", "Rscript", true},
		{"Swift", "swift", true},
		{"Kotlin", "kotlin", true},
		{"Scala", "scala", true},
		{"Perl", "perl", true},
		{"Lua", "lua", true},
		{"Haskell", "runhaskell", true},
		{"Elixir", "elixir", true},
		{"Clojure", "clojure", true},
		{"Unknown", "", false},
		{"NonExistent", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			info, ok := GetRuntimeInfo(tt.language)
			if ok != tt.wantOK {
				t.Errorf("GetRuntimeInfo(%q) ok = %v, want %v", tt.language, ok, tt.wantOK)
			}
			if ok && info.Runtime != tt.wantRuntime {
				t.Errorf("GetRuntimeInfo(%q).Runtime = %q, want %q", tt.language, info.Runtime, tt.wantRuntime)
			}
		})
	}
}

func TestGetRuntimeInfo_Aliases(t *testing.T) {
	tests := []struct {
		alias    string
		wantLang string
	}{
		{"python", "Python"},
		{"python3", "Python"},
		{"py", "Python"},
		{"js", "JavaScript"},
		{"node", "JavaScript"},
		{"nodejs", "JavaScript"},
		{"go", "Go"},
		{"golang", "Go"},
		{"ts", "TypeScript"},
		{"rust", "Rust"},
		{"rs", "Rust"},
		{"bash", "Shell"},
		{"sh", "Shell"},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			info, ok := GetRuntimeInfo(tt.alias)
			if !ok {
				t.Errorf("GetRuntimeInfo(%q) not found", tt.alias)
				return
			}
			if info.Language != tt.wantLang {
				t.Errorf("GetRuntimeInfo(%q).Language = %q, want %q", tt.alias, info.Language, tt.wantLang)
			}
		})
	}
}

func TestGetDockerImage(t *testing.T) {
	tests := []struct {
		language  string
		wantImage string
	}{
		{"Python", "python:3.12-slim"},
		{"JavaScript", "node:22-slim"},
		{"TypeScript", "node:22-slim"},
		{"Go", "golang:1.25-alpine"},
		{"Java", "eclipse-temurin:21-jdk"},
		{"Rust", "rust:1.75-slim"},
		{"Ruby", "ruby:3.3-slim"},
		{"PHP", "php:8.3-cli"},
		{"Unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			image := GetDockerImage(tt.language)
			if image != tt.wantImage {
				t.Errorf("GetDockerImage(%q) = %q, want %q", tt.language, image, tt.wantImage)
			}
		})
	}
}

func TestGetFileExtension(t *testing.T) {
	tests := []struct {
		language string
		wantExt  string
	}{
		{"Python", ".py"},
		{"JavaScript", ".js"},
		{"TypeScript", ".ts"},
		{"Go", ".go"},
		{"Java", ".java"},
		{"Rust", ".rs"},
		{"Ruby", ".rb"},
		{"PHP", ".php"},
		{"C", ".c"},
		{"C++", ".cpp"},
		{"Shell", ".sh"},
		{"R", ".R"},
		{"Swift", ".swift"},
		{"Unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			ext := GetFileExtension(tt.language)
			if ext != tt.wantExt {
				t.Errorf("GetFileExtension(%q) = %q, want %q", tt.language, ext, tt.wantExt)
			}
		})
	}
}

func TestGetRunCommand(t *testing.T) {
	tests := []struct {
		language string
		wantCmd  []string
	}{
		{"Python", []string{"python3"}},
		{"JavaScript", []string{"node"}},
		{"Go", []string{"go", "run"}},
		{"Unknown", nil},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			cmd := GetRunCommand(tt.language)
			if !sliceEqual(cmd, tt.wantCmd) {
				t.Errorf("GetRunCommand(%q) = %v, want %v", tt.language, cmd, tt.wantCmd)
			}
		})
	}
}

func TestNeedsCompilation(t *testing.T) {
	tests := []struct {
		language string
		wantComp bool
	}{
		{"Python", false},
		{"JavaScript", false},
		{"Go", false}, // go run handles compilation
		{"Rust", true},
		{"Java", true},
		{"C", true},
		{"C++", true},
		{"Unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			needs := NeedsCompilation(tt.language)
			if needs != tt.wantComp {
				t.Errorf("NeedsCompilation(%q) = %v, want %v", tt.language, needs, tt.wantComp)
			}
		})
	}
}

func TestRuntimeRegistry(t *testing.T) {
	r := NewRuntimeRegistry()

	// Test getting a default runtime
	info, ok := r.Get("Python")
	if !ok {
		t.Error("Registry should have Python by default")
	}
	if info.Runtime != "python3" {
		t.Errorf("Python runtime = %q, want %q", info.Runtime, "python3")
	}

	// Test registering custom runtime
	customRuntime := &RuntimeInfo{
		Language:    "MyLang",
		Aliases:     []string{"ml", "mylang"},
		Runtime:     "mylang",
		FileExt:     ".ml",
		RunCommand:  []string{"mylang"},
		DockerImage: "mylang:latest",
	}
	r.Register("MyLang", customRuntime)

	// Test getting custom runtime by name
	info, ok = r.Get("MyLang")
	if !ok {
		t.Error("Registry should have MyLang after registration")
	}
	if info.Runtime != "mylang" {
		t.Errorf("MyLang runtime = %q, want %q", info.Runtime, "mylang")
	}

	// Test getting custom runtime by alias
	info, ok = r.Get("ml")
	if !ok {
		t.Error("Registry should find MyLang by alias 'ml'")
	}
	if info.Language != "MyLang" {
		t.Errorf("Alias 'ml' language = %q, want %q", info.Language, "MyLang")
	}

	// Test listing languages
	langs := r.List()
	found := false
	for _, l := range langs {
		if l == "MyLang" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Registry.List() should include MyLang")
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCompiledLanguagesRunTheirOwnBinary documents the contract providers rely
// on since #37: compiled languages must not hardcode a shared output path.
// CompileCmd and RunCommand carry OutputBinaryPlaceholder instead, which
// providers resolve per execution via ResolveCompileCommand/ResolveRunCommand.
func TestCompiledLanguagesRunTheirOwnBinary(t *testing.T) {
	for name, info := range DefaultRuntimes {
		if info.CompileCmd == nil {
			continue
		}
		hasOutput := false
		for _, arg := range info.CompileCmd {
			if arg == OutputBinaryPlaceholder {
				hasOutput = true
			}
			if strings.HasPrefix(arg, "/tmp/") {
				t.Errorf("%s: CompileCmd %v hardcodes a shared output path; use OutputBinaryPlaceholder", name, info.CompileCmd)
			}
		}
		for _, arg := range info.RunCommand {
			if strings.HasPrefix(arg, "/tmp/") {
				t.Errorf("%s: RunCommand %v hardcodes a shared binary path; use OutputBinaryPlaceholder", name, info.RunCommand)
			}
		}
		if !hasOutput {
			continue
		}
		runsOutput := false
		for _, arg := range info.RunCommand {
			if arg == OutputBinaryPlaceholder {
				runsOutput = true
			}
		}
		if !runsOutput {
			t.Errorf("%s: CompileCmd produces a per-execution binary but RunCommand %v never references it", name, info.RunCommand)
		}
	}
}

func TestCompileOutputPath(t *testing.T) {
	tests := []struct {
		codePath string
		want     string
	}{
		{"/workspace/main.c", "/workspace/main"},
		{"/tmp/sindoq-exec-abc/main.rs", "/tmp/sindoq-exec-abc/main"},
		{"main.cpp", "main"},
	}
	for _, tt := range tests {
		if got := CompileOutputPath(tt.codePath); got != tt.want {
			t.Errorf("CompileOutputPath(%q) = %q, want %q", tt.codePath, got, tt.want)
		}
	}
}

func TestResolveCompileCommand(t *testing.T) {
	c, _ := GetRuntimeInfo("C")
	got := ResolveCompileCommand(c, "/workspace/main.c")
	want := []string{"gcc", "-o", "/workspace/main", "/workspace/main.c"}
	if !sliceEqual(got, want) {
		t.Errorf("ResolveCompileCommand(C) = %v, want %v", got, want)
	}

	rust, _ := GetRuntimeInfo("Rust")
	got = ResolveCompileCommand(rust, "/exec-b/main.rs")
	want = []string{"rustc", "-o", "/exec-b/main", "/exec-b/main.rs"}
	if !sliceEqual(got, want) {
		t.Errorf("ResolveCompileCommand(Rust) = %v, want %v", got, want)
	}

	py, _ := GetRuntimeInfo("Python")
	if got := ResolveCompileCommand(py, "/workspace/main.py"); got != nil {
		t.Errorf("ResolveCompileCommand(Python) = %v, want nil", got)
	}

	javac := &RuntimeInfo{
		Language:   "Java",
		CompileCmd: []string{"javac"},
		RunCommand: []string{"java"},
	}
	got = ResolveCompileCommand(javac, "/workspace/Main.java")
	want = []string{"javac", "/workspace/Main.java"}
	if !sliceEqual(got, want) {
		t.Errorf("ResolveCompileCommand(javac) = %v, want %v", got, want)
	}
}

func TestResolveRunCommand(t *testing.T) {
	c, _ := GetRuntimeInfo("C")
	got := ResolveRunCommand(c, "/workspace/main.c")
	want := []string{"/workspace/main"}
	if !sliceEqual(got, want) {
		t.Errorf("ResolveRunCommand(C) = %v, want %v", got, want)
	}

	py, _ := GetRuntimeInfo("Python")
	got = ResolveRunCommand(py, "/workspace/main.py")
	want = []string{"python3"}
	if !sliceEqual(got, want) {
		t.Errorf("ResolveRunCommand(Python) = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if py.RunCommand[0] != "python3" {
		t.Error("ResolveRunCommand must return a copy, not the table slice")
	}
}

// TestResolveCommandsPerExecutionIsolation is the #37 regression test: two
// executions in different code directories must compile to and run different
// binaries, so neither can pick up the other's output.
func TestResolveCommandsPerExecutionIsolation(t *testing.T) {
	c, _ := GetRuntimeInfo("C")
	rust, _ := GetRuntimeInfo("Rust")

	outA := CompileOutputPath("/exec-a/main.c")
	outB := CompileOutputPath("/exec-b/main.rs")
	if outA == outB {
		t.Fatalf("executions in different directories share output path %q", outA)
	}

	runA := ResolveRunCommand(c, "/exec-a/main.c")
	runB := ResolveRunCommand(rust, "/exec-b/main.rs")
	if sliceEqual(runA, runB) {
		t.Errorf("run commands collide across executions: %v", runA)
	}
	if runA[0] != outA {
		t.Errorf("C runs %q, want its own compile output %q", runA[0], outA)
	}
	if runB[0] != outB {
		t.Errorf("Rust runs %q, want its own compile output %q", runB[0], outB)
	}
}
