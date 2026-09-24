package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "LOCKER_PROVIDER"} {
		t.Setenv(k, "")
	}
}

func TestRunValidateConfig_Valid(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")

	var stdout, stderr bytes.Buffer
	code := runValidateConfig([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "config OK") {
		t.Errorf("stdout = %q, want it to contain %q", stdout.String(), "config OK")
	}
}

func TestRunValidateConfig_MissingAPIKey(t *testing.T) {
	clearProviderEnv(t)

	var stdout, stderr bytes.Buffer
	code := runValidateConfig([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "api_key") {
		t.Errorf("stderr = %q, want it to mention the missing api_key", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on failure", stdout.String())
	}
}

func TestRunValidateConfig_UnsupportedProvider(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("LOCKER_PROVIDER", "cohere")

	var stdout, stderr bytes.Buffer
	code := runValidateConfig([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestVersionString(t *testing.T) {
	got := versionString()
	for _, want := range []string{"locker version", version, commit, date} {
		if !strings.Contains(got, want) {
			t.Errorf("versionString() = %q, want it to contain %q", got, want)
		}
	}
}

func TestRunCompletion_Bash(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCompletion([]string{"bash"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "_locker_completions") {
		t.Errorf("stdout = %q, want a bash completion function", stdout.String())
	}
}

func TestRunCompletion_UnsupportedShell(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCompletion([]string{"zsh"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty for an unsupported shell", stdout.String())
	}
}

func TestRunCompletion_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCompletion(nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
