package detector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Info holds detected environment information.
type Info struct {
	OS string
	// Shell is the user's shell: the one envset places entries for. It is
	// always zsh, bash or fish, never a script interpreter like sh.
	Shell string
	// ShellSource says how Shell was chosen (flag, $ENVSET_SHELL, process
	// tree, $SHELL, directory service, default).
	ShellSource string
	// RunningShell is the nearest shell in the process tree, which may be
	// sh/dash under cron, IDE tasks or #!/bin/sh scripts. Informational only.
	RunningShell string
	LoginShell   string // basename of $SHELL ("unknown" if unset)
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

// override is set by the global --shell flag.
var override string

// SupportedShells are the shells envset can place entries for.
var SupportedShells = []string{"zsh", "bash", "fish"}

func isSupported(s string) bool {
	for _, x := range SupportedShells {
		if s == x {
			return true
		}
	}
	return false
}

// SetShellOverride forces the user shell (from --shell). Empty clears it.
func SetShellOverride(shell string) error {
	if shell != "" && !isSupported(shell) {
		return fmt.Errorf("unsupported shell %q (want one of %s)", shell, strings.Join(SupportedShells, ", "))
	}
	override = shell
	return nil
}

// Detect gathers environment information.
func Detect() (*Info, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("could not determine home directory (is $HOME set?): %v", err)
	}

	tree := shellsInTree(os.Getppid(), procInfo)
	shell, source := resolveUserShell(override, os.Getenv("ENVSET_SHELL"), tree, os.Getenv("SHELL"), dsclShell)
	running := ""
	if len(tree) > 0 {
		running = tree[0]
	}

	return &Info{
		OS:           runtime.GOOS,
		Shell:        shell,
		ShellSource:  source,
		RunningShell: running,
		LoginShell:   detectLoginShell(),
		HomeDir:      home,
		ShellConfigs: detectShellConfigs(home),
	}, nil
}

// resolveUserShell picks the shell to place entries for, in priority order:
// --shell, $ENVSET_SHELL, the nearest zsh/bash/fish in the process tree,
// $SHELL, the directory service's UserShell, and finally zsh (the macOS
// default). sh/dash/ksh/csh are skipped wherever they appear: they mean
// "a script is running", not "this is the user's shell".
func resolveUserShell(flag, envOverride string, tree []string, shellEnv string, dscl func() string) (shell, source string) {
	if flag != "" {
		return flag, "--shell flag"
	}
	if s := filepath.Base(strings.TrimSpace(envOverride)); isSupported(s) {
		return s, "$ENVSET_SHELL"
	}
	for _, s := range tree {
		if isSupported(s) {
			return s, "process tree"
		}
	}
	if s := filepath.Base(shellEnv); isSupported(s) {
		return s, "$SHELL"
	}
	if dscl != nil {
		if s := filepath.Base(dscl()); isSupported(s) {
			return s, "directory service"
		}
	}
	return "zsh", "default"
}

// detectLoginShell determines the login shell from $SHELL env var.
func detectLoginShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "unknown"
	}
	return filepath.Base(shell)
}

var knownShells = map[string]bool{
	"zsh": true, "bash": true, "fish": true, "sh": true,
	"csh": true, "tcsh": true, "ksh": true, "dash": true,
}

// procInfo returns (ppid, command) for a pid. Replaceable in tests.
var procInfo = psProcInfo

// shellsInTree walks up from pid and returns every known shell it passes,
// nearest first.
func shellsInTree(pid int, info func(int) (int, string, error)) []string {
	var out []string
	visited := map[int]bool{}
	for i := 0; pid > 1 && !visited[pid] && i < 64; i++ {
		visited[pid] = true
		ppid, comm, err := info(pid)
		if err != nil {
			break
		}
		if name := shellName(comm); knownShells[name] {
			out = append(out, name)
		}
		pid = ppid
	}
	return out
}

// shellName normalises a ps comm value: "-zsh" (login shell) -> "zsh",
// "/bin/bash" -> "bash".
func shellName(comm string) string {
	return strings.TrimPrefix(filepath.Base(strings.TrimSpace(comm)), "-")
}

func psProcInfo(pid int) (int, string, error) {
	out, err := exec.Command(sysBin("ps", "/bin/ps"), "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", err
	}
	return parsePSLine(string(out))
}

// parsePSLine parses "  <ppid> <command…>". The command is everything after
// the first field, so paths with spaces (".../Visual Studio Code.app/...")
// survive intact.
func parsePSLine(line string) (int, string, error) {
	line = strings.TrimSpace(line)
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return 0, "", fmt.Errorf("unexpected ps output %q", line)
	}
	ppid, err := strconv.Atoi(line[:i])
	if err != nil {
		return 0, "", fmt.Errorf("unexpected ps output %q", line)
	}
	return ppid, strings.TrimSpace(line[i:]), nil
}

// sysBin prefers the fixed system path so lookups work with an empty $PATH
// (cron, launchd, env -i), falling back to a $PATH search.
func sysBin(name, abs string) string {
	if _, err := os.Stat(abs); err == nil {
		return abs
	}
	return name
}

// dsclShell asks the macOS directory service for the user's login shell,
// which is right even when $SHELL is unset (cron, launchd, env -i).
func dsclShell() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	u := os.Getenv("USER")
	if u == "" {
		if cu, err := user.Current(); err == nil {
			u = cu.Username
		}
	}
	if u == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, sysBin("dscl", "/usr/bin/dscl"), ".", "-read", "/Users/"+u, "UserShell").Output()
	if err != nil {
		return ""
	}
	// "UserShell: /bin/zsh"
	if f := strings.Fields(string(out)); len(f) == 2 {
		return f[1]
	}
	return ""
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
