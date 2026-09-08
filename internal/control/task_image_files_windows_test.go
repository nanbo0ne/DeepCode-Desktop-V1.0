package control

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTaskImagesWindowsJunctionEscapesAndReadApprovalSwap(t *testing.T) {
	for _, swap := range []bool{false, true} {
		t.Run(map[bool]string{false: "junction path", true: "swap during read approval"}[swap], func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			putTaskImage(t, outside, "private.png", taskImagePNG(t, 20))
			link := filepath.Join(root, "linked")
			create := func() {
				// Directory junction creation does not require symlink privileges.
				if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
					t.Fatalf("create test junction: %v (%s)", err, output)
				}
				t.Cleanup(func() { _ = os.Remove(link) })
			}
			gate := allowTaskImages()
			if swap {
				putTaskImage(t, root, "linked/private.png", taskImagePNG(t, 1))
				gate = taskImageGateFunc(func(_ context.Context, name string, _ json.RawMessage, _ bool) (bool, string, error) {
					if name == "read_file" {
						if err := os.Rename(link, link+".old"); err != nil {
							t.Fatal(err)
						}
						create()
					}
					return true, "", nil
				})
			} else {
				create()
			}
			if _, _, err := resolveTaskImages(context.Background(), root, []string{"linked/private.png"}, "selected/vision", nil, gate, 8, maxVisionImageBytesPerTurn); err == nil {
				t.Fatal("junction escape accepted")
			}
			if _, err := openTaskImageRoot(link); err == nil {
				t.Fatal("junction workspace accepted")
			}
		})
	}
}
