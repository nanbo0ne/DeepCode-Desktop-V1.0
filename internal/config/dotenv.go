package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv resolves dotenv sources into an isolated environment map. It does
// not mutate the process environment, so loading another workspace cannot
// inherit values from this one.
// Order: a project ./.env (read-only back-compat, so a manual project override
// takes precedence), then the deepseek-orca-owned global credentials file in the user
// config dir (where `deepseek-orca setup` writes keys, so they resolve from any
// directory without ever touching a project's own .env), then ~/.env as a legacy
// fallback (the desktop app writes there). Existing environment variables always
// win over all three.
func loadDotEnv() map[string]string {
	return loadDotEnvForRoot(".")
}

// loadDotEnvForRoot loads a root's .env file (if present) before the home .env
// fallback. When root is "." it behaves like loadDotEnv().
func loadDotEnvForRoot(root string) map[string]string {
	values := make(map[string]string)
	dotEnvPath := ".env"
	if root != "" && root != "." {
		dotEnvPath = filepath.Join(root, ".env")
	}
	loadDotEnvFile(dotEnvPath, values)
	if p := UserCredentialsPath(); p != "" {
		loadDotEnvFile(p, values)
	}
	if p := LegacyUserCredentialsPath(); p != "" {
		loadDotEnvFile(p, values)
	}
	if home, err := os.UserHomeDir(); err == nil {
		loadDotEnvFile(filepath.Join(home, ".env"), values)
	}
	return values
}

// loadDotEnvFile reads one .env file (if present) into values. Host variables
// are deliberately left untouched. Lenient, zero-dependency parsing.
func loadDotEnvFile(path string, values map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key == "" {
			continue
		}
		// A host variable, including an explicitly empty one, always wins.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if _, exists := values[key]; !exists {
			values[key] = val
		}
	}
}
