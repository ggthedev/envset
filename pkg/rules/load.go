package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/experimentalist/envset/pkg/fsutil"
)

// Dir returns the directory that holds rules files: ~/.config/envset/.
// Honours $XDG_CONFIG_HOME when set.
func Dir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "envset"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "envset"), nil
}

// Paths returns the absolute paths of ideals.json and custom.json.
func Paths() (idealsPath, customPath string, err error) {
	d, err := Dir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(d, "ideals.json"), filepath.Join(d, "custom.json"), nil
}

// LoadOrInit loads both rule files, creating them with sensible defaults if
// they don't exist. Returns the loaded Ideals and Custom.
//
// Behaviour:
//   - If ideals.json is missing OR has an older Version, it is overwritten with
//     BuiltinIdeals(envsetVersion). Custom rules are preserved.
//   - If custom.json is missing, an empty file is created.
func LoadOrInit(envsetVersion string) (Ideals, Custom, error) {
	dir, err := Dir()
	if err != nil {
		return Ideals{}, Custom{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Ideals{}, Custom{}, fmt.Errorf("creating %s: %w", dir, err)
	}

	idealsPath, customPath, _ := Paths()

	// --- Ideals: refresh on missing or version mismatch. ---
	builtin := BuiltinIdeals(envsetVersion)
	ideals, err := readIdeals(idealsPath)
	if err != nil || ideals.Version != CurrentVersion {
		ideals = builtin
		if err := writeJSON(idealsPath, ideals); err != nil {
			return Ideals{}, Custom{}, fmt.Errorf("writing ideals.json: %w", err)
		}
	}

	// --- Custom: create empty file on first run. ---
	custom, err := readCustom(customPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Ideals{}, Custom{}, fmt.Errorf("reading custom.json: %w", err)
		}
		custom = Custom{Version: CurrentVersion}
		if err := writeJSON(customPath, custom); err != nil {
			return Ideals{}, Custom{}, fmt.Errorf("writing custom.json: %w", err)
		}
	}

	return ideals, custom, nil
}

// SaveCustom writes the custom rules atomically to ~/.config/envset/custom.json.
// Used by `audit --fix` after the interactive session.
func SaveCustom(c Custom) error {
	_, customPath, err := Paths()
	if err != nil {
		return err
	}
	c.Version = CurrentVersion
	return writeJSON(customPath, c)
}

// FindMatching returns the first custom rule matching the given finding fingerprint.
// Hybrid keying: a rule matches when File matches AND (Subject matches OR LineHash matches).
// If a rule has only one of Subject/LineHash, only that field is consulted.
func FindMatching(c Custom, checkID, file, subject, lineHash string) *CustomRule {
	for k := range c.Rules {
		r := &c.Rules[k]
		if r.CheckID != checkID || r.Match.File != file {
			continue
		}
		if r.Match.Subject != "" && r.Match.Subject == subject {
			return r
		}
		if r.Match.LineHash != "" && r.Match.LineHash == lineHash {
			return r
		}
	}
	return nil
}

// LineHash returns a 24-hex-char fingerprint of a line's trimmed content.
// Used as the fallback identifier when no subject token is available.
func LineHash(line string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(line)))
	return hex.EncodeToString(sum[:])[:24]
}

// RefreshImpacts walks custom.Rules and updates each rule's ApproxImpact
// from the matching IdealCheck. Returns true if anything changed.
// Called from `audit --fix` so users see current impact estimates even after
// the auditor's defaults are tuned.
func RefreshImpacts(custom *Custom, ideals Ideals) bool {
	changed := false
	for i := range custom.Rules {
		check := ideals.FindCheck(custom.Rules[i].CheckID)
		if check == nil {
			continue
		}
		if custom.Rules[i].ApproxImpact != check.ApproxImpact {
			custom.Rules[i].ApproxImpact = check.ApproxImpact
			changed = true
		}
	}
	return changed
}

// PruneStale removes custom rules whose target file no longer exists on disk.
// Returns the number pruned.
func PruneStale(custom *Custom) int {
	out := custom.Rules[:0]
	pruned := 0
	for _, r := range custom.Rules {
		if r.Match.File == "" {
			out = append(out, r)
			continue
		}
		if _, err := os.Stat(r.Match.File); err == nil {
			out = append(out, r)
		} else {
			pruned++
		}
	}
	custom.Rules = out
	return pruned
}

// ---------- helpers ----------

func readIdeals(path string) (Ideals, error) {
	var i Ideals
	b, err := os.ReadFile(path)
	if err != nil {
		return i, err
	}
	if err := json.Unmarshal(b, &i); err != nil {
		return i, err
	}
	return i, nil
}

func readCustom(path string) (Custom, error) {
	var c Custom
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, b, 0o644)
}
