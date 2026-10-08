package shaderpack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `/* LumaTape
{"version":1,"name":"Test","description":"A test filter","coordinates":"preserve","parameters":[{"name":"Tint","min":0,"max":1,"step":0.01,"default":0.5}]}
*/
vec3 lumatape(vec2 uv) { return ltSample(uv) * ltParams[0]; }
`

func TestAIFileNormalization(t *testing.T) {
	p, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{fixture, "\ufeff" + fixture, strings.ReplaceAll(fixture, "\n", "\r\n"), "```glsl\n" + fixture + "```"} {
		actual, e := Parse(source)
		if e != nil || actual.ID != p.ID {
			t.Fatalf("normalization: %v %+v", e, actual)
		}
	}
	if !ValidID(p.ID) || p.Defaults()[0] != .5 || p.ValidateParams(p.Defaults()) != nil {
		t.Fatal("descriptor/default contract")
	}
}

func TestInvalidShaderDoesNotEnterLibrary(t *testing.T) {
	variants := []string{
		strings.Replace(fixture, `"version":1`, `"version":2`, 1),
		strings.Replace(fixture, `"coordinates":"preserve"`, `"coordinates":"guess"`, 1),
		strings.Replace(fixture, `"default":0.5`, `"default":2`, 1),
		strings.Replace(fixture, `"step":0.01`, `"step":0`, 1),
		strings.Replace(fixture, `"name":"Test"`, `"name":""`, 1),
		strings.Replace(fixture, `"version":1`, `"version":1,"url":"https://example.invalid"`, 1),
		fixture + "\x00", strings.Repeat(" ", MaxSourceBytes) + fixture,
		strings.Replace(fixture, "vec3 lumatape", "vec3 another", 1),
		fixture + "\n/* unfinished",
	}
	for _, token := range []string{"#version 330 core", "void main(){}", "discard;", "uniform float evil;", "while(true){}", "for(;;){}", "gl_FragDepth=0.;", "frag=vec4(0);"} {
		variants = append(variants, fixture+token)
	}
	dir := t.TempDir()
	for i, source := range variants {
		if _, e := Parse(source); e == nil {
			t.Fatalf("accepted invalid case%d", i)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("validation unexpectedly wrote files")
	}
}

func TestCommentsDoNotRejectNaturalLanguage(t *testing.T) {
	if _, err := Parse(fixture + "\n// for this effect no main is required\n/* while #include gl_FragColor */"); err != nil {
		t.Fatal(err)
	}
}

func TestParameterBounds(t *testing.T) {
	p, _ := Parse(fixture)
	for _, v := range [][8]float64{{-1}, {2}, {.5, 1}} {
		if p.ValidateParams(v) == nil {
			t.Fatalf("accepted %v", v)
		}
	}
}

func TestStorePreservesOriginalAndRejectsTampering(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Свои шейдеры")
	p, _ := Parse(fixture)
	if err := Save(dir, p); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, p); err != nil {
		t.Fatal("idempotent save", err)
	}
	p2, _ := Parse(strings.Replace(fixture, "return ltSample(uv)", "return vec3(1.0)", 1))
	if p2.ID == p.ID {
		t.Fatal("source change must create a new identity")
	}
	if err := Save(dir, p2); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, p.ID)
	if err != nil || got.Source != p.Source {
		t.Fatal("old shader changed", err)
	}
	if _, err = Load(dir, "../secret"); err == nil {
		t.Fatal("accepted a path")
	}
	if err = Remove(dir, "../secret"); err == nil {
		t.Fatal("accepted delete path")
	}
	if err = os.WriteFile(filepath.Join(dir, p2.ID+extension), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(dir, p2.ID); err == nil {
		t.Fatal("accepted changed content under an old identity")
	}
	items, err := List(dir)
	if err == nil || len(items) != 1 || items[0].ID != p.ID {
		t.Fatal("one corrupt file hid working library", items, err)
	}
}

func TestSaveFailureKeepsWorkingShader(t *testing.T) {
	dir := t.TempDir()
	p, _ := Parse(fixture)
	if err := Save(dir, p); err != nil {
		t.Fatal(err)
	}
	invalid := p
	invalid.ID = strings.Repeat("0", 64)
	if err := Save(dir, invalid); err == nil {
		t.Fatal("forged ID accepted")
	}
	blocked := filepath.Join(dir, "not-directory")
	os.WriteFile(blocked, []byte("marker"), 0600)
	if err := Save(blocked, p); err == nil {
		t.Fatal("write to non-directory succeeded")
	}
	actual, err := Load(dir, p.ID)
	if err != nil || actual.Source != p.Source {
		t.Fatal("failed save broke working source")
	}
}

func TestShippedExamples(t *testing.T) {
	paths, err := filepath.Glob("../../examples/shaders/*.lumatape.glsl")
	if err != nil || len(paths) < 2 {
		t.Fatal("examples missing", err)
	}
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		p, e := Parse(string(b))
		if e != nil {
			t.Fatalf("%s: %v", path, e)
		}
		if e = p.ValidateParams(p.Defaults()); e != nil {
			t.Fatal(e)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(fixture)
	f.Add("not a shader")
	f.Add("/* LumaTape {} */")
	f.Fuzz(func(t *testing.T, source string) {
		p, e := Parse(source)
		if e == nil {
			again, e := Parse(p.Source)
			if e != nil || again.ID != p.ID {
				t.Fatalf("successful parser result is not stable: %v", e)
			}
			if e = p.ValidateParams(p.Defaults()); e != nil {
				t.Fatal(e)
			}
		}
	})
}
