package placer

import (
	"strings"

	"github.com/experimentalist/envset/pkg/targets"
)

// Scope defines when the variable should be active.
type Scope string

const (
	ScopeInteractive Scope = "interactive"  // .zshrc, ~/.gg_system_interactive
	ScopeLogin       Scope = "login"        // .zprofile
	ScopeAllSessions Scope = "all_sessions" // .zshenv, ~/.gg_system_exports
	ScopeSession     Scope = "session"      // launchd, systemd-env (session-wide / GUI)
	ScopeUnknown     Scope = "unknown"
)

// Category classifies the kind of variable being set.
type Category string

const (
	CatPath     Category = "path"        // PATH modifications
	CatSecret   Category = "secret"      // API keys, tokens
	CatEval     Category = "eval"        // eval $(command)
	CatSource   Category = "source"      // source /path
	CatApp      Category = "application" // Language/tool specific: JAVA_HOME, GOPATH, etc.
	CatSystem   Category = "system"      // System-level config
	CatUserPref Category = "user_pref"   // Personal preference: EDITOR, PAGER, PS1
	CatGeneric  Category = "generic"
)

// Intent captures what the user is trying to add.
type Intent struct {
	Key      string
	Value    string
	VarType  targets.VarType
	Scope    Scope
	Category Category
}

// DetectIntent infers category and tentative scope from the variable.
func DetectIntent(key, value string, varType targets.VarType) Intent {
	intent := Intent{
		Key:     key,
		Value:   value,
		VarType: varType,
		Scope:   ScopeUnknown,
	}

	switch varType {
	case targets.VarTypePathAppend, targets.VarTypePathPrepend:
		intent.Category = CatPath
		intent.Scope = ScopeAllSessions
	case targets.VarTypeEval:
		intent.Category = CatEval
		intent.Scope = ScopeInteractive // eval is usually interactive
	case targets.VarTypeSource:
		intent.Category = CatSource
		intent.Scope = ScopeInteractive // default, may be refined
	case targets.VarTypeFishSet:
		intent.Category = CatGeneric
		intent.Scope = ScopeInteractive
	case targets.VarTypeExport:
		intent = classifyExport(key, value, intent)
	}

	if isSecret(key) {
		intent.Category = CatSecret
	}

	return intent
}

func classifyExport(key, value string, intent Intent) Intent {
	lower := lowerKey(key)

	// PATH variables
	if lower == "path" || contains(lower, "_path") {
		intent.Category = CatPath
		intent.Scope = ScopeAllSessions
		return intent
	}

	// Language/tool environments
	appKeys := []string{"java_home", "gopath", "goroot", "rustup_home", "cargo_home",
		"node_home", "nvm_dir", "rbenv_root", "pyenv_root", "conda_home",
		"flutter_home", "android_home", "sdk_root"}
	for _, k := range appKeys {
		if contains(lower, k) {
			intent.Category = CatApp
			intent.Scope = ScopeAllSessions
			return intent
		}
	}

	// AWS / cloud provider
	cloudKeys := []string{"aws_", "azure_", "gcp_", "google_", "do_", "digitalocean_"}
	for _, k := range cloudKeys {
		if startsWith(lower, k) {
			intent.Category = CatApp
			intent.Scope = ScopeAllSessions
			return intent
		}
	}

	// Personal preferences
	prefKeys := []string{"editor", "pager", "visual", "ps1", "ps2", "prompt",
		"less", "manpager", "browser", "term"}
	for _, k := range prefKeys {
		if lower == k || contains(lower, "_"+k) || startsWith(lower, k+"_") {
			intent.Category = CatUserPref
			intent.Scope = ScopeInteractive
			return intent
		}
	}

	// System-level
	sysKeys := []string{"locale", "lang", "lc_", "tz", "timezone",
		"display", "xauthority", "ssh_", "gnupghome"}
	for _, k := range sysKeys {
		if lower == k || startsWith(lower, k) {
			intent.Category = CatSystem
			intent.Scope = ScopeAllSessions
			return intent
		}
	}

	// XDG / config dirs
	if contains(lower, "xdg_") || contains(lower, "config_") {
		intent.Category = CatSystem
		intent.Scope = ScopeAllSessions
		return intent
	}

	intent.Category = CatGeneric
	return intent
}

func lowerKey(s string) string { return strings.ToLower(s) }

func contains(s, substr string) bool   { return strings.Contains(s, substr) }
func startsWith(s, prefix string) bool { return strings.HasPrefix(s, prefix) }

// isSecret checks if a key looks like a secret/API key.
func isSecret(key string) bool {
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
