package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListPreviewDomains(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "factories"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"web":   "preview_domains = [\n  \"*.example.test\",\n  \"example.test\",\n]\n",
		"empty": "preview_domains = []\n",
		"old":   "runtime = \"one-shot\"\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "factories", name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	instances := factoryInstances(root)
	if len(instances) != 3 || strings.Join(instances[2].PreviewDomains, ",") != "*.example.test,example.test" {
		t.Fatalf("preview glob list did not parse: %+v", instances)
	}
	capture, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	original := os.Stdout
	os.Stdout = capture
	defer func() { os.Stdout = original }()
	if err := runList(root, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(capture)
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if !strings.Contains(output, `web preview_domains = ["*.example.test" "example.test"]`) ||
		!strings.Contains(output, "empty preview_domains = []") ||
		strings.Contains(output, "old preview_domains") {
		t.Fatalf("list lost configured/empty/absent distinction:\n%s", output)
	}
}
