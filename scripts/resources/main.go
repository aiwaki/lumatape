// resources validates the checked-in icon exports, then uses SDK/LLVM
// resource tools to embed icon101 and version metadata into all three Windows EXEs.
// Artwork is exported separately; this command never rewrites icon assets.
package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	root := flag.String("root", ".", "repository root")
	version := flag.String("version", "0.3.0", "semantic product version")
	rc := flag.String("rc", "", "path to rc.exe or llvm-rc")
	cvtres := flag.String("cvtres", "", "path to cvtres.exe or llvm-cvtres")
	assetsOnly := flag.Bool("assets-only", false, "deprecated: validate checked-in icon assets without compiling EXE resources")
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
	iconPath, err := validateAssets(root)
	if err != nil {
		return err
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

// validateAssets keeps the Go executables and Tauri shell on the same export.
func validateAssets(root string) (string, error) {
	iconPath := filepath.Join(root, "assets", "lumatape.ico")
	ico, err := os.ReadFile(iconPath)
	if err != nil {
		return "", err
	}
	layers, err := validateICO(ico)
	if err != nil {
		return "", fmt.Errorf("%s: %w", iconPath, err)
	}
	desktopIcons := filepath.Join(root, "desktop", "src-tauri", "icons")
	desktopICO := filepath.Join(desktopIcons, "icon.ico")
	other, err := os.ReadFile(desktopICO)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(ico, other) {
		return "", fmt.Errorf("icon exports are out of sync: %s and %s must be identical", iconPath, desktopICO)
	}
	pngPath := filepath.Join(desktopIcons, "icon.png")
	data, err := os.ReadFile(pngPath)
	if err != nil {
		return "", err
	}
	preview, err := validatePNG(data, 256, 256)
	if err != nil {
		return "", fmt.Errorf("%s: %w", pngPath, err)
	}
	if !samePixels(preview, layers[256]) {
		return "", fmt.Errorf("icon exports are out of sync: %s must match the 256x256 ICO layer", pngPath)
	}
	return iconPath, nil
}

func validateICO(data []byte) (map[int]image.Image, error) {
	if len(data) < 6 || binary.LittleEndian.Uint16(data[:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, errors.New("invalid ICO header")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	tableEnd := 6 + 16*count
	if count == 0 || count > 256 || tableEnd > len(data) {
		return nil, errors.New("invalid or truncated ICO directory")
	}
	type interval struct{ start, end uint64 }
	ranges := make([]interval, 0, count)
	layers := make(map[int]image.Image)
	for i := 0; i < count; i++ {
		entry := data[6+16*i : 6+16*(i+1)]
		w, h := int(entry[0]), int(entry[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		planes, bits := binary.LittleEndian.Uint16(entry[4:6]), binary.LittleEndian.Uint16(entry[6:8])
		if w != h || w < 16 || entry[2] != 0 || entry[3] != 0 || planes > 1 || (bits != 0 && bits != 32) {
			return nil, fmt.Errorf("ICO layer %d has invalid dimensions or color metadata", i)
		}
		length := uint64(binary.LittleEndian.Uint32(entry[8:12]))
		start := uint64(binary.LittleEndian.Uint32(entry[12:16]))
		end := start + length
		if length == 0 || start < uint64(tableEnd) || end > uint64(len(data)) {
			return nil, fmt.Errorf("ICO layer %d is outside the image payload", i)
		}
		for _, prior := range ranges {
			if start < prior.end && prior.start < end {
				return nil, fmt.Errorf("ICO layer %d overlaps another layer", i)
			}
		}
		ranges = append(ranges, interval{start, end})
		im, err := validatePNG(data[start:end], 16, 256)
		if err != nil {
			return nil, fmt.Errorf("ICO layer %d must contain a valid PNG export: %w", i, err)
		}
		if im.Bounds().Dx() != w || im.Bounds().Dy() != h {
			return nil, fmt.Errorf("ICO layer %d dimensions differ from its directory entry", i)
		}
		if layers[w] != nil {
			return nil, fmt.Errorf("ICO export contains duplicate %dx%d layers", w, w)
		}
		layers[w] = im
	}
	for _, size := range []int{16, 32, 256} {
		if layers[size] == nil {
			return nil, fmt.Errorf("ICO export is missing the %dx%d layer", size, size)
		}
	}
	return layers, nil
}

func validatePNG(data []byte, minSize, maxSize int) (image.Image, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width != config.Height || config.Width < minSize || config.Width > maxSize {
		return nil, fmt.Errorf("expected a square PNG between %d and %d pixels", minSize, maxSize)
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var transparent, visible bool
	for y := 0; y < config.Height; y++ {
		for x := 0; x < config.Width; x++ {
			_, _, _, alpha := im.At(x, y).RGBA()
			transparent = transparent || alpha == 0
			visible = visible || alpha != 0
			if transparent && visible {
				return im, nil
			}
		}
	}
	return nil, errors.New("icon must contain both transparent and visible pixels")
}

func samePixels(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				return false
			}
		}
	}
	return true
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
