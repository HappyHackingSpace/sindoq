//go:build microsandbox && cgo && integration

package microsandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
)

func newTestInstance(t *testing.T, ctx context.Context, p *Provider, opts *provider.CreateOptions) provider.Instance {
	t.Helper()
	instance, err := p.Create(ctx, opts)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	t.Cleanup(func() {
		if err := instance.Stop(context.Background()); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})
	return instance
}

func TestMicrosandboxProviderIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	p, err := New(nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := p.Validate(ctx); err != nil {
		t.Skipf("microsandbox not available: %v", err)
	}

	instance := newTestInstance(t, ctx, p, &provider.CreateOptions{
		Runtime:     "Python",
		WorkDir:     "/workspace",
		Environment: map[string]string{"SANDBOX_ENV": "from-create"},
		Resources:   provider.ResourceConfig{MemoryMB: 512, CPUs: 1},
	})

	t.Run("status running", func(t *testing.T) {
		status, err := instance.Status(ctx)
		if err != nil || status != provider.StatusRunning {
			t.Errorf("Status() = %q, %v; want running", status, err)
		}
	})

	t.Run("execute python", func(t *testing.T) {
		result, err := instance.Execute(ctx, `print("Hello from microVM!")`, &executor.ExecutionOptions{Language: "Python"})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "Hello from microVM!" {
			t.Errorf("got exit=%d stdout=%q stderr=%q", result.ExitCode, result.Stdout, result.Stderr)
		}
		if result.Language != "Python" || result.Duration <= 0 {
			t.Errorf("Language = %q, Duration = %v", result.Language, result.Duration)
		}
	})

	t.Run("exit code and stderr", func(t *testing.T) {
		result, err := instance.Execute(ctx, "import sys\nprint('oops', file=sys.stderr)\nsys.exit(3)", &executor.ExecutionOptions{Language: "Python"})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.ExitCode != 3 || strings.TrimSpace(result.Stderr) != "oops" {
			t.Errorf("got exit=%d stderr=%q", result.ExitCode, result.Stderr)
		}
	})

	t.Run("env stdin workdir files", func(t *testing.T) {
		code := `import os, sys
print(os.environ["SANDBOX_ENV"], os.environ["EXEC_ENV"])
print(sys.stdin.read().strip())
print(os.getcwd())
print(open("data/input.txt").read())`
		result, err := instance.Execute(ctx, code, &executor.ExecutionOptions{
			Language: "Python",
			Env:      map[string]string{"EXEC_ENV": "from-exec"},
			Stdin:    "piped input",
			WorkDir:  "/app",
			Files:    map[string][]byte{"data/input.txt": []byte("file content")},
		})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		want := "from-create from-exec\npiped input\n/app\nfile content\n"
		if result.ExitCode != 0 || result.Stdout != want {
			t.Errorf("got exit=%d stdout=%q stderr=%q, want stdout %q", result.ExitCode, result.Stdout, result.Stderr, want)
		}
	})

	t.Run("code with heredoc-like content is not interpreted", func(t *testing.T) {
		result, err := instance.Execute(ctx, "s = '''\nEOF\n$(echo pwned)\n'''\nprint(s.strip())", &executor.ExecutionOptions{Language: "Python"})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Stdout != "EOF\n$(echo pwned)\n" {
			t.Errorf("stdout = %q", result.Stdout)
		}
	})

	t.Run("shell language", func(t *testing.T) {
		result, err := instance.Execute(ctx, `echo "shell $((2+3))"`, &executor.ExecutionOptions{Language: "Shell"})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.ExitCode != 0 || result.Stdout != "shell 5\n" {
			t.Errorf("got exit=%d stdout=%q stderr=%q", result.ExitCode, result.Stdout, result.Stderr)
		}
	})

	t.Run("unsupported language", func(t *testing.T) {
		if _, err := instance.Execute(ctx, "x", &executor.ExecutionOptions{Language: "NoSuchLanguage"}); err == nil {
			t.Error("expected error for unsupported language")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		start := time.Now()
		_, err := instance.Execute(ctx, "import time\ntime.sleep(30)", &executor.ExecutionOptions{Language: "Python", Timeout: 2 * time.Second})
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if elapsed := time.Since(start); elapsed > 15*time.Second {
			t.Errorf("timeout took %v", elapsed)
		}
	})

	t.Run("stream", func(t *testing.T) {
		var stdout, stderr strings.Builder
		var types []executor.StreamEventType
		exitCode := -1
		err := instance.ExecuteStream(ctx, "import sys\nfor i in range(3):\n    print(i, flush=True)\nprint('err', file=sys.stderr)\nsys.exit(2)",
			&executor.ExecutionOptions{Language: "Python"},
			func(e *executor.StreamEvent) error {
				types = append(types, e.Type)
				switch e.Type {
				case executor.StreamStdout:
					stdout.WriteString(e.Data)
				case executor.StreamStderr:
					stderr.WriteString(e.Data)
				case executor.StreamComplete:
					exitCode = e.ExitCode
				}
				return nil
			})
		if err != nil {
			t.Fatalf("ExecuteStream() error = %v", err)
		}
		if stdout.String() != "0\n1\n2\n" || stderr.String() != "err\n" || exitCode != 2 {
			t.Errorf("stdout=%q stderr=%q exit=%d", stdout.String(), stderr.String(), exitCode)
		}
		// StreamStart comes from sindoq.ExecuteStream, not from providers.
		for _, typ := range types {
			if typ == executor.StreamStart {
				t.Errorf("provider must not emit StreamStart, got %v", types)
			}
		}
		if types[len(types)-1] != executor.StreamComplete {
			t.Errorf("event order = %v", types)
		}
	})

	t.Run("stream stdin", func(t *testing.T) {
		var stdout strings.Builder
		err := instance.ExecuteStream(ctx, "import sys\nprint(sys.stdin.read().upper())",
			&executor.ExecutionOptions{Language: "Python", Stdin: "hello"},
			func(e *executor.StreamEvent) error {
				if e.Type == executor.StreamStdout {
					stdout.WriteString(e.Data)
				}
				return nil
			})
		if err != nil || stdout.String() != "HELLO\n" {
			t.Errorf("stdout=%q err=%v", stdout.String(), err)
		}
	})

	t.Run("stream handler error stops execution", func(t *testing.T) {
		stop := errors.New("stop")
		start := time.Now()
		err := instance.ExecuteStream(ctx, "import time\nwhile True:\n    print('tick', flush=True)\n    time.sleep(0.1)",
			&executor.ExecutionOptions{Language: "Python"},
			func(e *executor.StreamEvent) error {
				if e.Type == executor.StreamStdout {
					return stop
				}
				return nil
			})
		if !errors.Is(err, stop) {
			t.Errorf("err = %v, want handler error", err)
		}
		if elapsed := time.Since(start); elapsed > 15*time.Second {
			t.Errorf("handler error took %v to stop", elapsed)
		}
	})

	t.Run("run command", func(t *testing.T) {
		result, err := instance.RunCommand(ctx, "sh", []string{"-c", "echo $0-$1; exit 4", "a b", "c"})
		if err != nil {
			t.Fatalf("RunCommand() error = %v", err)
		}
		if result.ExitCode != 4 || result.Stdout != "a b-c\n" {
			t.Errorf("got exit=%d stdout=%q", result.ExitCode, result.Stdout)
		}
	})

	t.Run("filesystem", func(t *testing.T) {
		fsys := instance.FileSystem()

		if err := fsys.Write(ctx, "/workspace/nested/dir/a.txt", []byte("alpha")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		data, err := fsys.Read(ctx, "/workspace/nested/dir/a.txt")
		if err != nil || string(data) != "alpha" {
			t.Fatalf("Read() = %q, %v", data, err)
		}

		st, err := fsys.Stat(ctx, "/workspace/nested/dir/a.txt")
		if err != nil || st.Size != 5 || st.IsDir || st.Name != "a.txt" {
			t.Errorf("Stat() = %+v, %v", st, err)
		}

		if err := fsys.Copy(ctx, "/workspace/nested/dir/a.txt", "/workspace/nested/b.txt"); err != nil {
			t.Fatalf("Copy() error = %v", err)
		}
		if err := fsys.Move(ctx, "/workspace/nested/b.txt", "/workspace/nested/c.txt"); err != nil {
			t.Fatalf("Move() error = %v", err)
		}
		if ok, _ := fsys.Exists(ctx, "/workspace/nested/b.txt"); ok {
			t.Error("b.txt should not exist after Move")
		}

		entries, err := fsys.List(ctx, "/workspace/nested")
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		got := map[string]bool{}
		for _, e := range entries {
			got[e.Name] = e.IsDir
		}
		if isDir, ok := got["dir"]; !ok || !isDir {
			t.Errorf("List() should contain dir/, got %+v", entries)
		}
		if isDir, ok := got["c.txt"]; !ok || isDir {
			t.Errorf("List() should contain c.txt, got %+v", entries)
		}

		local := filepath.Join(t.TempDir(), "local.txt")
		if err := os.WriteFile(local, []byte("from host"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := fsys.Upload(ctx, local, "/workspace/up/local.txt"); err != nil {
			t.Fatalf("Upload() error = %v", err)
		}
		if err := fsys.UploadReader(ctx, strings.NewReader("from reader"), "/workspace/up/reader.txt"); err != nil {
			t.Fatalf("UploadReader() error = %v", err)
		}
		for remote, want := range map[string]string{"/workspace/up/local.txt": "from host", "/workspace/up/reader.txt": "from reader"} {
			var buf bytes.Buffer
			if err := fsys.Download(ctx, remote, &buf); err != nil || buf.String() != want {
				t.Errorf("Download(%s) = %q, %v; want %q", remote, buf.String(), err, want)
			}
		}

		if err := fsys.Delete(ctx, "/workspace/nested"); err != nil {
			t.Fatalf("Delete(dir) error = %v", err)
		}
		if err := fsys.Delete(ctx, "/workspace/up/local.txt"); err != nil {
			t.Fatalf("Delete(file) error = %v", err)
		}
		for _, p := range []string{"/workspace/nested", "/workspace/up/local.txt"} {
			if ok, err := fsys.Exists(ctx, p); ok || err != nil {
				t.Errorf("Exists(%s) = %v, %v after Delete", p, ok, err)
			}
		}

		if _, err := fsys.Read(ctx, "/does/not/exist"); err == nil {
			t.Error("Read() of missing file should fail")
		}
	})
}

func TestMicrosandboxNetworkIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	p, _ := New(nil)
	if err := p.Validate(ctx); err != nil {
		t.Skipf("microsandbox not available: %v", err)
	}

	code := `import urllib.request
try:
    urllib.request.urlopen("http://example.com", timeout=5)
    print("online")
except Exception:
    print("offline")`

	for _, tt := range []struct {
		internet bool
		want     string
	}{{false, "offline"}, {true, "online"}} {
		instance := newTestInstance(t, ctx, p, &provider.CreateOptions{Runtime: "Python", InternetAccess: tt.internet})
		result, err := instance.Execute(ctx, code, &executor.ExecutionOptions{Language: "Python"})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if strings.TrimSpace(result.Stdout) != tt.want {
			t.Errorf("InternetAccess=%v: stdout=%q stderr=%q, want %s", tt.internet, result.Stdout, result.Stderr, tt.want)
		}
	}
}

func TestMicrosandboxLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	p, _ := New(nil)
	if err := p.Validate(ctx); err != nil {
		t.Skipf("microsandbox not available: %v", err)
	}

	// Create must not depend on ctx staying alive (the SDK guarantees this).
	createCtx, createCancel := context.WithCancel(ctx)
	instance, err := p.Create(createCtx, &provider.CreateOptions{Runtime: "Python"})
	createCancel()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := instance.Execute(ctx, "print(1)", &executor.ExecutionOptions{Language: "Python"}); err != nil {
		t.Fatalf("Execute() after create ctx cancel error = %v", err)
	}

	// CreateOptions.Timeout carries sindoq's default execution timeout (30s);
	// the sandbox must keep running past it.
	short := newTestInstance(t, ctx, p, &provider.CreateOptions{Runtime: "Python", Timeout: 2 * time.Second})
	time.Sleep(5 * time.Second)
	if _, err := short.Execute(ctx, "print(1)", &executor.ExecutionOptions{Language: "Python"}); err != nil {
		t.Errorf("sandbox should outlive CreateOptions.Timeout, Execute() error = %v", err)
	}

	if err := instance.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := instance.Stop(ctx); err != nil {
		t.Errorf("second Stop() should be a no-op, got %v", err)
	}
	if status, _ := instance.Status(ctx); status != provider.StatusStopped {
		t.Errorf("Status() after Stop = %q", status)
	}
	if _, err := instance.Execute(ctx, "print(1)", &executor.ExecutionOptions{Language: "Python"}); err == nil {
		t.Error("Execute() after Stop should fail")
	}
}

// Compiled languages pull large images (gcc, rust, jdk); skip them with -short.
func TestMicrosandboxCompiledLanguages(t *testing.T) {
	if testing.Short() {
		t.Skip("pulls large compiler images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	p, _ := New(nil)
	if err := p.Validate(ctx); err != nil {
		t.Skipf("microsandbox not available: %v", err)
	}

	tests := []struct {
		language string
		code     string
		broken   string
	}{
		{"C", "#include <stdio.h>\nint main(void) { printf(\"hello from C\\n\"); return 0; }", "int main(void) { return x; }"},
		{"C++", "#include <iostream>\nint main() { std::cout << \"hello from C++\" << std::endl; }", "int main() { return x; }"},
		{"Rust", "fn main() { println!(\"hello from Rust\"); }", "fn main() { let x: i32 = \"no\"; }"},
		// Java is left out: the shared runtime table (pkg/langdetect/runtime.go) compiles
		// with "javac" and then runs a bare "java" with no class, which only prints usage
		// and exits 1 (seen here; other providers use the same table). Running "java main.java" (single-file source
		// launch) works in the eclipse-temurin:21-jdk image; add Java back once that is fixed.
	}
	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			instance := newTestInstance(t, ctx, p, &provider.CreateOptions{
				Runtime:   tt.language,
				WorkDir:   "/workspace",
				Resources: provider.ResourceConfig{MemoryMB: 2048, CPUs: 2},
			})

			result, err := instance.Execute(ctx, tt.code, &executor.ExecutionOptions{Language: tt.language, Timeout: 5 * time.Minute})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			want := "hello from " + tt.language
			if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != want {
				t.Errorf("run: exit=%d stdout=%q stderr=%q, want %q", result.ExitCode, result.Stdout, result.Stderr, want)
			}

			result, err = instance.Execute(ctx, tt.broken, &executor.ExecutionOptions{Language: tt.language, Timeout: 5 * time.Minute})
			if err != nil {
				t.Fatalf("Execute(broken) error = %v", err)
			}
			if result.ExitCode == 0 || result.Stderr == "" {
				t.Errorf("compile error should be reported: exit=%d stderr=%q", result.ExitCode, result.Stderr)
			}
		})
	}
}

// Regression tests for bugs found in review.
func TestMicrosandboxExecutionEdgeCases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	p, _ := New(nil)
	if err := p.Validate(ctx); err != nil {
		t.Skipf("microsandbox not available: %v", err)
	}
	instance := newTestInstance(t, ctx, p, &provider.CreateOptions{Runtime: "Python", WorkDir: "/workspace"})

	t.Run("concurrent executions do not overwrite each other", func(t *testing.T) {
		var wg sync.WaitGroup
		for n := 0; n < 8; n++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				result, err := instance.Execute(ctx, fmt.Sprintf("import time\ntime.sleep(0.05)\nprint(%d)", n), &executor.ExecutionOptions{Language: "Python"})
				if err != nil {
					t.Errorf("#%d Execute() error = %v", n, err)
					return
				}
				if got := strings.TrimSpace(result.Stdout); got != fmt.Sprint(n) {
					t.Errorf("#%d ran someone else's code: stdout=%q", n, got)
				}
			}(n)
		}
		wg.Wait()
	})

	t.Run("cwd and files stay in workdir", func(t *testing.T) {
		result, err := instance.Execute(ctx, "import os\nprint(os.getcwd(), open('in.txt').read())", &executor.ExecutionOptions{
			Language: "Python",
			Files:    map[string][]byte{"in.txt": []byte("data")},
		})
		if err != nil || result.Stdout != "/workspace data\n" {
			t.Errorf("stdout=%q stderr=%q err=%v", result.Stdout, result.Stderr, err)
		}
	})

	t.Run("stream ignores stdin the program never reads", func(t *testing.T) {
		var stdout strings.Builder
		err := instance.ExecuteStream(ctx, `print("ignored stdin")`, &executor.ExecutionOptions{
			Language: "Python",
			Stdin:    strings.Repeat("x", 2<<20),
		}, func(e *executor.StreamEvent) error {
			if e.Type == executor.StreamStdout {
				stdout.WriteString(e.Data)
			}
			return nil
		})
		if err != nil || stdout.String() != "ignored stdin\n" {
			t.Errorf("stdout=%q err=%v", stdout.String(), err)
		}
	})

	t.Run("stream timeout is an error like Execute", func(t *testing.T) {
		opts := &executor.ExecutionOptions{Language: "Python", Timeout: 2 * time.Second}
		_, execErr := instance.Execute(ctx, "import time\ntime.sleep(30)", opts)

		start := time.Now()
		streamErr := instance.ExecuteStream(ctx, "import time\ntime.sleep(30)", opts, func(*executor.StreamEvent) error { return nil })
		if execErr == nil || streamErr == nil {
			t.Fatalf("both should time out: Execute err=%v, ExecuteStream err=%v", execErr, streamErr)
		}
		if !strings.Contains(streamErr.Error(), "timed out") {
			t.Errorf("ExecuteStream err = %v, want a timeout error", streamErr)
		}
		if elapsed := time.Since(start); elapsed > 15*time.Second {
			t.Errorf("stream timeout took %v", elapsed)
		}

		// The timed-out guest process must be killed, not left running.
		count := `for p in /proc/[0-9]*; do tr '\0' ' ' < $p/cmdline 2>/dev/null; echo; done | grep -c '[p]ython3 /tmp/sindoq-exec-' || true`
		out, err := instance.RunCommand(ctx, "sh", []string{"-c", count})
		if err != nil || strings.TrimSpace(out.Stdout) != "0" {
			t.Errorf("leftover python processes = %q, err=%v", out.Stdout, err)
		}
	})

	t.Run("code directories are cleaned up unless artifacts are kept", func(t *testing.T) {
		countDirs := func() string {
			out, err := instance.RunCommand(ctx, "sh", []string{"-c", "ls -d /tmp/sindoq-exec-* 2>/dev/null | wc -l"})
			if err != nil {
				t.Fatalf("RunCommand() error = %v", err)
			}
			return strings.TrimSpace(out.Stdout)
		}
		if n := countDirs(); n != "0" {
			t.Errorf("code directories left after earlier executions: %s", n)
		}
		if _, err := instance.Execute(ctx, "print(1)", &executor.ExecutionOptions{Language: "Python", KeepArtifacts: true}); err != nil {
			t.Fatal(err)
		}
		if n := countDirs(); n != "1" {
			t.Errorf("KeepArtifacts should keep the code directory, found %s", n)
		}
	})
}
