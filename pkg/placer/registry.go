package placer

import (
	"fmt"
	"github.com/ggthedev/envset/pkg/logger"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ggthedev/envset/pkg/fsutil"
)

// ConfigFile describes a known configuration file in the registry.
type ConfigFile struct {
	Name     string   // e.g., "zshrc", "gg_system_exports"
	Path     string   // absolute path
	Scope    Scope    // interactive, login, all_sessions — declared/intended scope
	Category Category // primary category this file accepts
	Exists   bool
	Custom   bool // user-defined, not a standard shell config
	Priority int  // lower = higher priority within same scope/category

	// SourcedFrom lists absolute paths of registered files that contain a
	// `source <this>` (or `. <this>`) directive. Empty for primary files
	// that the shell loads directly (.zshenv, .zshrc, .zprofile, etc).
	SourcedFrom []string
	// sourcedAt maps each SourcedFrom path to the line of its source directive.
	sourcedAt map[string]int

	// EffectiveScope is the broadest scope that ACTUALLY reaches this file
	// in the shell-startup graph. Differs from Scope when a custom file is
	// declared as all_sessions but is only sourced from a login-only ancestor:
	// its declared Scope says "all_sessions" but EffectiveScope is "login".
	//
	// Computed by Registry.BuildSourceChain.
	EffectiveScope Scope
}

// Registry holds all known config files for the current user.
type Registry struct {
	Home  string
	Files []ConfigFile
	Shell string
}

// NewRegistry creates a registry with standard + user custom files. After
// registration, BuildSourceChain runs so every ConfigFile carries an accurate
// EffectiveScope reflecting the actual shell-startup graph on disk.
func NewRegistry(home, shell string) *Registry {
	defer logger.Enter()()
	r := &Registry{
		Home:  home,
		Shell: shell,
	}
	r.registerDefaults()
	r.registerCustomFiles()
	r.BuildSourceChain()
	return r
}

func (r *Registry) registerDefaults() {
	defer logger.Enter()()
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
	// launchd (macOS only)
	if runtime.GOOS == "darwin" {
		r.add(ConfigFile{
			Name:     "launchd",
			Path:     filepath.Join(fsutil.ConfigDirFor(r.Home), "launchd.sh"),
			Scope:    ScopeSession,
			Category: CatGeneric,
			Priority: 1,
		})
	}
}

// registerCustomFiles adds the user's cross-shell files (~/.gg_system_*).
// They are only registered when they exist on this Mac, so the placer never
// suggests a file that nothing sources.
func (r *Registry) registerCustomFiles() {
	defer logger.Enter()()
	for _, f := range []ConfigFile{
		{Name: "gg_system_exports", Path: filepath.Join(r.Home, ".gg_system_exports"), Scope: ScopeAllSessions},
		{Name: "gg_system_interactive", Path: filepath.Join(r.Home, ".gg_system_interactive"), Scope: ScopeInteractive},
	} {
		if _, err := os.Stat(f.Path); err != nil {
			continue
		}
		f.Category, f.Custom, f.Priority = CatGeneric, true, 1
		r.add(f)
	}
}

func (r *Registry) add(f ConfigFile) {
	defer logger.Enter()()
	if _, err := os.Stat(f.Path); err == nil {
		f.Exists = true
	}
	r.Files = append(r.Files, f)
}

// FindByName returns a registered file by its short name.
func (r *Registry) FindByName(name string) (*ConfigFile, error) {
	defer logger.Enter()()
	for i := range r.Files {
		if r.Files[i].Name == name {
			return &r.Files[i], nil
		}
	}
	return nil, fmt.Errorf("no registered file named %q", name)
}

// ByScope returns files matching the given scope.
func (r *Registry) ByScope(scope Scope) []ConfigFile {
	defer logger.Enter()()
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
	defer logger.Enter()()
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = f.Name
	}
	return out
}

// AddCustomFile allows runtime registration of user custom files.
func (r *Registry) AddCustomFile(name, path string, scope Scope, category Category) {
	defer logger.Enter()()
	r.add(ConfigFile{
		Name:     name,
		Path:     path,
		Scope:    scope,
		Category: category,
		Custom:   true,
		Priority: 1,
	})
}
