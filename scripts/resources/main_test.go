package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
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
