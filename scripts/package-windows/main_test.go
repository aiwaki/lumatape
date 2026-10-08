package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZipReproducibleAndPortable(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "LumaTape")
	if e := os.MkdirAll(filepath.Join(root, "docs"), 0755); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "app.txt"), []byte("LumaTape"), 0644)
	os.WriteFile(filepath.Join(root, "docs", "readme.txt"), []byte("Guide"), 0644)
	a, b := filepath.Join(d, "a.zip"), filepath.Join(d, "b.zip")
	if e := writeZip(root, a); e != nil {
		t.Fatal(e)
	}
	if e := writeZip(root, b); e != nil {
		t.Fatal(e)
	}
	x, _ := os.ReadFile(a)
	y, _ := os.ReadFile(b)
	if !bytes.Equal(x, y) {
		t.Fatal("identical input produced different ZIP")
	}
	z, e := zip.OpenReader(a)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	if len(z.File) != 2 {
		t.Fatal(len(z.File))
	}
	for _, f := range z.File {
		if strings.Contains(f.Name, "\\") || !strings.HasPrefix(f.Name, "LumaTape/") || f.Modified.Year() != 1980 {
			t.Fatal(f.FileHeader)
		}
	}
}
func TestPublishRefusesUserDirectoryAndReplacesOwnedOutput(t *testing.T) {
	d := t.TempDir()
	out := filepath.Join(d, "out")
	stage := filepath.Join(d, "stage")
	os.Mkdir(out, 0755)
	os.Mkdir(stage, 0755)
	os.WriteFile(filepath.Join(out, "user.txt"), []byte("keep"), 0644)
	if publish(stage, out) == nil {
		t.Fatal("replaced arbitrary user directory")
	}
	if _, e := os.Stat(filepath.Join(out, "user.txt")); e != nil {
		t.Fatal(e)
	}
	writeJSON(filepath.Join(out, "BUILD-STATUS.json"), map[string]string{"name": "LumaTape"})
	os.WriteFile(filepath.Join(stage, "new.txt"), []byte("new"), 0644)
	if e := publish(stage, out); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(out, "new.txt")); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(out, "user.txt")); !os.IsNotExist(e) {
		t.Fatal("stale distribution entry survived")
	}
}
func TestCopyRefusesLinksAndManifestExcludesGeneratedArtifacts(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "source.go"), []byte("package sample"), 0644)
	os.Mkdir(filepath.Join(d, "artifacts"), 0755)
	os.WriteFile(filepath.Join(d, "artifacts", "private.log"), []byte("private"), 0600)
	os.WriteFile(filepath.Join(d, "resource.syso"), []byte("binary"), 0644)
	r, e := sourceRecords(d)
	if e != nil || len(r) != 1 || r[0].Name != "source.go" {
		t.Fatal(r, e)
	}
	link := filepath.Join(d, "link")
	if e := os.Symlink(filepath.Join(d, "source.go"), link); e != nil {
		t.Skip("symlink unavailable")
	}
	if copyFile(link, filepath.Join(d, "copied")) == nil {
		t.Fatal("accepted symlink")
	}
}

func TestRejectsOverlappingPackagePathsBeforeCopy(t *testing.T) {
	root := t.TempDir()
	for _, o := range []options{
		{root: root, input: filepath.Join(root, "inputs"), output: filepath.Join(root, "inputs", "out"), archive: filepath.Join(root, "out.zip")},
		{root: root, input: filepath.Join(root, "out", "inputs"), output: filepath.Join(root, "out"), archive: filepath.Join(root, "out.zip")},
		{root: root, input: filepath.Join(root, "inputs"), output: filepath.Join(root, "out"), archive: filepath.Join(root, "out", "self.zip")},
	} {
		if err := pack(o); err == nil {
			t.Fatal("accepted overlap", o)
		}
	}
}

func TestPublicDocsExcludeLocalCheckpointAndUnlistedFiles(t *testing.T) {
	root, stage := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(append([]string{}, publicDocs...), "CURRENT_STATE.md", "local-session.md") {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, "docs", name)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "docs", name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyPublicDocs(root, stage); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CURRENT_STATE.md", "local-session.md"} {
		if _, err := os.Stat(filepath.Join(stage, "docs", name)); !os.IsNotExist(err) {
			t.Fatalf("private document copied: %s: %v", name, err)
		}
	}
	for _, name := range publicDocs {
		data, err := os.ReadFile(filepath.Join(stage, "docs", name))
		if err != nil || string(data) != name {
			t.Fatalf("public document missing or altered: %s: %v", name, err)
		}
	}
	records, err := sourceRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.Name == "docs/CURRENT_STATE.md" {
			t.Fatal("private checkpoint entered source manifest")
		}
	}
	if err := os.Remove(filepath.Join(root, "docs", "CURRENT_STATE.md")); err != nil {
		t.Fatal(err)
	}
	if err := copyPublicDocs(root, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
