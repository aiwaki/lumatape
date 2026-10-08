package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIconScalesHaveTransparentMarginAndVisibleBody(t *testing.T) {
	s, e := os.ReadFile("../../assets/lumatape.svg")
	if e != nil {
		t.Fatal(e)
	}
	shapes, e := parseSVG(s)
	if e != nil {
		t.Fatal(e)
	}
	b, e := makeICO(shapes)
	if e != nil {
		t.Fatal(e)
	}
	if binary.LittleEndian.Uint16(b[4:6]) != uint16(len(sizes)) {
		t.Fatal("missing icon sizes")
	}
	for i, size := range sizes {
		entry := b[6+i*16:]
		length := binary.LittleEndian.Uint32(entry[8:])
		offset := binary.LittleEndian.Uint32(entry[12:])
		im, e := png.Decode(bytes.NewReader(b[offset : offset+length]))
		if e != nil {
			t.Fatal(e)
		}
		if im.Bounds().Dx() != size || im.Bounds().Dy() != size {
			t.Fatal("wrong icon dimensions")
		}
		_, _, _, corner := im.At(0, 0).RGBA()
		_, _, _, body := im.At(size/2, size/2).RGBA()
		if corner != 0 || body == 0 {
			t.Fatalf("size%d invalid alpha", size)
		}
	}
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
