package main

import "testing"

func TestDevModeRequiresExplicitTrue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "uppercase true", value: " TRUE ", want: true},
		{name: "false", value: "false", want: false},
		{name: "zero", value: "0", want: false},
		{name: "empty", value: "", want: false},
		{name: "arbitrary text", value: "development", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := devModeEnabled(test.value); got != test.want {
				t.Fatalf("devModeEnabled(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestSingleInstanceLockOnlyDisabledByExplicitDevMode(t *testing.T) {
	for _, value := range []string{"", "false", "0", "development"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ORCA_DEV", value)
			if lock := singleInstanceLock(nil); lock == nil {
				t.Fatalf("ORCA_DEV=%q disabled the single-instance lock", value)
			}
		})
	}
	for _, value := range []string{"true", "1", "TRUE"} {
		t.Run("enabled-"+value, func(t *testing.T) {
			t.Setenv("ORCA_DEV", value)
			if lock := singleInstanceLock(nil); lock != nil {
				t.Fatalf("ORCA_DEV=%q did not disable the single-instance lock", value)
			}
		})
	}
}
