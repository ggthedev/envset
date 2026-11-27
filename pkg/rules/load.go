package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/experimentalist/envset/pkg/logger"
	"os"
	"path/filepath"
	"strings"

	"github.com/experimentalist/envset/pkg/fsutil"
)

// Dir returns the directory that holds rules files: ~/.config/envset/.
// Honours $XDG_CONFIG_HOME when set.
func Dir() (string, error) {
	defer logger.Enter()()
	return fsutil.ConfigDir()
}

// Paths returns the absolute paths of ideals.json and custom.json.
func Paths() (idealsPath, customPath string, err error) {
	defer logger.Enter()()
	d, err := Dir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(d, "ideals.json"), filepath.Join(d, "custom.json"), nil
}

// Loaded is the result of Load: the rules to audit with, plus what (if
// anything) should be persisted when the user runs `audit --fix`.
type Loaded struct {
	Ideals Ideals
	Custom Custom
	// IdealsStale is true when ideals.json is missing or out of date; the
	// in-memory built-in set is used and written only by Persist.
	IdealsStale bool
	// CustomWarning is set when custom.json exists but cannot be parsed. The
	// audit continues without custom rules, and the file must not be
	// overwritten (CustomBroken) so the user can repair it.
	CustomWarning error
	CustomBroken  bool
}

// Load reads both rule files without writing anything, so a read-only
// `envset audit` never touches disk. Missing or stale files fall back to
// built-in defaults; a malformed custom.json becomes a warning, not an error.
// Rule paths under $HOME are normalised to ~/… so a synced config works on
// every Mac regardless of username.
func Load(envsetVersion string) (Loaded, error) {
	defer logger.Enter()()
	idealsPath, customPath, err := Paths()
	if err != nil {
		return Loaded{}, err
	}
	var out Loaded

	ideals, err := readIdeals(idealsPath)
	if builtin := BuiltinIdeals(envsetVersion); err != nil || ideals.Version != CurrentVersion || !ideals.covers(builtin) {
		ideals = builtin
		out.IdealsStale = true
	}
	out.Ideals = ideals

	custom, err := readCustom(customPath)
	switch {
	case err == nil:
		for i := range custom.Rules {
			custom.Rules[i].Match.File = PortablePath(custom.Rules[i].Match.File)
		}
	case errors.Is(err, os.ErrNotExist):
		custom = Custom{Version: CurrentVersion}
	default:
		out.CustomWarning = fmt.Errorf("%s is not valid JSON (%v); auditing without your acknowledged rules. Fix or delete the file", fsutil.Tilde(customPath), err)
		out.CustomBroken = true
		custom = Custom{Version: CurrentVersion}
	}
	out.Custom = custom
	return out, nil
}

// covers reports whether every built-in check is present, so an ideals.json
// written by an older binary is refreshed when new checks are added.
func (i Ideals) covers(builtin Ideals) bool {
	defer logger.Enter()()
	for _, c := range builtin.Checks {
		if i.FindCheck(c.ID) == nil {
			return false
		}
	}
	return true
}

// PersistIdeals writes ideals.json if Load found it missing or stale.
func PersistIdeals(l Loaded) error {
	defer logger.Enter()()
	if !l.IdealsStale {
		return nil
	}
	idealsPath, _, err := Paths()
	if err != nil {
		return err
	}
	return writeJSON(idealsPath, l.Ideals)
}

// LoadOrInit loads both rule files and creates or refreshes them on disk.
// A malformed custom.json is an error here (it is never overwritten).
func LoadOrInit(envsetVersion string) (Ideals, Custom, error) {
	defer logger.Enter()()
	l, err := Load(envsetVersion)
	if err != nil {
		return Ideals{}, Custom{}, err
	}
	if l.CustomBroken {
		return Ideals{}, Custom{}, l.CustomWarning
	}
	if err := PersistIdeals(l); err != nil {
		return Ideals{}, Custom{}, fmt.Errorf("writing ideals.json: %w", err)
	}
	_, customPath, _ := Paths()
	if _, err := os.Stat(customPath); errors.Is(err, os.ErrNotExist) {
		if err := writeJSON(customPath, l.Custom); err != nil {
			return Ideals{}, Custom{}, fmt.Errorf("writing custom.json: %w", err)
		}
	}
	return l.Ideals, l.Custom, nil
}

// PortablePath stores a path under $HOME as ~/…, so rules written on one Mac
// match the same file on another Mac with a different username.
func PortablePath(p string) string {
	defer logger.Enter()()
	if p == "" || strings.HasPrefix(p, "~") {
		return p
	}
	return fsutil.Tilde(p)
}

// ExpandPath turns a ~/… rule path back into an absolute path for this Mac.
func ExpandPath(p string) string {
	defer logger.Enter()()
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home + p[1:]
		}
	}
	return p
}

// SaveCustom writes the custom rules atomically to ~/.config/envset/custom.json.
// Used by `audit --fix` after the interactive session.
func SaveCustom(c Custom) error {
	defer logger.Enter()()
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
	defer logger.Enter()()
	for k := range c.Rules {
		r := &c.Rules[k]
		if r.CheckID != checkID || ExpandPath(r.Match.File) != ExpandPath(file) {
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
	defer logger.Enter()()
	sum := sha256.Sum256([]byte(strings.TrimSpace(line)))
	return hex.EncodeToString(sum[:])[:24]
}

// RefreshImpacts walks custom.Rules and updates each rule's ApproxImpact
// from the matching IdealCheck. Returns true if anything changed.
// Called from `audit --fix` so users see current impact estimates even after
// the auditor's defaults are tuned.
func RefreshImpacts(custom *Custom, ideals Ideals) bool {
	defer logger.Enter()()
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

// PruneStale removes custom rules whose file is under this Mac's $HOME and
// no longer exists. Rules for paths outside $HOME (e.g. written on another
// Mac with a different username, before paths were stored as ~/…) are kept,
// so syncing the config between Macs never erases the other Mac's rules.
// Returns the number pruned.
func PruneStale(custom *Custom) int {
	defer logger.Enter()()
	home, _ := os.UserHomeDir()
	out := custom.Rules[:0]
	pruned := 0
	for _, r := range custom.Rules {
		p := ExpandPath(r.Match.File)
		underHome := home != "" && strings.HasPrefix(p, home+"/")
		if p == "" || !underHome {
			out = append(out, r)
			continue
		}
		if _, err := os.Stat(p); err == nil {
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
	defer logger.Enter()()
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
	defer logger.Enter()()
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
	defer logger.Enter()()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, b, 0o644)
}
