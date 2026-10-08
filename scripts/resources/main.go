// resources renders the repository's small SVG subset into a multiscale ICO,
// then uses SDK/LLVM resource tools to embed icon101 and version metadata into
// all three Windows EXEs. No image/network/UI dependency is needed.
package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var sizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

type shape struct {
	kind          string
	x, y, w, h, r float64
	c             color.NRGBA
}

func main() {
	root := flag.String("root", ".", "repository root")
	version := flag.String("version", "0.3.0", "semantic product version")
	rc := flag.String("rc", "", "path to rc.exe or llvm-rc")
	cvtres := flag.String("cvtres", "", "path to cvtres.exe or llvm-cvtres")
	assetsOnly := flag.Bool("assets-only", false, "write vector-derived ICO/PNG assets without compiling EXE resources")
	flag.Parse()
	if err := run(*root, *version, *rc, *cvtres, *assetsOnly); err != nil {
		fmt.Fprintln(os.Stderr, "resources:", err)
		os.Exit(1)
	}
}

func run(root, version, rc, cvtres string, assetsOnly bool) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	svg, err := os.ReadFile(filepath.Join(root, "assets", "lumatape.svg"))
	if err != nil {
		return err
	}
	shapes, err := parseSVG(svg)
	if err != nil {
		return err
	}
	ico, err := makeICO(shapes)
	if err != nil {
		return err
	}
	iconPath := filepath.Join(root, "assets", "lumatape.ico")
	if err = os.WriteFile(iconPath, ico, 0644); err != nil {
		return err
	}
	// The Go EXEs, Tauri shell and previews share this SVG renderer. Do not
	// maintain an independently edited raster icon for the desktop shell.
	desktopIcons := filepath.Join(root, "desktop", "src-tauri", "icons")
	if err = os.MkdirAll(desktopIcons, 0755); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(desktopIcons, "icon.ico"), ico, 0644); err != nil {
		return err
	}
	if err = writePNG(filepath.Join(desktopIcons, "icon.png"), raster(shapes, 256)); err != nil {
		return err
	}
	previewDir := filepath.Join(root, "build", "resources")
	if err = os.MkdirAll(previewDir, 0755); err != nil {
		return err
	}
	for _, size := range []int{16, 24, 64} {
		if err = writePNG(filepath.Join(previewDir, fmt.Sprintf("icon-%d.png", size)), raster(shapes, size)); err != nil {
			return err
		}
	}
	if assetsOnly {
		return nil
	}
	parts, err := versionParts(version)
	if err != nil {
		return err
	}
	if rc == "" {
		rc = findTool("rc")
	}
	if cvtres == "" {
		cvtres = findTool("cvtres")
	}
	if rc == "" || cvtres == "" {
		return errors.New("resource compiler unavailable: pass -rc and -cvtres (Windows SDK/MSVC or LLVM tools)")
	}
	fmt.Printf("resources: rc=%s; cvtres=%s\n", rc, cvtres)
	dir := filepath.Join(root, "build", "resources")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, p := range []struct{ name, description string }{{"lumatape", "LumaTape CRT and VHS"}, {"lumatape-watchdog", "LumaTape display recovery"}, {"lumatape-testcard", "LumaTape test card"}} {
		source := fmt.Sprintf("101 ICON %q\n1 VERSIONINFO\n FILEVERSION %s\n PRODUCTVERSION %s\n FILEFLAGSMASK 0x3fL\n FILEFLAGS 0\n FILEOS 0x40004L\n FILETYPE 1\n FILESUBTYPE 0\nBEGIN\n BLOCK \"StringFileInfo\"\n BEGIN\n  BLOCK \"040904B0\"\n  BEGIN\n   VALUE \"CompanyName\", \"aiwaki\\0\"\n   VALUE \"FileDescription\", \"%s\\0\"\n   VALUE \"FileVersion\", \"%s\\0\"\n   VALUE \"InternalName\", \"%s\\0\"\n   VALUE \"OriginalFilename\", \"%s.exe\\0\"\n   VALUE \"ProductName\", \"LumaTape\\0\"\n   VALUE \"ProductVersion\", \"%s\\0\"\n   VALUE \"LegalCopyright\", \"Copyright (c) 2026 aiwaki\\0\"\n  END\n END\n BLOCK \"VarFileInfo\"\n BEGIN\n  VALUE \"Translation\", 0x0409, 1200\n END\nEND\n", filepath.ToSlash(iconPath), parts, parts, p.description, version, p.name, p.name, version)
		rcPath := filepath.Join(dir, p.name+".rc")
		resPath := filepath.Join(dir, p.name+".res")
		if err = os.WriteFile(rcPath, []byte(source), 0644); err != nil {
			return err
		}
		if err = commandIn(dir, rc, "/FO", filepath.Base(resPath), filepath.Base(rcPath)); err != nil {
			return err
		}
		out := filepath.Join(root, "cmd", p.name, "resource_windows_amd64.syso")
		object := p.name + ".syso"
		if err = commandIn(dir, cvtres, "/NOLOGO", "/MACHINE:X64", "/OUT:"+object, filepath.Base(resPath)); err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(dir, object))
		if err != nil {
			return err
		}
		b, err = normalizeResourceCOFF(b)
		if err != nil {
			return fmt.Errorf("%s resource object: %w", p.name, err)
		}
		if err = os.WriteFile(out, b, 0644); err != nil {
			return err
		}
	}
	return nil
}

func normalizeResourceCOFF(data []byte) ([]byte, error) {
	if len(data) < 20 || binary.LittleEndian.Uint16(data[:2]) != pe.IMAGE_FILE_MACHINE_AMD64 || binary.LittleEndian.Uint16(data[16:18]) != 0 {
		return nil, errors.New("resource converter did not produce AMD64 COFF")
	}
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid resource COFF: %w", err)
	}
	defer f.Close()
	out := bytes.Clone(data)
	// cvtres embeds its wall-clock timestamp and, unlike llvm-cvtres, may
	// emit the absolute compiler-ID symbol @comp.id. Go's PE linker accepts
	// @feat.00 but rejects @comp.id with "sectnum < 0". Mark only this unused
	// compiler metadata as IMAGE_SYM_DEBUG, which Go already ignores. Keep
	// symbol indices, relocations, feature flags and all resource bytes intact.
	for index := 0; index < len(f.COFFSymbols); {
		symbol := f.COFFSymbols[index]
		next := index + 1 + int(symbol.NumberOfAuxSymbols)
		if next > len(f.COFFSymbols) {
			return nil, errors.New("resource COFF has truncated auxiliary symbols")
		}
		name, err := symbol.FullName(f.StringTable)
		if err != nil {
			return nil, fmt.Errorf("invalid resource COFF symbol: %w", err)
		}
		if name == "@comp.id" {
			if (symbol.SectionNumber != -1 && symbol.SectionNumber != -2) || symbol.StorageClass != 3 || symbol.Type != 0 || symbol.NumberOfAuxSymbols != 0 {
				return nil, errors.New("unexpected @comp.id resource metadata")
			}
			for _, section := range f.Sections {
				for _, relocation := range section.Relocs {
					if relocation.SymbolTableIndex == uint32(index) {
						return nil, errors.New("resource relocation references compiler metadata @comp.id")
					}
				}
			}
			offset := uint64(f.PointerToSymbolTable) + uint64(index)*18 + 12
			if offset+2 > uint64(len(out)) {
				return nil, errors.New("resource COFF symbol offset is outside the file")
			}
			binary.LittleEndian.PutUint16(out[offset:offset+2], 0xfffe) // IMAGE_SYM_DEBUG
		} else if symbol.SectionNumber == -1 && name != "@feat.00" {
			return nil, fmt.Errorf("unsupported absolute resource symbol %q", name)
		}
		index = next
	}
	binary.LittleEndian.PutUint32(out[4:8], 0)
	return out, nil
}

func writePNG(path string, im image.Image) error {
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0644)
}

func commandIn(dir, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	b, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, b)
	}
	if len(b) > 0 {
		fmt.Print(string(b))
	}
	return nil
}
func findTool(name string) string {
	for _, n := range []string{"llvm-" + name, name + ".exe", name, "/opt/homebrew/opt/llvm/bin/llvm-" + name} {
		if p, e := exec.LookPath(n); e == nil {
			return p
		}
	}
	pf := os.Getenv("ProgramFiles(x86)")
	if name == "rc" {
		files, _ := filepath.Glob(filepath.Join(pf, "Windows Kits", "10", "bin", "*", "x64", "rc.exe"))
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
		if len(files) > 0 {
			return files[0]
		}
	}
	if name == "cvtres" {
		vswhere := filepath.Join(pf, "Microsoft Visual Studio", "Installer", "vswhere.exe")
		b, e := exec.Command(vswhere, "-latest", "-products", "*", "-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "-property", "installationPath").Output()
		if e == nil {
			files, _ := filepath.Glob(filepath.Join(strings.TrimSpace(string(b)), "VC", "Tools", "MSVC", "*", "bin", "Hostx64", "x64", "cvtres.exe"))
			sort.Sort(sort.Reverse(sort.StringSlice(files)))
			if len(files) > 0 {
				return files[0]
			}
		}
	}
	return ""
}

var versionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:[-+][A-Za-z0-9.-]+)?$`)

func versionParts(v string) (string, error) {
	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return "", errors.New("version must be numeric major.minor.patch, with an optional safe prerelease suffix")
	}
	for _, p := range m[1:] {
		if _, e := strconv.ParseUint(p, 10, 16); e != nil {
			return "", errors.New("version component exceeds 65535")
		}
	}
	return strings.Join(m[1:], ",") + ",0", nil
}
func parseSVG(b []byte) ([]shape, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	var out []shape
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		s, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if s.Name.Local != "rect" && s.Name.Local != "circle" {
			continue
		}
		a := map[string]string{}
		for _, v := range s.Attr {
			a[v.Name.Local] = v.Value
		}
		n := func(k string) float64 { v, _ := strconv.ParseFloat(a[k], 64); return v }
		c := strings.TrimPrefix(a["fill"], "#")
		v, e := strconv.ParseUint(c, 16, 24)
		if e != nil || len(c) != 6 {
			return nil, errors.New("SVG fills must be six-digit RGB")
		}
		p := shape{kind: s.Name.Local, x: n("x"), y: n("y"), w: n("width"), h: n("height"), r: n("rx"), c: color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}}
		if p.kind == "circle" {
			p.x, p.y, p.r = n("cx"), n("cy"), n("r")
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("SVG has no supported vector shapes")
	}
	return out, nil
}
func (p shape) contains(x, y float64) bool {
	if p.kind == "circle" {
		return math.Pow(x-p.x, 2)+math.Pow(y-p.y, 2) <= p.r*p.r
	}
	if x < p.x || y < p.y || x >= p.x+p.w || y >= p.y+p.h {
		return false
	}
	if p.r <= 0 {
		return true
	}
	cx := math.Max(p.x+p.r, math.Min(x, p.x+p.w-p.r))
	cy := math.Max(p.y+p.r, math.Min(y, p.y+p.h-p.r))
	return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= p.r*p.r
}
func raster(shapes []shape, size int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, size, size))
	const samples = 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a uint32
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					px := (float64(x) + (float64(sx)+.5)/samples) * 64 / float64(size)
					py := (float64(y) + (float64(sy)+.5)/samples) * 64 / float64(size)
					var c color.NRGBA
					for _, s := range shapes {
						if s.contains(px, py) {
							c = s.c
						}
					}
					r += uint32(c.R) * uint32(c.A)
					g += uint32(c.G) * uint32(c.A)
					b += uint32(c.B) * uint32(c.A)
					a += uint32(c.A)
				}
			}
			if a > 0 {
				im.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / (samples * samples))})
			}
		}
	}
	return im
}
func makeICO(shapes []shape) ([]byte, error) {
	images := make([][]byte, len(sizes))
	for i, s := range sizes {
		var b bytes.Buffer
		if e := png.Encode(&b, raster(shapes, s)); e != nil {
			return nil, e
		}
		images[i] = b.Bytes()
	}
	b := new(bytes.Buffer)
	binary.Write(b, binary.LittleEndian, uint16(0))
	binary.Write(b, binary.LittleEndian, uint16(1))
	binary.Write(b, binary.LittleEndian, uint16(len(sizes)))
	offset := uint32(6 + 16*len(sizes))
	for i, s := range sizes {
		b.Write([]byte{byte(s % 256), byte(s % 256), 0, 0})
		binary.Write(b, binary.LittleEndian, uint16(1))
		binary.Write(b, binary.LittleEndian, uint16(32))
		binary.Write(b, binary.LittleEndian, uint32(len(images[i])))
		binary.Write(b, binary.LittleEndian, offset)
		offset += uint32(len(images[i]))
	}
	for _, im := range images {
		b.Write(im)
	}
	return b.Bytes(), nil
}
