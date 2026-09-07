package builtin

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestBashWorkspaceEnvironmentsAreIndependent(t *testing.T) {
	const key = "ORCA_TEST_SCOPED_SHELL"
	before, present := os.LookupEnv(key)
	var wg sync.WaitGroup
	for _, value := range []string{"project-a", "project-b"} {
		reg := tool.NewRegistry()
		reg.Add(bash{})
		env := setEnvValue(os.Environ(), key, value)
		BindBashEnvironment(reg, env)
		for i := range env {
			env[i] = "MUTATED=1"
		}
		got, _ := reg.Get("bash")
		wg.Add(1)
		go func(b bash, want string) {
			defer wg.Done()
			actual, ok := envValue(bashEnvironment(context.Background(), b.environment), key)
			if !ok || actual != want {
				t.Errorf("environment = %q, want %q", actual, want)
			}
		}(got.(bash), value)
	}
	wg.Wait()
	after, stillPresent := os.LookupEnv(key)
	if after != before || present != stillPresent {
		t.Fatal("host environment mutated")
	}
}
