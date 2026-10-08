package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var iconAssetPaths = []string{
	"assets/lumatape.ico",
	"desktop/src-tauri/icons/icon.ico",
	"desktop/src-tauri/icons/icon.png",
}

func copyExportedAssets(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	root := t.TempDir()
	assets := make(map[string][]byte)
	for _, name := range iconAssetPaths {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		assets[name] = data
	}
	return root, assets
}

func TestExportedAssetsValidateWithoutRewritingOrRequiringSVG(t *testing.T) {
	root, assets := copyExportedAssets(t)
	// The deprecated flag must still work without a version, SVG or resource tools.
	if err := run(root, "not-a-version", "missing-rc", "missing-cvtres", true); err != nil {
		t.Fatal(err)
	}
	files := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		files++
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, assets[filepath.ToSlash(name)]) {
			t.Errorf("asset changed or unexpected output created: %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files != len(assets) {
		t.Fatalf("validation changed the asset set: got %d files, want %d", files, len(assets))
	}
}

func TestIconValidationRejectsDamagedExports(t *testing.T) {
	data, err := os.ReadFile("../../assets/lumatape.ico")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"short-header":           func(b []byte) []byte { return b[:5] },
		"not-an-icon":            func(b []byte) []byte { b[2] = 2; return b },
		"empty-directory":        func(b []byte) []byte { binary.LittleEndian.PutUint16(b[4:6], 0); return b },
		"truncated-directory":    func(b []byte) []byte { return b[:6] },
		"invalid-dimensions":     func(b []byte) []byte { b[7] = 15; return b },
		"wrong-pixel-dimensions": func(b []byte) []byte { b[6], b[7] = 17, 17; return b },
		"invalid-color-metadata": func(b []byte) []byte { b[12] = 1; return b },
		"empty-payload":          func(b []byte) []byte { binary.LittleEndian.PutUint32(b[14:18], 0); return b },
		"payload-over-directory": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[18:22], 6); return b },
		"payload-outside-file":   func(b []byte) []byte { binary.LittleEndian.PutUint32(b[18:22], 0xfffffff0); return b },
		"corrupt-png":            func(b []byte) []byte { b[binary.LittleEndian.Uint32(b[18:22])] = 0; return b },
		"overlapping-layers":     func(b []byte) []byte { copy(b[22:38], b[6:22]); return b },
		"missing-scales":         func(b []byte) []byte { binary.LittleEndian.PutUint16(b[4:6], 1); return b },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateICO(mutate(bytes.Clone(data))); err == nil {
				t.Fatal("accepted damaged ICO export")
			}
		})
	}
}

func TestAssetValidationRejectsMissingOrMismatchedFiles(t *testing.T) {
	for _, name := range iconAssetPaths {
		t.Run("missing-"+name, func(t *testing.T) {
			root, _ := copyExportedAssets(t)
			if err := os.Remove(filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
			if _, err := validateAssets(root); err == nil {
				t.Fatal("accepted missing export")
			}
		})
	}
	t.Run("different-ico", func(t *testing.T) {
		root, assets := copyExportedAssets(t)
		name := "desktop/src-tauri/icons/icon.ico"
		data := bytes.Clone(assets[name])
		data[len(data)-1] ^= 1
		if err := os.WriteFile(filepath.Join(root, name), data, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := validateAssets(root); err == nil || !strings.Contains(err.Error(), "out of sync") {
			t.Fatal("did not report mismatched ICO exports", err)
		}
	})
	t.Run("different-png-pixels", func(t *testing.T) {
		root, assets := copyExportedAssets(t)
		name := "desktop/src-tauri/icons/icon.png"
		im, err := png.Decode(bytes.NewReader(assets[name]))
		if err != nil {
			t.Fatal(err)
		}
		// Change one pixel in a temporary fixture while keeping a valid PNG.
		changed := image.NewNRGBA(im.Bounds())
		draw.Draw(changed, changed.Bounds(), im, im.Bounds().Min, draw.Src)
		x, y := changed.Bounds().Dx()/2, changed.Bounds().Dy()/2
		c := changed.NRGBAAt(x, y)
		changed.SetNRGBA(x, y, color.NRGBA{R: c.R ^ 0xff, G: c.G, B: c.B, A: 255})
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, changed); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), encoded.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := validateAssets(root); err == nil || !strings.Contains(err.Error(), "out of sync") {
			t.Fatal("did not report mismatched PNG pixels", err)
		}
	})
	t.Run("corrupt-desktop-png", func(t *testing.T) {
		root, _ := copyExportedAssets(t)
		if err := os.WriteFile(filepath.Join(root, "desktop/src-tauri/icons/icon.png"), []byte("invalid PNG"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := validateAssets(root); err == nil {
			t.Fatal("accepted corrupt PNG export")
		}
	})
}

func TestVersionCannotInjectResourceSource(t *testing.T) {
	for _, bad := range []string{"1.2", "1.2.3\"", "65536.0.0", "1.0.0\n101 ICON whatever"} {
		if _, e := versionParts(bad); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if p, e := versionParts("1.2.3-beta.1"); e != nil || p != "1,2,3,0" {
		t.Fatal(p, e)
	}
}

// A minimal resource-only COFF object with the metadata emitted by MSVC
// cvtres. The empty resource directory is valid and needs no external files.
func compilerIDFixture(relocation bool) []byte {
	symbolOffset := uint32(76)
	if relocation {
		symbolOffset += 10
	}
	b := make([]byte, int(symbolOffset)+3*18+4)
	u16 := func(at int, v uint16) { binary.LittleEndian.PutUint16(b[at:], v) }
	u32 := func(at int, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }
	u16(0, pe.IMAGE_FILE_MACHINE_AMD64)
	u16(2, 1)
	u32(4, 1234)
	u32(8, symbolOffset)
	u32(12, 3)
	copy(b[20:], ".rsrc")
	u32(36, 16)
	u32(40, 60)
	u32(56, 0x40000040)
	if relocation {
		u32(44, 76)
		u16(52, 1)
		u16(84, 3) // IMAGE_REL_AMD64_ADDR32NB, referencing symbol zero.
	}
	for i, name := range []string{"@comp.id", "@feat.00", ".rsrc"} {
		at := int(symbolOffset) + i*18
		copy(b[at:], name)
		u16(at+12, 0xffff)
		b[at+16] = 3 // IMAGE_SYM_CLASS_STATIC
	}
	u32(int(symbolOffset)+8, 0x1001234)
	u32(int(symbolOffset)+18+8, 17)
	u16(int(symbolOffset)+36+12, 1)
	u32(len(b)-4, 4)
	return b
}

func TestResourceMetadataNormalizationPreservesResourceAndSymbolLayout(t *testing.T) {
	input := compilerIDFixture(false)
	saved := bytes.Clone(input)
	out, err := normalizeResourceCOFF(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, saved) {
		t.Fatal("normalization modified the caller's input")
	}
	expected := bytes.Clone(input)
	binary.LittleEndian.PutUint32(expected[4:8], 0)
	binary.LittleEndian.PutUint16(expected[88:90], 0xfffe)
	if !bytes.Equal(out, expected) {
		t.Fatal("changed resource data, symbol layout or feature metadata")
	}
	again, err := normalizeResourceCOFF(out)
	if err != nil || !bytes.Equal(out, again) {
		t.Fatal("normalization is not idempotent", err)
	}
}

func TestResourceMetadataRejectsReferencedOrMalformedCompilerIDs(t *testing.T) {
	for name, mutate := range map[string]func([]byte) []byte{
		"truncated":                   func(b []byte) []byte { return b[:19] },
		"wrong-machine":               func(b []byte) []byte { b[0] = 0; return b },
		"symbol-table-outside-file":   func(b []byte) []byte { binary.LittleEndian.PutUint32(b[8:12], 0xffffff00); return b },
		"unknown-absolute-metadata":   func(b []byte) []byte { copy(b[76:84], "@unknown"); return b },
		"runtime-symbol":              func(b []byte) []byte { binary.LittleEndian.PutUint16(b[88:90], 1); return b },
		"wrong-symbol-class":          func(b []byte) []byte { b[92] = 2; return b },
		"unexpected-auxiliary-symbol": func(b []byte) []byte { b[93] = 1; return b },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeResourceCOFF(mutate(compilerIDFixture(false))); err == nil {
				t.Fatal("accepted malformed resource metadata")
			}
		})
	}
	if _, err := normalizeResourceCOFF(compilerIDFixture(true)); err == nil || !strings.Contains(err.Error(), "relocation references") {
		t.Fatal("accepted referenced compiler metadata", err)
	}
}

func TestNormalizedResourceLinksWithWindowsGoLinker(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"go.mod": "module resourcecofftest\n\ngo 1.23.0\n", "main.go": "package main\nfunc main() {}\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := compilerIDFixture(false)
	object := filepath.Join(dir, "resource_windows_amd64.syso")
	if err := os.WriteFile(object, input, 0600); err != nil {
		t.Fatal(err)
	}
	build := func() ([]byte, error) {
		cmd := exec.Command("go", "build", "-o", "test.exe", ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0", "GOWORK=off", "GOTOOLCHAIN=local")
		return cmd.CombinedOutput()
	}
	if output, err := build(); err == nil || !bytes.Contains(output, []byte("sectnum < 0")) {
		t.Fatalf("expected the cvtres compiler-ID regression before normalization: %v\n%s", err, output)
	}
	out, err := normalizeResourceCOFF(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(object, out, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := build(); err != nil {
		t.Fatalf("normalized resource does not link: %v\n%s", err, output)
	}
	f, err := pe.Open(filepath.Join(dir, "test.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || header.DataDirectory[2].VirtualAddress == 0 || header.DataDirectory[2].Size < 16 {
		t.Fatal("linked Windows executable lost its resource directory")
	}
}
