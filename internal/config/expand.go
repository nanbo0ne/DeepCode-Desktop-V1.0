package config

import (
	"os"
	"regexp"
	"strings"
)

// varRef matches ${VAR} and ${VAR:-default}: a shell-style reference with an
// optional ":-default" fallback used when the variable is unset or empty.
var varRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// ExpandVars substitutes ${VAR} / ${VAR:-default} references from the process
// environment. An unset variable with no default expands to "" (matching the
// MCP / Claude Code convention), so a missing secret yields an empty header
// rather than a literal "${TOKEN}" leaking onto the wire.
func ExpandVars(s string) string {
	return expandVars(s, os.LookupEnv)
}

// ExpandVars expands references using this config's host-priority, workspace-
// scoped environment.
func (c *Config) ExpandVars(s string) string {
	if c == nil {
		return ExpandVars(s)
	}
	return expandVars(s, c.Env)
}

func expandVars(s string, lookup func(string) (string, bool)) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return varRef.ReplaceAllStringFunc(s, func(m string) string {
		g := varRef.FindStringSubmatch(m)
		name, hasDefault, def := g[1], g[2] != "", g[3]
		if v, ok := lookup(name); ok && v != "" {
			return v
		}
		if hasDefault {
			return def
		}
		return ""
	})
}

// ExpandedPlugin returns a copy of e with ${VAR} references expanded across the
// command, args, env values, url, and header values — the fields Claude Code
// also expands. The entry itself is left untouched.
func (e PluginEntry) ExpandedPlugin() PluginEntry {
	if e.env == nil {
		return expandPlugin(e, ExpandVars)
	}
	return expandPlugin(e, func(s string) string {
		return expandVars(s, func(key string) (string, bool) {
			return lookupScopedEnv(e.env, key)
		})
	})
}

// ExpandPlugin returns a copy of e with this config's environment applied to
// command, args, env, URL, and headers. It is the scoped counterpart to
// PluginEntry.ExpandedPlugin for MCP startup and subprocess wiring.
func (c *Config) ExpandPlugin(e PluginEntry) PluginEntry {
	if c == nil {
		return e.ExpandedPlugin()
	}
	e.env = c.env
	return e.ExpandedPlugin()
}

func expandPlugin(e PluginEntry, expand func(string) string) PluginEntry {
	out := e
	out.Command = expand(e.Command)
	out.URL = expand(e.URL)
	if len(e.Args) > 0 {
		out.Args = make([]string, len(e.Args))
		for i, a := range e.Args {
			out.Args[i] = expand(a)
		}
	}
	out.Env = expandMap(e.Env, expand)
	out.Headers = expandMap(e.Headers, expand)
	return out
}

func expandMap(m map[string]string, expand func(string) string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = expand(v)
	}
	return out
}
