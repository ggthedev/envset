package logger

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggthedev/envset/pkg/paths"
)

// Environment variables / .env keys recognised by the logger.
const (
	EnvLogFile = "ENVSET_LOG_FILE" // explicit absolute file path
	EnvLogDir  = "ENVSET_LOG_DIR"  // directory; envset.log appended

	defaultFile = "envset.log"
)

// ResolveLogPath determines where to write the audit log.
//
// Precedence (first non-empty wins):
//  1. $ENVSET_LOG_FILE
//  2. $ENVSET_LOG_DIR  (joined with envset.log)
//  3. <config>/.env  (same keys), where <config> is $XDG_CONFIG_HOME/envset
//     or ~/.config/envset
//  4. <config>/logs/envset.log  (default)
//
// It returns "" when none applies because no home directory is known.
//
// A .env in the current directory is deliberately NOT consulted: running
// envset inside some project must not redirect its log.
//
// All paths starting with "~/" or "$HOME/" are expanded to the user's home.
func ResolveLogPath() string {
	home, _ := os.UserHomeDir()

	// 1 & 2: process environment
	if p := os.Getenv(EnvLogFile); p != "" {
		return expandHome(p, home)
	}
	if d := os.Getenv(EnvLogDir); d != "" {
		return filepath.Join(expandHome(d, home), defaultFile)
	}

	// Without a config directory (no $HOME) there is nowhere to log: a
	// relative path would write into whatever directory envset runs in.
	cfg, err := paths.ConfigDir()
	if err != nil {
		return ""
	}

	// 3: envset's own .env
	if vals, err := readDotEnv(filepath.Join(cfg, ".env")); err == nil {
		if p := vals[EnvLogFile]; p != "" {
			return expandHome(p, home)
		}
		if d := vals[EnvLogDir]; d != "" {
			return filepath.Join(expandHome(d, home), defaultFile)
		}
	}

	// 4: default
	return filepath.Join(cfg, "logs", defaultFile)
}

func expandHome(p, home string) string {
	if home == "" {
		return p
	}
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return home + p[1:]
	case strings.HasPrefix(p, "$HOME/"):
		return home + p[5:]
	}
	return p
}

func readDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		// strip surrounding single/double quotes
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		out[k] = v
	}
	return out, sc.Err()
}
