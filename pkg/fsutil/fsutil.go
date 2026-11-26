// Package fsutil provides crash-safe file writes and timestamped backups for
// every file envset mutates (shell rc files, launchd script, audit fixes, rules).
package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ConfigDir returns envset's config directory:
// $XDG_CONFIG_HOME/envset if set, otherwise ~/.config/envset.
func ConfigDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "envset"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "envset"), nil
}

// ConfigDirFor is ConfigDir for an explicit home directory (used where the
// caller already resolved $HOME, e.g. targets built for a test HOME).
func ConfigDirFor(home string) string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "envset")
	}
	return filepath.Join(home, ".config", "envset")
}

// ResolvePath follows symlinks so writes land on the real file (e.g. a
// dotfiles-repo file that ~/.zshrc links to) and the link itself survives.
// A path that does not exist yet is returned unchanged; a dangling symlink
// resolves to its (not yet existing) target.
func ResolvePath(path string) (string, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, nil
	}
	// Dangling link: resolve one hop manually.
	dest, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(path), dest)
	}
	return dest, nil
}

// WriteFileAtomic replaces path's contents with data such that a crash,
// kill or full disk never leaves a truncated file: data goes to a temp file in
// the same directory, is fsynced, then renamed over the target. Symlinks are
// followed (the link is preserved) and the existing file mode is kept;
// newMode applies only when the file does not exist yet.
func WriteFileAtomic(path string, data []byte, newMode os.FileMode) error {
	real, err := ResolvePath(path)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}
	mode := newMode
	if fi, err := os.Stat(real); err == nil {
		mode = fi.Mode().Perm()
	}

	dir := filepath.Dir(real)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(real)+".envset-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("syncing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, real); err != nil {
		return fmt.Errorf("replacing %s: %w", real, err)
	}
	ok = true
	return nil
}

// ---------- backups ----------

// MaxBackupsPerFile is how many backups are kept per file; older ones are pruned.
const MaxBackupsPerFile = 10

const (
	stampLayout      = "20060102T150405.000000000"
	preRestoreSuffix = ".pre-restore"
)

// BackupDir is where backups are stored: $ENVSET_BACKUP_DIR if set,
// otherwise <ConfigDir>/backups.
func BackupDir() (string, error) {
	if d := os.Getenv("ENVSET_BACKUP_DIR"); d != "" {
		return d, nil
	}
	cd, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "backups"), nil
}

// backupKey turns a path into a flat file-name prefix: files under $HOME
// become "home__<rel>" (e.g. home__.zshrc), others the full path.
func backupKey(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return "home__" + strings.ReplaceAll(rel, "/", "__"), nil
		}
	}
	return strings.ReplaceAll(strings.TrimPrefix(abs, "/"), "/", "__"), nil
}

// Tilde shortens a path under $HOME to ~/... for display.
func Tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// Backup copies path byte-for-byte into the backup directory (mode 0600) and
// returns the backup's path. A missing file is not an error: it returns "".
func Backup(path string) (string, error) {
	return backup(path, "")
}

func backup(path, suffix string) (string, error) {
	real, err := ResolvePath(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(real)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s for backup: %w", real, err)
	}
	dir, err := BackupDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating backup dir %s: %w", dir, err)
	}
	key, err := backupKey(path)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, key+"."+time.Now().Format(stampLayout)+suffix)
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return "", fmt.Errorf("writing backup %s: %w", dest, err)
	}
	pruneBackups(dir, key)
	return dest, nil
}

// BackupInfo describes one stored backup.
type BackupInfo struct {
	Path       string
	Time       time.Time
	PreRestore bool // taken automatically just before a restore
}

// ListBackups returns backups of path, newest first.
func ListBackups(path string) ([]BackupInfo, error) {
	dir, err := BackupDir()
	if err != nil {
		return nil, err
	}
	key, err := backupKey(path)
	if err != nil {
		return nil, err
	}
	return listBackups(dir, key)
}

func listBackups(dir, key string) ([]BackupInfo, error) {
	matches, err := filepath.Glob(filepath.Join(dir, globEscape(key)+".*"))
	if err != nil {
		return nil, err
	}
	var out []BackupInfo
	for _, m := range matches {
		rest := strings.TrimPrefix(filepath.Base(m), key+".")
		pre := strings.HasSuffix(rest, preRestoreSuffix)
		rest = strings.TrimSuffix(rest, preRestoreSuffix)
		t, err := time.ParseInLocation(stampLayout, rest, time.Local)
		if err != nil {
			continue // not one of ours (e.g. a key that is a prefix of another)
		}
		out = append(out, BackupInfo{Path: m, Time: t, PreRestore: pre})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

func pruneBackups(dir, key string) {
	all, err := listBackups(dir, key)
	if err != nil {
		return
	}
	for i := MaxBackupsPerFile; i < len(all); i++ {
		os.Remove(all[i].Path)
	}
}

// Restore replaces path with the given backup (or, if backupPath is empty, the
// newest backup that was not itself taken by a restore). The current contents
// are first saved as a ".pre-restore" backup so a restore can be undone.
// Returns the backup that was restored.
func Restore(path, backupPath string) (string, error) {
	if backupPath == "" {
		list, err := ListBackups(path)
		if err != nil {
			return "", err
		}
		for _, b := range list {
			if !b.PreRestore {
				backupPath = b.Path
				break
			}
		}
		if backupPath == "" {
			return "", fmt.Errorf("no backups found for %s", path)
		}
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return "", fmt.Errorf("reading backup: %w", err)
	}
	if _, err := backup(path, preRestoreSuffix); err != nil {
		return "", fmt.Errorf("saving current state before restore: %w", err)
	}
	if err := WriteFileAtomic(path, data, 0o644); err != nil {
		return "", err
	}
	return backupPath, nil
}

func globEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`)
	return r.Replace(s)
}
