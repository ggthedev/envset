package detector

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Info holds detected environment information.
type Info struct {
	OS           string
	Shell        string
	HomeDir      string
	ShellConfigs []ShellConfig
}

// ShellConfig represents a shell configuration file.
type ShellConfig struct {
	Name     string
	Path     string
	Shell    string
	Exists   bool
	Priority int // lower = higher priority for suggestions
}

// Detect gathers environment information.
func Detect() (*Info, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not determine home directory: %w", err)
	}

	shell := DetectShell(home)
	configs := detectShellConfigs(home)

	return &Info{
		OS:           runtime.GOOS,
		Shell:        shell,
		HomeDir:      home,
		ShellConfigs: configs,
	}, nil
}

// DetectShell determines the user's shell from SHELL env var.
func DetectShell(home string) string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		// fallback: check common shells
		candidates := []string{"/bin/zsh", "/bin/bash", "/usr/bin/zsh", "/usr/bin/bash", "/bin/fish"}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return filepath.Base(c)
			}
		}
		return "unknown"
	}
	return filepath.Base(shell)
}

func detectShellConfigs(home string) []ShellConfig {
	configs := []ShellConfig{
		{Name: ".zshrc", Path: filepath.Join(home, ".zshrc"), Shell: "zsh", Priority: 1},
		{Name: ".zprofile", Path: filepath.Join(home, ".zprofile"), Shell: "zsh", Priority: 2},
		{Name: ".zshenv", Path: filepath.Join(home, ".zshenv"), Shell: "zsh", Priority: 3},
		{Name: ".bashrc", Path: filepath.Join(home, ".bashrc"), Shell: "bash", Priority: 1},
		{Name: ".bash_profile", Path: filepath.Join(home, ".bash_profile"), Shell: "bash", Priority: 2},
		{Name: ".profile", Path: filepath.Join(home, ".profile"), Shell: "bash", Priority: 3},
		{Name: ".bash_login", Path: filepath.Join(home, ".bash_login"), Shell: "bash", Priority: 4},
		{Name: "config.fish", Path: filepath.Join(home, ".config", "fish", "config.fish"), Shell: "fish", Priority: 1},
	}

	for i := range configs {
		if _, err := os.Stat(configs[i].Path); err == nil {
			configs[i].Exists = true
		}
	}

	return configs
}

// SuggestConfig returns the best config file for the detected shell.
func (i *Info) SuggestConfig() *ShellConfig {
	var best *ShellConfig
	for idx := range i.ShellConfigs {
		c := &i.ShellConfigs[idx]
		if c.Shell != i.Shell {
			continue
		}
		if c.Exists && (best == nil || c.Priority < best.Priority) {
			best = c
		}
	}
	// If no matching shell config exists, suggest the primary one for the shell.
	if best == nil {
		for idx := range i.ShellConfigs {
			c := &i.ShellConfigs[idx]
			if c.Shell == i.Shell && c.Priority == 1 {
				return c
			}
		}
	}
	return best
}

// IsSecret detects if a variable name looks like a secret/API key.
func IsSecret(key string) bool {
	lower := strings.ToLower(key)
	patterns := []string{
		"secret", "token", "password", "passwd", "api_key", "apikey",
		"private_key", "privkey", "auth", "credential", "access_key",
		"aws_secret", "github_token", "slack_token", "docker_password",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// ShellExists checks if a shell binary is available.
func ShellExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
