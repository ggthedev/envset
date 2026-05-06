package logger

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Environment variables / .env keys recognised by the logger.
const (
	EnvLogFile = "ENVSET_LOG_FILE" // explicit absolute file path
	EnvLogDir  = "ENVSET_LOG_DIR"  // directory; envset.log appended

	defaultDir  = "~/.config/envset/logs"
	defaultFile = "envset.log"
)

// ResolveLogPath determines where to write the audit log.
//
// Precedence (first non-empty wins):
//  1. $ENVSET_LOG_FILE
//  2. $ENVSET_LOG_DIR  (joined with envset.log)
//  3. ~/.config/envset/.env  (same keys)
//  4. ./.env in the current working directory  (same keys)
//  5. ~/.config/envset/logs/envset.log  (default)
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

	// 3 & 4: .env files
	for _, dotenv := range []string{
		filepath.Join(home, ".config", "envset", ".env"),
		".env",
	} {
		vals, err := readDotEnv(dotenv)
		if err != nil {
			continue
		}
		if p := vals[EnvLogFile]; p != "" {
			return expandHome(p, home)
		}
		if d := vals[EnvLogDir]; d != "" {
			return filepath.Join(expandHome(d, home), defaultFile)
		}
	}

	// 5: default
	return filepath.Join(expandHome(defaultDir, home), defaultFile)
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
