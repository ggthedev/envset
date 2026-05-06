package placer

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigFile describes a known configuration file in the registry.
type ConfigFile struct {
	Name     string   // e.g., "zshrc", "gg_system_exports"
	Path     string   // absolute path
	Scope    Scope    // interactive, login, all_sessions
	Category Category // primary category this file accepts
	Exists   bool
	Custom   bool // user-defined, not a standard shell config
	Priority int  // lower = higher priority within same scope/category
}

// Registry holds all known config files for the current user.
type Registry struct {
	Home   string
	Files  []ConfigFile
	Shell  string
}

// NewRegistry creates a registry with standard + user custom files.
func NewRegistry(home, shell string) *Registry {
	r := &Registry{
		Home:  home,
		Shell: shell,
	}
	r.registerDefaults()
	r.registerCustomFiles()
	return r
}

func (r *Registry) registerDefaults() {
	// Standard zsh files
	if r.Shell == "zsh" || r.Shell == "" {
		r.add(ConfigFile{Name: "zshenv", Path: filepath.Join(r.Home, ".zshenv"), Scope: ScopeAllSessions, Category: CatGeneric, Priority: 1})
		r.add(ConfigFile{Name: "zprofile", Path: filepath.Join(r.Home, ".zprofile"), Scope: ScopeLogin, Category: CatGeneric, Priority: 1})
		r.add(ConfigFile{Name: "zshrc", Path: filepath.Join(r.Home, ".zshrc"), Scope: ScopeInteractive, Category: CatGeneric, Priority: 1})
	}
	// Standard bash files
	if r.Shell == "bash" || r.Shell == "" {
		r.add(ConfigFile{Name: "bash_profile", Path: filepath.Join(r.Home, ".bash_profile"), Scope: ScopeLogin, Category: CatGeneric, Priority: 1})
		r.add(ConfigFile{Name: "bashrc", Path: filepath.Join(r.Home, ".bashrc"), Scope: ScopeInteractive, Category: CatGeneric, Priority: 1})
		r.add(ConfigFile{Name: "profile", Path: filepath.Join(r.Home, ".profile"), Scope: ScopeLogin, Category: CatGeneric, Priority: 2})
	}
	// fish
	if r.Shell == "fish" || r.Shell == "" {
		r.add(ConfigFile{Name: "config_fish", Path: filepath.Join(r.Home, ".config", "fish", "config.fish"), Scope: ScopeInteractive, Category: CatGeneric, Priority: 1})
	}
}

func (r *Registry) registerCustomFiles() {
	// User's cross-shell common files
	r.add(ConfigFile{
		Name:     "gg_system_exports",
		Path:     filepath.Join(r.Home, ".gg_system_exports"),
		Scope:    ScopeAllSessions,
		Category: CatGeneric,
		Custom:   true,
		Priority: 1,
	})
	r.add(ConfigFile{
		Name:     "gg_system_interactive",
		Path:     filepath.Join(r.Home, ".gg_system_interactive"),
		Scope:    ScopeInteractive,
		Category: CatGeneric,
		Custom:   true,
		Priority: 1,
	})
}

func (r *Registry) add(f ConfigFile) {
	if _, err := os.Stat(f.Path); err == nil {
		f.Exists = true
	}
	r.Files = append(r.Files, f)
}

// FindByName returns a registered file by its short name.
func (r *Registry) FindByName(name string) (*ConfigFile, error) {
	for i := range r.Files {
		if r.Files[i].Name == name {
			return &r.Files[i], nil
		}
	}
	return nil, fmt.Errorf("no registered file named %q", name)
}

// ByScope returns files matching the given scope.
func (r *Registry) ByScope(scope Scope) []ConfigFile {
	var out []ConfigFile
	for _, f := range r.Files {
		if f.Scope == scope {
			out = append(out, f)
		}
	}
	return out
}

// AllNames returns all registered file names.
func (r *Registry) AllNames() []string {
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = f.Name
	}
	return out
}

// AddCustomFile allows runtime registration of user custom files.
func (r *Registry) AddCustomFile(name, path string, scope Scope, category Category) {
	r.add(ConfigFile{
		Name:     name,
		Path:     path,
		Scope:    scope,
		Category: category,
		Custom:   true,
		Priority: 1,
	})
}
