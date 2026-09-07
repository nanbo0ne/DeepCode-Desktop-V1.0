package installsource

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDesktopReleaseVersionInputs(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if runtime.GOOS == "windows" {
		candidate := `C:\Program Files\Git\bin\bash.exe`
		if _, statErr := os.Stat(candidate); statErr == nil {
			bash, err = candidate, nil
		}
	}
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, tc := range []struct {
		name, channel, tag, base, run, want string
	}{
		{"stable", "stable", "desktop-v3.0.2", "", "", "version=v3.0.2"},
		{"prerelease", "stable", "desktop-v3.0.2-rc.1", "", "", "prerelease=true"},
		{"wrong namespace", "stable", "v3.0.2", "", "", ""},
		{"missing patch", "stable", "desktop-v3.0", "", "", ""},
		{"output injection", "stable", "desktop-v3.0.2\nchannel=canary", "", "", ""},
		{"canary", "canary", "", "v3.0.3", "12", "version=v3.0.3-canary.12"},
		{"canary namespace", "canary", "", "desktop-v3.0.3", "12", "version=v3.0.3-canary.12"},
		{"invalid canary", "canary", "", "3.0.3-beta", "12", ""},
		{"invalid run", "canary", "", "3.0.3", "12\ntag=wrong", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "outputs.txt")
			cmd := exec.Command(bash, "../../scripts/resolve-desktop-release.sh")
			cmd.Env = append(os.Environ(), "EVENT_NAME=workflow_dispatch", "IN_CHANNEL="+tc.channel,
				"IN_TAG="+tc.tag, "IN_BASE_VERSION="+tc.base, "RUN_NUMBER="+tc.run,
				"GITHUB_OUTPUT="+filepath.ToSlash(output))
			log, err := cmd.CombinedOutput()
			if tc.want == "" {
				if err == nil {
					t.Fatalf("accepted invalid version input: %s", log)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve: %v: %s", err, log)
			}
			data, err := os.ReadFile(output)
			if err != nil || !strings.Contains(string(data), tc.want) {
				t.Fatalf("outputs=%q, err=%v; want %q", data, err, tc.want)
			}
		})
	}
}
