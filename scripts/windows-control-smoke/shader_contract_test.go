package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"testing"
)

func TestShaderLibraryIsolationReplacesCaseInsensitiveWindowsVariableOnlyInChild(t *testing.T) {
	original := []string{"PATH=C:\\Windows", "LocalAppData=C:\\real-profile", "LOCALAPPDATA=C:\\duplicate", "OTHER=value"}
	before := append([]string(nil), original...)
	want := []string{"PATH=C:\\Windows", "OTHER=value", "LOCALAPPDATA=C:\\smoke\\Лума"}
	if got := isolatedShaderEnvironment(original, "C:\\smoke\\Лума"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if !reflect.DeepEqual(original, before) {
		t.Fatal("parent environment mutated")
	}
}

func previewFixture(t *testing.T, alpha uint8, width int) []byte {
	t.Helper()
	i := image.NewNRGBA(image.Rect(0, 0, width, 1))
	for x := 0; x < width; x++ {
		i.SetNRGBA(x, 0, color.NRGBA{12, 34, 56, alpha})
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, i); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"mime": "image/png", "width": 2, "height": 1, "png_base64": base64.StdEncoding.EncodeToString(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestShaderPreviewRequiresActualDimensionsAndOpaqueAlpha(t *testing.T) {
	pixels, _, err := shaderPreviewPixels(previewFixture(t, 255, 2), 2, 1)
	if err != nil || !bytes.Equal(pixels, []byte{12, 34, 56, 12, 34, 56}) {
		t.Fatalf("pixels=%v err=%v", pixels, err)
	}
	for _, raw := range [][]byte{previewFixture(t, 254, 2), previewFixture(t, 255, 1), []byte(`{"mime":"image/png","width":2,"height":1,"png_base64":"!"}`)} {
		if _, _, err := shaderPreviewPixels(raw, 2, 1); err == nil {
			t.Fatal("invalid preview accepted")
		}
	}
}
func TestShaderPreviewSeparatesBypassFromVisibleEffect(t *testing.T) {
	base := []byte{12, 34, 56, 78, 90, 123}
	if mae, err := shaderPreviewDifference(base, append([]byte(nil), base...), []byte{90, 20, 10, 2, 5, 7}); err != nil || mae < 1 {
		t.Fatalf("mae=%f err=%v", mae, err)
	}
	for _, pair := range [][2][]byte{{base, base}, {{13, 34, 56, 78, 90, 123}, {90, 20, 10, 2, 5, 7}}, {base, {12}}} {
		if _, err := shaderPreviewDifference(base, pair[0], pair[1]); err == nil {
			t.Fatal("invalid bypass/effect accepted")
		}
	}
}
