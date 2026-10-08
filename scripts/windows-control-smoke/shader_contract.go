package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"strings"
)

func isolatedShaderEnvironment(current []string, localAppData string) []string {
	result := make([]string, 0, len(current)+1)
	for _, entry := range current {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "LOCALAPPDATA") {
			result = append(result, entry)
		}
	}
	return append(result, "LOCALAPPDATA="+localAppData)
}

// Decode pixels, not PNG byte streams: compression is not an effect oracle.
func shaderPreviewPixels(raw []byte, width, height int) (pixels, encoded []byte, err error) {
	var result struct {
		MIME   string `json:"mime"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		PNG    string `json:"png_base64"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return
	}
	if result.MIME != "image/png" || result.Width != width || result.Height != height {
		err = errors.New("preview MIME or declared dimensions differ")
		return
	}
	encoded, err = base64.StdEncoding.DecodeString(result.PNG)
	if err != nil {
		return
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		return nil, nil, err
	}
	if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		return nil, nil, errors.New("decoded preview dimensions differ")
	}
	pixels = make([]byte, 0, width*height*3)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := decoded.At(x+decoded.Bounds().Min.X, y+decoded.Bounds().Min.Y).RGBA()
			if a != 65535 {
				return nil, nil, fmt.Errorf("preview alpha is not opaque at %d,%d", x, y)
			}
			pixels = append(pixels, byte(r>>8), byte(g>>8), byte(b>>8))
		}
	}
	return
}

func shaderPreviewDifference(original, zero, full []byte) (float64, error) {
	if len(original) == 0 || len(original) != len(zero) || len(original) != len(full) {
		return 0, errors.New("preview pixel lengths differ or are empty")
	}
	if !bytes.Equal(original, zero) {
		return 0, errors.New("0% shader is not exact original-pixel bypass")
	}
	var difference uint64
	for i, v := range original {
		d := int(v) - int(full[i])
		if d < 0 {
			d = -d
		}
		difference += uint64(d)
	}
	mae := float64(difference) / float64(len(original))
	if mae < 1 {
		return mae, fmt.Errorf("100%% custom shader difference too small: MAE %.4f/255", mae)
	}
	return mae, nil
}
