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

	// EffectiveScope mirrors File.EffectiveScope at analysis time. The placer
	// uses it to compare against the intent's requested scope.
	EffectiveScope Scope

	// ScopeMismatch is true when the file's *declared* Scope matches the
	// intent but its *effective* scope (per the source-chain) does not. The
	// wizard surfaces this with a clarifying question so the user can pick
	// between the broken-but-familiar file and the primary one for the scope.
	ScopeMismatch bool

	// DuplicateLocations lists OTHER files (paths) where the same key is
	// already defined. Empty when the key is unique. Populated for every
	// candidate so the wizard / CLI can warn about dead duplicates regardless
	// of which file is finally picked.
	DuplicateLocations []string
}

// Placer analyzes an intent and produces ranked placements.
type Placer struct {
	Registry *Registry
}

// NewPlacer creates a placer from detected environment info. Files are
// registered for the user's shell (info.Shell), never the running one, so a
// #!/bin/sh script or cron job still places into .zshenv/.bash_profile.
func NewPlacer(info *detector.Info) *Placer {
	return &Placer{
		Registry: NewRegistry(info.HomeDir, info.Shell),
	}
}

// Analyze produces ranked placement suggestions for an intent.
//
// Ranking signals (highest to lowest weight):
//  1. Key already exists in a file — prefer updating in place.
//  2. Heuristic match for the intent's category (PATH, secret, eval, etc).
//  3. Files whose declared Scope matches intent.Scope.
//  4. Every other file as a low-confidence fallback.
//
// After ranking, two cross-cutting penalties are applied to every candidate:
//   - SCOPE-MISMATCH penalty (−0.20): file's EffectiveScope is narrower than
//     the intent demands (e.g. ~/.gg_system_exports declared all_sessions but
//     only sourced from .zprofile).
//   - DUPLICATE awareness: every candidate that finds the key in OTHER files
//     gets DuplicateLocations populated so the UI can warn even when the
//     winning placement is an update of an existing entry.
func (p *Placer) Analyze(intent Intent) []Placement {
	var placements []Placement

	// 1. EXISTING KEY MATCH — highest priority
	existing := p.findExisting(intent.Key)
	existingPaths := make([]string, 0, len(existing))
	for _, f := range existing {
		existingPaths = append(existingPaths, f.Path)
	}
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

	// 5. CROSS-CUTTING ADJUSTMENTS — effective-scope penalty + duplicates.
	for i := range placements {
		annotateForScopeAndDuplicates(&placements[i], intent, existingPaths)
	}

	return sortPlacements(placements)
}

// annotateForScopeAndDuplicates fills in EffectiveScope, ScopeMismatch, and
// DuplicateLocations on a placement and applies a confidence penalty when the
// file's effective scope does not satisfy the intent's required scope.
func annotateForScopeAndDuplicates(pl *Placement, intent Intent, existingPaths []string) {
	pl.EffectiveScope = pl.File.EffectiveScope

	// Duplicates: every existing location that ISN'T this file.
	for _, p := range existingPaths {
		if p != pl.File.Path {
			pl.DuplicateLocations = append(pl.DuplicateLocations, p)
		}
	}

	// Scope mismatch only matters when the intent has a defined scope and the
	// declared file scope satisfies it but the effective scope does not. Files
	// whose declared scope is itself irrelevant to the intent are already
	// scored low by the main ranking and don't need an extra penalty.
	if intent.Scope == ScopeUnknown {
		return
	}
	declaredOK := scopeBroaderOrEqual(pl.File.Scope, intent.Scope)
	effectiveOK := scopeBroaderOrEqual(pl.File.EffectiveScope, intent.Scope)
	if declaredOK && !effectiveOK {
		pl.ScopeMismatch = true
		pl.Confidence -= 0.20
		if pl.Confidence < 0 {
			pl.Confidence = 0
		}
		pl.Reason = fmt.Sprintf("%s; but only effective for %s shells (source-chain broken)",
			pl.Reason, pl.File.EffectiveScope)
	}
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
