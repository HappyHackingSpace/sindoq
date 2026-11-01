package langdetect

import (
	"strings"
)

// RuntimeInfo maps languages to execution details.
type RuntimeInfo struct {
	// Language is the canonical language name.
	Language string

	// Aliases are alternative names for the language.
	Aliases []string

	// Runtime is the interpreter/compiler command (e.g., "python3", "node", "go").
	Runtime string

	// FileExt is the typical file extension (e.g., ".py", ".js", ".go").
	FileExt string

	// RunCommand is the command to execute code (args after command).
	RunCommand []string

	// CompileCmd is the optional compile step (nil if interpreted).
	CompileCmd []string

	// DockerImage is the default Docker image for this language.
	DockerImage string

	// REPLMode indicates if bare expressions produce output.
	REPLMode bool
}

// DefaultRuntimes provides default runtime configurations.
var DefaultRuntimes = map[string]*RuntimeInfo{
	"Python": {
		Language:    "Python",
		Aliases:     []string{"python", "python3", "py"},
		Runtime:     "python3",
		FileExt:     ".py",
		RunCommand:  []string{"python3"},
		DockerImage: "python:3.12-slim",
		REPLMode:    false,
	},
	"Go": {
		Language:    "Go",
		Aliases:     []string{"go", "golang"},
		Runtime:     "go",
		FileExt:     ".go",
		RunCommand:  []string{"go", "run"},
		DockerImage: "golang:1.25-alpine",
		REPLMode:    false,
	},
	"JavaScript": {
		Language:    "JavaScript",
		Aliases:     []string{"javascript", "js", "node", "nodejs"},
		Runtime:     "node",
		FileExt:     ".js",
		RunCommand:  []string{"node"},
		DockerImage: "node:22-slim",
		REPLMode:    false,
	},
	"TypeScript": {
		Language:    "TypeScript",
		Aliases:     []string{"typescript", "ts"},
		Runtime:     "ts-node",
		FileExt:     ".ts",
		RunCommand:  []string{"npx", "ts-node"},
		DockerImage: "node:22-slim",
		REPLMode:    false,
	},
	"Rust": {
		Language:    "Rust",
		Aliases:     []string{"rust", "rs"},
		Runtime:     "rustc",
		FileExt:     ".rs",
		CompileCmd:  []string{"rustc", "-o", "/tmp/main"},
		RunCommand:  []string{"/tmp/main"},
		DockerImage: "rust:1.75-slim",
		REPLMode:    false,
	},
	"Java": {
		Language: "Java",
		Aliases:  []string{"java"},
		Runtime:  "java",
		FileExt:  ".java",
		// No compile step: providers only append the source path for
		// interpreted languages, so Java uses single-file source launch
		// (java Main.java, Java 11+). With a compile step, the run command
		// was a bare `java` with no class argument and always failed.
		RunCommand:  []string{"java"},
		DockerImage: "eclipse-temurin:21-jdk",
		REPLMode:    false,
	},
	"C": {
		Language:    "C",
		Aliases:     []string{"c"},
		Runtime:     "gcc",
		FileExt:     ".c",
		CompileCmd:  []string{"gcc", "-o", "/tmp/main"},
		RunCommand:  []string{"/tmp/main"},
		DockerImage: "gcc:14",
		REPLMode:    false,
	},
	"C++": {
		Language:    "C++",
		Aliases:     []string{"cpp", "c++", "cxx"},
		Runtime:     "g++",
		FileExt:     ".cpp",
		CompileCmd:  []string{"g++", "-o", "/tmp/main"},
		RunCommand:  []string{"/tmp/main"},
		DockerImage: "gcc:14",
		REPLMode:    false,
	},
	"Ruby": {
		Language:    "Ruby",
		Aliases:     []string{"ruby", "rb"},
		Runtime:     "ruby",
		FileExt:     ".rb",
		RunCommand:  []string{"ruby"},
		DockerImage: "ruby:3.3-slim",
		REPLMode:    false,
	},
	"PHP": {
		Language:    "PHP",
		Aliases:     []string{"php"},
		Runtime:     "php",
		FileExt:     ".php",
		RunCommand:  []string{"php"},
		DockerImage: "php:8.3-cli",
		REPLMode:    false,
	},
	"Shell": {
		Language:    "Shell",
		Aliases:     []string{"shell", "bash", "sh"},
		Runtime:     "bash",
		FileExt:     ".sh",
		RunCommand:  []string{"bash"},
		DockerImage: "bash:5",
		REPLMode:    false,
	},
	"R": {
		Language:    "R",
		Aliases:     []string{"r"},
		Runtime:     "Rscript",
		FileExt:     ".R",
		RunCommand:  []string{"Rscript"},
		DockerImage: "r-base:4.3.2",
		REPLMode:    false,
	},
	"Kotlin": {
		Language:    "Kotlin",
		Aliases:     []string{"kotlin", "kt"},
		Runtime:     "kotlin",
		FileExt:     ".kt",
		RunCommand:  []string{"kotlin"},
		DockerImage: "zenika/kotlin:1.9",
		REPLMode:    false,
	},
	"Swift": {
		Language:    "Swift",
		Aliases:     []string{"swift"},
		Runtime:     "swift",
		FileExt:     ".swift",
		RunCommand:  []string{"swift"},
		DockerImage: "swift:5.9",
		REPLMode:    false,
	},
	"Scala": {
		Language:    "Scala",
		Aliases:     []string{"scala"},
		Runtime:     "scala",
		FileExt:     ".scala",
		RunCommand:  []string{"scala"},
		DockerImage: "sbtscala/scala-sbt:eclipse-temurin-21.0.1_12_1.9.7_3.3.1",
		REPLMode:    false,
	},
	"Perl": {
		Language:    "Perl",
		Aliases:     []string{"perl", "pl"},
		Runtime:     "perl",
		FileExt:     ".pl",
		RunCommand:  []string{"perl"},
		DockerImage: "perl:5.38",
		REPLMode:    false,
	},
	"Lua": {
		Language:    "Lua",
		Aliases:     []string{"lua"},
		Runtime:     "lua",
		FileExt:     ".lua",
		RunCommand:  []string{"lua"},
		DockerImage: "nickblah/lua:5.4",
		REPLMode:    false,
	},
	"Haskell": {
		Language:    "Haskell",
		Aliases:     []string{"haskell", "hs"},
		Runtime:     "runhaskell",
		FileExt:     ".hs",
		RunCommand:  []string{"runhaskell"},
		DockerImage: "haskell:9.4",
		REPLMode:    false,
	},
	"Elixir": {
		Language:    "Elixir",
		Aliases:     []string{"elixir", "ex"},
		Runtime:     "elixir",
		FileExt:     ".exs",
		RunCommand:  []string{"elixir"},
		DockerImage: "elixir:1.16",
		REPLMode:    false,
	},
	"Clojure": {
		Language:    "Clojure",
		Aliases:     []string{"clojure", "clj"},
		Runtime:     "clojure",
		FileExt:     ".clj",
		RunCommand:  []string{"clojure"},
		DockerImage: "clojure:tools-deps",
		REPLMode:    false,
	},
	"SQL": {
		Language:    "SQL",
		Aliases:     []string{"sql"},
		Runtime:     "sqlite3",
		FileExt:     ".sql",
		RunCommand:  []string{"sqlite3", ":memory:"},
		DockerImage: "keinos/sqlite3:latest",
		REPLMode:    false,
	},
}

// aliasMap is built at init for fast lookups
var aliasMap map[string]*RuntimeInfo

func init() {
	aliasMap = make(map[string]*RuntimeInfo)
	for _, info := range DefaultRuntimes {
		// Add canonical name
		aliasMap[strings.ToLower(info.Language)] = info
		// Add aliases
		for _, alias := range info.Aliases {
			aliasMap[strings.ToLower(alias)] = info
		}
	}
}

// GetRuntimeInfo returns runtime info for a language.
func GetRuntimeInfo(language string) (*RuntimeInfo, bool) {
	// Try exact match first
	if info, ok := DefaultRuntimes[language]; ok {
		return info, true
	}

	// Try alias lookup
	if info, ok := aliasMap[strings.ToLower(language)]; ok {
		return info, true
	}

	return nil, false
}

// GetDockerImage returns the Docker image for a language.
func GetDockerImage(language string) string {
	if info, ok := GetRuntimeInfo(language); ok {
		return info.DockerImage
	}
	return ""
}

// GetFileExtension returns the file extension for a language.
func GetFileExtension(language string) string {
	if info, ok := GetRuntimeInfo(language); ok {
		return info.FileExt
	}
	return ""
}

// GetRunCommand returns the run command for a language.
func GetRunCommand(language string) []string {
	if info, ok := GetRuntimeInfo(language); ok {
		return info.RunCommand
	}
	return nil
}

// NeedsCompilation returns true if the language requires compilation.
func NeedsCompilation(language string) bool {
	if info, ok := GetRuntimeInfo(language); ok {
		return info.CompileCmd != nil
	}
	return false
}

// SupportedLanguages returns all supported language names.
func SupportedLanguages() []string {
	languages := make([]string, 0, len(DefaultRuntimes))
	for lang := range DefaultRuntimes {
		languages = append(languages, lang)
	}
	return languages
}

// RuntimeRegistry allows custom runtime registration.
type RuntimeRegistry struct {
	runtimes map[string]*RuntimeInfo
	aliases  map[string]*RuntimeInfo
}

// NewRuntimeRegistry creates a new registry with defaults.
func NewRuntimeRegistry() *RuntimeRegistry {
	r := &RuntimeRegistry{
		runtimes: make(map[string]*RuntimeInfo),
		aliases:  make(map[string]*RuntimeInfo),
	}

	// Copy defaults
	for name, info := range DefaultRuntimes {
		r.Register(name, info)
	}

	return r
}

// Register adds or updates a runtime.
func (r *RuntimeRegistry) Register(name string, info *RuntimeInfo) {
	r.runtimes[name] = info
	r.aliases[strings.ToLower(name)] = info
	for _, alias := range info.Aliases {
		r.aliases[strings.ToLower(alias)] = info
	}
}

// Get retrieves runtime info.
func (r *RuntimeRegistry) Get(language string) (*RuntimeInfo, bool) {
	if info, ok := r.runtimes[language]; ok {
		return info, true
	}
	if info, ok := r.aliases[strings.ToLower(language)]; ok {
		return info, true
	}
	return nil, false
}

// List returns all registered languages.
func (r *RuntimeRegistry) List() []string {
	languages := make([]string, 0, len(r.runtimes))
	for lang := range r.runtimes {
		languages = append(languages, lang)
	}
	return languages
}
