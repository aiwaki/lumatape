// package-windows builds a clean, allowlisted Windows distribution and a
// deterministic ZIP. It never infers runtime qualification from compilation.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type options struct {
	root, input, licenses, output, archive, version, commit, buildTime string
	lightweight                                                        bool
}
type fileRecord struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func main() {
	var o options
	flag.StringVar(&o.root, "root", ".", "source repository root")
	flag.StringVar(&o.input, "input", "build/windows-full/stage", "directory containing freshly built EXEs, DLLs and licenses")
	flag.StringVar(&o.licenses, "licenses", "", "explicit license directory; defaults to input/licenses")
	flag.StringVar(&o.output, "output", "dist/lumatape-windows-x64", "generated distribution directory")
	flag.StringVar(&o.archive, "zip", "dist/lumatape-windows-x64-unqualified.zip", "output ZIP")
	flag.StringVar(&o.version, "version", "0.1.0", "product version")
	flag.StringVar(&o.commit, "commit", "unknown", "source revision, never inferred from binary dates")
	flag.StringVar(&o.buildTime, "build-time", "unknown", "build time from the caller")
	flag.BoolVar(&o.lightweight, "lightweight", false, "omit capture and Full demonstration")
	flag.Parse()
	if err := pack(o); err != nil {
		fmt.Fprintln(os.Stderr, "package:", err)
		os.Exit(1)
	}
}
func pack(o options) error {
	var err error
	for _, p := range []*string{&o.root, &o.input, &o.output, &o.archive} {
		*p, err = filepath.Abs(*p)
		if err != nil {
			return err
		}
	}
	if within(o.root, o.output) || within(o.input, o.output) || within(o.output, o.input) {
		return errors.New("output must not replace source or overlap binary input")
	}
	if within(o.archive, o.output) {
		return errors.New("ZIP must be outside its input distribution directory")
	}
	if o.licenses == "" {
		o.licenses = filepath.Join(o.input, "licenses")
	}
	if err = os.MkdirAll(filepath.Dir(o.output), 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(o.output), ".lumatape-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	bin := []string{"lumatape.exe", "lumatape-watchdog.exe", "lumatape-testcard.exe", "glfw3.dll"}
	if !o.lightweight {
		bin = append(bin, "lumatape_capture.dll", "lumatape_capture_abi_smoke.exe")
	}
	var provenance []fileRecord
	for _, name := range bin {
		path := filepath.Join(o.input, name)
		f, e := pe.Open(path)
		if e != nil {
			return fmt.Errorf("%s is not a valid PE: %w", name, e)
		}
		machine := f.Machine
		f.Close()
		if machine != pe.IMAGE_FILE_MACHINE_AMD64 {
			return fmt.Errorf("%s is not Windows AMD64", name)
		}
		if err = copyFile(path, filepath.Join(stage, name)); err != nil {
			return err
		}
		r, e := record(filepath.Join(stage, name), name)
		if e != nil {
			return e
		}
		provenance = append(provenance, r)
	}
	for _, name := range []string{"README.md", "README.en.md", "START-HERE.txt", "START-HERE.en.txt", "LICENSE", "assets/lumatape.svg", "assets/lumatape.ico", "third_party/README.md", "third_party/README.en.md", "third_party/Go-LICENSE.txt", "third_party/cppwinrt-LICENSE.txt", "third_party/MSVC-STL-LICENSE.txt"} {
		if err = copyFile(filepath.Join(o.root, filepath.FromSlash(name)), filepath.Join(stage, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	for _, name := range []string{"LICENSE", "GLFW-LICENSE.md", "Go-LICENSE.txt", "cppwinrt-LICENSE.txt", "MSVC-STL-LICENSE.txt"} {
		if err = copyFile(filepath.Join(o.licenses, name), filepath.Join(stage, "licenses", name)); err != nil {
			return err
		}
	}
	if err = copyPublicDocs(o.root, stage); err != nil {
		return err
	}
	if !o.lightweight {
		if err = copyFile(filepath.Join(o.root, "Try-VHS.cmd"), filepath.Join(stage, "Try-VHS.cmd")); err != nil {
			return err
		}
		if err = copyByExtension(filepath.Join(o.root, "examples"), filepath.Join(stage, "examples"), ".json"); err != nil {
			return err
		}
	}
	status := map[string]any{"name": "LumaTape", "version": o.version, "commit": o.commit, "build_time": o.buildTime, "target": "windows/amd64", "minimum_windows_build": 19041, "capture_included": !o.lightweight, "release_qualification": "UNQUALIFIED", "runtime_qualification": "Not inferred from packaging; see docs/WINDOWS_VALIDATION.md", "packager": "scripts/package-windows"}
	status["source_snapshot_scope"] = "SOURCE-FILES.json describes packaging-time sources, not proof of binary/source correspondence; BINARY-PROVENANCE.json records exact input bytes"
	if err = writeJSON(filepath.Join(stage, "BUILD-STATUS.json"), status); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(stage, "BINARY-PROVENANCE.json"), provenance); err != nil {
		return err
	}
	sources, err := sourceRecords(o.root, stage, o.output, o.archive)
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(stage, "SOURCE-FILES.json"), sources); err != nil {
		return err
	}
	files, err := listFiles(stage)
	if err != nil {
		return err
	}
	var hashes strings.Builder
	for _, name := range files {
		r, e := record(filepath.Join(stage, filepath.FromSlash(name)), name)
		if e != nil {
			return e
		}
		fmt.Fprintf(&hashes, "%s  %s\n", r.SHA256, name)
	}
	if err = os.WriteFile(filepath.Join(stage, "SHA256SUMS.txt"), []byte(hashes.String()), 0644); err != nil {
		return err
	}
	if err = publish(stage, o.output); err != nil {
		return err
	}
	if err = writeZip(o.output, o.archive); err != nil {
		return err
	}
	fmt.Printf("Packaged %s\nArchive %s\n", o.output, o.archive)
	return nil
}
func copyFile(from, to string) error {
	st, e := os.Lstat(from)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular input %s", from)
	}
	if e = os.MkdirAll(filepath.Dir(to), 0755); e != nil {
		return e
	}
	in, e := os.Open(from)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	return errors.Join(e, out.Close())
}

var publicDocs = []string{"README.md", "README.en.md", "shaders/README.md", "shaders/README.en.md", "WINDOWS_VALIDATION.md", "PARALLELS_SMOKE.md", "ARCHITECTURE.md", "PRIOR_ART_AUDIT.md", "SHADER_SPEC.md", "UPDATES.md"}

func copyPublicDocs(root, stage string) error {
	for _, name := range publicDocs {
		if err := copyFile(filepath.Join(root, "docs", name), filepath.Join(stage, "docs", name)); err != nil {
			return err
		}
	}
	return nil
}

func copyByExtension(from, to, ext string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(d.Name()) != ext {
			return nil
		}
		rel, e := filepath.Rel(from, path)
		if e != nil {
			return e
		}
		return copyFile(path, filepath.Join(to, rel))
	})
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func record(path, name string) (fileRecord, error) {
	f, e := os.Open(path)
	if e != nil {
		return fileRecord{}, e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, f)
	return fileRecord{Name: name, SHA256: hex.EncodeToString(h.Sum(nil)), Size: n}, e
}
func listFiles(root string) ([]string, error) {
	var out []string
	e := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular distribution entry %s", p)
		}
		r, e := filepath.Rel(root, p)
		if e == nil {
			out = append(out, filepath.ToSlash(r))
		}
		return e
	})
	sort.Strings(out)
	return out, e
}

// within includes equality and respects path-component boundaries.
func within(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sourceRecords(root string, excluded ...string) ([]fileRecord, error) {
	var out []fileRecord
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		for _, skip := range excluded {
			if within(p, skip) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || d.Name() == "build" || d.Name() == "dist" || d.Name() == "artifacts" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if ext == ".syso" || ext == ".exe" || ext == ".dll" || d.Name() == ".DS_Store" || filepath.ToSlash(rel) == "docs/CURRENT_STATE.md" {
			return nil
		}
		r, e := record(p, filepath.ToSlash(rel))
		if e == nil {
			out = append(out, r)
		}
		return e
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, err
}
func publish(stage, out string) error {
	backup := out + ".previous"
	if _, e := os.Stat(out); e == nil {
		b, e := os.ReadFile(filepath.Join(out, "BUILD-STATUS.json"))
		if e != nil {
			return errors.New("refusing to replace a directory without LumaTape BUILD-STATUS.json")
		}
		var status struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(b, &status) != nil || status.Name != "LumaTape" {
			return errors.New("refusing to replace an unrecognized distribution")
		}
		if _, e := os.Stat(backup); !errors.Is(e, os.ErrNotExist) {
			return errors.New("previous distribution backup already exists; inspect it before retrying")
		}
		if e = os.Rename(out, backup); e != nil {
			return e
		}
		if e = os.Rename(stage, out); e != nil {
			_ = os.Rename(backup, out)
			return e
		}
		return os.RemoveAll(backup)
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return os.Rename(stage, out)
}
func writeZip(root, path string) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".lumatape-*.zip")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	z := zip.NewWriter(f)
	files, e := listFiles(root)
	if e != nil {
		f.Close()
		return e
	}
	for _, name := range files {
		h := &zip.FileHeader{Name: filepath.Base(root) + "/" + name, Method: zip.Deflate}
		h.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		h.SetMode(0644)
		w, err := z.CreateHeader(h)
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
		in, err := os.Open(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
		_, err = io.Copy(w, in)
		in.Close()
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
	}
	if e = z.Close(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	backup := path + ".previous"
	if _, e = os.Stat(path); e == nil {
		if _, e = os.Stat(backup); !errors.Is(e, os.ErrNotExist) {
			return errors.New("previous ZIP backup exists")
		}
		if e = os.Rename(path, backup); e != nil {
			return e
		}
		if e = os.Rename(tmp, path); e != nil {
			_ = os.Rename(backup, path)
			return e
		}
		return os.Remove(backup)
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return os.Rename(tmp, path)
}
