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
	Shell        string // current running shell (from process tree)
	LoginShell   string // login shell (from $SHELL)
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

	loginShell := detectLoginShell()
	currentShell := detectCurrentShell()

	// Prefer current shell for suggestions; fallback to login shell.
	shell := currentShell
	if shell == "" || shell == "unknown" {
		shell = loginShell
	}

	configs := detectShellConfigs(home)

	return &Info{
		OS:           runtime.GOOS,
		Shell:        shell,
		LoginShell:   loginShell,
		HomeDir:      home,
		ShellConfigs: configs,
	}, nil
}

// detectLoginShell determines the login shell from $SHELL env var.
func detectLoginShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "unknown"
	}
	return filepath.Base(shell)
}

// detectCurrentShell walks the parent process tree to find the actual
// shell process you're currently running in (e.g., bash launched from zsh).
func detectCurrentShell() string {
	knownShells := map[string]bool{
		"zsh": true, "bash": true, "fish": true, "sh": true,
		"csh": true, "tcsh": true, "ksh": true, "dash": true,
	}

	pid := os.Getppid()
	visited := make(map[int]bool)

	for pid > 1 && !visited[pid] {
		visited[pid] = true
		comm, ppid, err := getProcessInfo(pid)
		if err != nil {
			break
		}
		name := filepath.Base(strings.TrimSpace(comm))
		if knownShells[name] {
			return name
		}
		// Skip common intermediaries (e.g., go run, script, sshd, tmux, screen)
		skip := map[string]bool{"go": true, "script": true, "sshd": true,
			"tmux": true, "screen": true, "login": true, "sudo": true,
			"su": true, "launchd": true, "init": true}
		if !skip[name] && name != "" {
			// If we hit a non-shell, non-intermediary process, stop traversing
			// unless it's a terminal emulator (keep going).
			termEmulators := map[string]bool{
				" Terminal": true, "iTerm2": true, "kitty": true,
				"alacritty": true, "warp": true, "wezterm": true,
			}
			if !termEmulators[name] {
				// Could be a command run from shell; continue one more level
			}
		}
		pid = ppid
	}

	return ""
}

// getProcessInfo returns the command name and parent PID for a given PID.
// Uses ps which is available on both macOS and Linux.
func getProcessInfo(pid int) (string, int, error) {
	cmd := exec.Command("ps", "-o", "comm=", "-o", "ppid=", "-p", fmt.Sprintf("%d", pid))
	out, err := cmd.Output()
	if err != nil {
		return "", 0, err
	}
	parts := strings.Fields(string(out))
	if len(parts) < 2 {
		return "", 0, fmt.Errorf("unexpected ps output")
	}
	comm := parts[0]
	ppid := 0
	fmt.Sscanf(parts[1], "%d", &ppid)
	return comm, ppid, nil
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
		{Name: "config_fish", Path: filepath.Join(home, ".config", "fish", "config.fish"), Shell: "fish", Priority: 1},
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
