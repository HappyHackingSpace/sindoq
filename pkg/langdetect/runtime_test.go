package langdetect

import (
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
		{"Java", false}, // single-file source launch, see #36
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

// TestJavaUsesSingleFileSourceLaunch guards against regressing #36: Java must
// be treated as interpreted (no compile step) so providers append the source
// file to `java` and use single-file source launch.
func TestJavaUsesSingleFileSourceLaunch(t *testing.T) {
	info, ok := GetRuntimeInfo("Java")
	if !ok {
		t.Fatal("Java runtime info not found")
	}
	if info.CompileCmd != nil {
		t.Errorf("Java CompileCmd = %v, want nil (single-file source launch)", info.CompileCmd)
	}
	if len(info.RunCommand) != 1 || info.RunCommand[0] != "java" {
		t.Errorf("Java RunCommand = %v, want [java]", info.RunCommand)
	}
	if NeedsCompilation("Java") {
		t.Error("NeedsCompilation(Java) = true, want false")
	}
}

// TestCompiledLanguagesRunTheirOwnBinary documents the contract the providers
// rely on: a language with a compile step must carry the output binary path in
// RunCommand, because providers do not append anything after compiling.
func TestCompiledLanguagesRunTheirOwnBinary(t *testing.T) {
	for name, info := range DefaultRuntimes {
		if info.CompileCmd == nil {
			continue
		}
		if len(info.RunCommand) == 0 || info.RunCommand[0] == info.Runtime {
			t.Errorf("%s: RunCommand %v does not point at the compiled output; providers append nothing after the compile step", name, info.RunCommand)
		}
	}
}
