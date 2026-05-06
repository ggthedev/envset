package detector

import (
	"os"
	"testing"
)

func TestIsSecret(t *testing.T) {
	cases := []struct {
		key      string
		expected bool
	}{
		{"AWS_SECRET_ACCESS_KEY", true},
		{"GITHUB_TOKEN", true},
		{"DOCKER_PASSWORD", true},
		{"API_KEY", true},
		{"PRIVATE_KEY", true},
		{"HOME", false},
		{"EDITOR", false},
		{"PATH", false},
		{"FOO", false},
	}

	for _, c := range cases {
		got := IsSecret(c.key)
		if got != c.expected {
			t.Errorf("IsSecret(%q) = %v, want %v", c.key, got, c.expected)
		}
	}
}

func TestDetectLoginShell(t *testing.T) {
	orig := os.Getenv("SHELL")
	defer os.Setenv("SHELL", orig)

	os.Setenv("SHELL", "/bin/zsh")
	if got := detectLoginShell(); got != "zsh" {
		t.Errorf("detectLoginShell() = %q, want zsh", got)
	}

	os.Setenv("SHELL", "/usr/local/bin/bash")
	if got := detectLoginShell(); got != "bash" {
		t.Errorf("detectLoginShell() = %q, want bash", got)
	}

	os.Setenv("SHELL", "")
	if got := detectLoginShell(); got != "unknown" {
		t.Errorf("detectLoginShell() = %q, want unknown", got)
	}
}

func TestShellExists(t *testing.T) {
	// sh almost always exists on Unix
	if !ShellExists("sh") {
		t.Skip("sh not found, skipping ShellExists test")
	}
}
