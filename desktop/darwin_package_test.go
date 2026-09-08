package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"
	"text/template"
)

func TestDarwinBundleMetadataEscapesXML(t *testing.T) {
	config, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		Name           string
		OutputFilename string
		Info           struct {
			ProductVersion   string
			Comments         string
			Copyright        string
			FileAssociations []map[string]string
			Protocols        []map[string]string
		}
	}
	if err := json.Unmarshal(config, &project); err != nil {
		t.Fatal(err)
	}
	want := project.Info.Comments
	project.Info.Copyright = `Copyright & <test> "quoted"`
	project.Info.FileAssociations = []map[string]string{{"Ext": "txt", "Name": "A & B <file>", "Role": "Editor", "IconName": "icon"}}
	project.Info.Protocols = []map[string]string{{"Scheme": "orca", "Role": "Viewer"}}
	for _, path := range []string{"build/darwin/Info.plist", "build/darwin/Info.dev.plist"} {
		t.Run(path, func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tmpl, err := template.New(path).Parse(string(source))
			if err != nil {
				t.Fatal(err)
			}
			var rendered bytes.Buffer
			if err := tmpl.Execute(&rendered, project); err != nil {
				t.Fatal(err)
			}
			decoder := xml.NewDecoder(bytes.NewReader(rendered.Bytes()))
			var content strings.Builder
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("invalid rendered plist: %v", err)
				}
				if text, ok := token.(xml.CharData); ok {
					content.Write(text)
				}
			}
			for _, value := range []string{want, project.Info.Copyright, "A & B <file>", project.Info.ProductVersion, "O.R.C.A.", "12.0"} {
				if !strings.Contains(content.String(), value) {
					t.Fatalf("metadata did not survive XML round trip: %q", value)
				}
			}
		})
	}
}

func TestDarwinPackagingVerifiesMountedBundle(t *testing.T) {
	source, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []string{`plutil -lint "$app/Contents/Info.plist"`, `codesign --verify --deep --strict "$app"`, `bash "$ROOT/scripts/test-desktop-dmg.sh" "$dmg" "$numver" "$arch"`} {
		if !strings.Contains(string(source), check) {
			t.Errorf("missing macOS package validation: %s", check)
		}
	}
	if strings.Contains(string(source), `"$dmg" "$dmgsrc" || true`) || !strings.Contains(string(source), `dmg="$staging/${ARTIFACT_BASE}-macos-universal.dmg"`) {
		t.Fatal("DMG creation must use fresh staging and propagate failures")
	}
}

func TestDesktopWindowTitleMatchesPlatform(t *testing.T) {
	for goos, want := range map[string]string{"windows": "O.R.C.A. for Windows", "darwin": "O.R.C.A.", "linux": "O.R.C.A."} {
		if got := desktopWindowTitle(goos); got != want {
			t.Errorf("%s title = %q, want %q", goos, got, want)
		}
	}
}
