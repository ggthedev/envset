package placer

import (
	"fmt"

	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/targets"
)

// Placement is a ranked suggestion for where to write an entry.
type Placement struct {
	File       ConfigFile
	Confidence float64 // 0.0–1.0
	Reason     string
	Update     bool // true if the key already exists in this file
}

// Placer analyzes an intent and produces ranked placements.
type Placer struct {
	Registry *Registry
}

// NewPlacer creates a placer from detected environment info.
func NewPlacer(info *detector.Info) *Placer {
	shell := info.Shell
	if shell == "" || shell == "unknown" {
		shell = info.LoginShell
	}
	return &Placer{
		Registry: NewRegistry(info.HomeDir, shell),
	}
}

// Analyze produces ranked placement suggestions for an intent.
func (p *Placer) Analyze(intent Intent) []Placement {
	var placements []Placement

	// 1. EXISTING KEY MATCH — highest priority
	existing := p.findExisting(intent.Key)
	for _, f := range existing {
		placements = append(placements, Placement{
			File:       f,
			Confidence: 0.98,
			Reason:     fmt.Sprintf("Key %q already exists in this file", intent.Key),
			Update:     true,
		})
	}

	// 2. SCOPE-CATEGORY HEURISTICS
	heuristic := p.matchByHeuristic(intent)
	if heuristic.Confidence > 0 {
		placements = append(placements, heuristic)
	}

	// 3. SCOPE FALLBACKS — add all files in the inferred scope
	scopeFiles := p.Registry.ByScope(intent.Scope)
	for _, f := range scopeFiles {
		// Skip if already in placements
		if p.contains(placements, f.Name) {
			continue
		}
		score := 0.5
		if f.Exists {
			score += 0.1
		}
		if f.Custom {
			score += 0.05 // slight preference for user's custom files
		}
		placements = append(placements, Placement{
			File:       f,
			Confidence: score,
			Reason:     fmt.Sprintf("Scope matches: %s", intent.Scope),
		})
	}

	// 4. ALL OTHER FILES — low confidence fallbacks
	for _, f := range p.Registry.Files {
		if p.contains(placements, f.Name) {
			continue
		}
		score := 0.2
		if f.Exists {
			score += 0.05
		}
		placements = append(placements, Placement{
			File:       f,
			Confidence: score,
			Reason:     "Fallback option",
		})
	}

	return sortPlacements(placements)
}

// Top returns the highest-confidence placement.
func (p *Placer) Top(intent Intent) *Placement {
	all := p.Analyze(intent)
	if len(all) == 0 {
		return nil
	}
	return &all[0]
}

// findExisting scans all registered files for an existing key.
func (p *Placer) findExisting(key string) []ConfigFile {
	var found []ConfigFile
	for _, f := range p.Registry.Files {
		if !f.Exists {
			continue
		}
		t, err := targets.Factory(f.Name, p.Registry.Home)
		if err != nil {
			continue
		}
		entries, err := t.List(key)
		if err != nil || len(entries) == 0 {
			continue
		}
		found = append(found, f)
	}
	return found
}

// matchByHeuristic applies rules to score a specific file.
func (p *Placer) matchByHeuristic(intent Intent) Placement {
	var best ConfigFile
	var score float64
	var reason string

	switch {
	// PATH → all_sessions, prefer existing files
	case intent.Category == CatPath:
		best = p.pickBestInScope(ScopeAllSessions)
		score = 0.85
		reason = "PATH modifications belong in all-sessions scope"

	// Secrets → all_sessions but warn; prefer custom files
	case intent.Category == CatSecret:
		best = p.pickBestInScope(ScopeAllSessions)
		score = 0.70
		reason = "Secrets should be in a dedicated or restricted file"

	// Eval → interactive scope (usually shell initialization)
	case intent.Category == CatEval:
		best = p.pickBestInScope(ScopeInteractive)
		score = 0.80
		reason = "eval commands typically belong in interactive configs"

	// App/tool env → all_sessions
	case intent.Category == CatApp:
		best = p.pickBestInScope(ScopeAllSessions)
		score = 0.80
		reason = "Application environments should be available in all sessions"

	// System → all_sessions
	case intent.Category == CatSystem:
		best = p.pickBestInScope(ScopeAllSessions)
		score = 0.80
		reason = "System settings should be available in all sessions"

	// User pref → interactive
	case intent.Category == CatUserPref:
		best = p.pickBestInScope(ScopeInteractive)
		score = 0.75
		reason = "Personal preferences typically belong in interactive configs"

	// Source → depends on what's being sourced; default to interactive
	case intent.Category == CatSource:
		best = p.pickBestInScope(ScopeInteractive)
		score = 0.70
		reason = "source commands typically belong in interactive configs"

	default:
		best = p.pickBestInScope(ScopeInteractive)
		score = 0.50
		reason = "Defaulting to interactive scope"
	}

	return Placement{
		File:       best,
		Confidence: score,
		Reason:     reason,
	}
}

// pickBestInScope selects the highest-priority, existing file in a scope.
// Falls back to highest-priority even if missing.
func (p *Placer) pickBestInScope(scope Scope) ConfigFile {
	var best *ConfigFile
	for i := range p.Registry.Files {
		f := &p.Registry.Files[i]
		if f.Scope != scope {
			continue
		}
		if best == nil || f.Priority < best.Priority {
			best = f
		}
		// Prefer existing files of same priority
		if f.Exists && !best.Exists && f.Priority == best.Priority {
			best = f
		}
	}
	if best == nil {
		return ConfigFile{}
	}
	return *best
}

func (p *Placer) contains(placements []Placement, name string) bool {
	for _, pl := range placements {
		if pl.File.Name == name {
			return true
		}
	}
	return false
}

func sortPlacements(placements []Placement) []Placement {
	// Simple bubble sort — fine for small N
	n := len(placements)
	for i := 0; i < n; i++ {
		for j := 0; j < n-i-1; j++ {
			if placements[j].Confidence < placements[j+1].Confidence {
				placements[j], placements[j+1] = placements[j+1], placements[j]
			}
		}
	}
	return placements
}
