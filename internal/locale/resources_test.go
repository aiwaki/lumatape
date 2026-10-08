package locale

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// These checks guard translator mistakes at the call sites: missing languages,
// a Russian fallback in English, untranslated app copy, and format-argument drift.
func TestProductResourcesHaveBothLanguagesAndPreserveFormatArguments(t *testing.T) {
	cyrillic := regexp.MustCompile(`[А-Яа-яЁё]`)
	directive := regexp.MustCompile(`%[-+# 0]*(?:\d+|\*)?(?:\.(?:\d+|\*))?[a-zA-Z%]`)
	resources, formats := 0, 0
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate resource audit source")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../.."))
	if _, err := os.Stat(filepath.Join(repository, "go.mod")); err != nil {
		t.Skip("source audit requires the checkout; language/runtime tests remain independent")
	}
	for _, root := range []string{filepath.Join(repository, "internal"), filepath.Join(repository, "cmd")} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			files := token.NewFileSet()
			file, err := parser.ParseFile(files, path, nil, 0)
			if err != nil {
				return err
			}
			translated := map[*ast.BasicLit]bool{}
			textArgs := func(call *ast.CallExpr) (ru, en *ast.BasicLit, ok bool) {
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Text" {
					return nil, nil, false
				}
				pkg, ok := selector.X.(*ast.Ident)
				if !ok || pkg.Name != "locale" {
					return nil, nil, false
				}
				if len(call.Args) != 2 {
					t.Errorf("%s: locale.Text requires two resources", files.Position(call.Pos()))
					return nil, nil, false
				}
				ru, rok := call.Args[0].(*ast.BasicLit)
				en, eok := call.Args[1].(*ast.BasicLit)
				if !rok || !eok || ru.Kind != token.STRING || en.Kind != token.STRING {
					t.Errorf("%s: resources must be explicit strings", files.Position(call.Pos()))
					return nil, nil, false
				}
				return ru, en, true
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if ru, en, ok := textArgs(call); ok {
					resources++
					translated[ru], translated[en] = true, true
					rus, _ := strconv.Unquote(ru.Value)
					eng, _ := strconv.Unquote(en.Value)
					if strings.TrimSpace(rus) == "" || strings.TrimSpace(eng) == "" || cyrillic.MatchString(eng) {
						t.Errorf("%s: empty or untranslated resource", files.Position(call.Pos()))
					}
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if !ok || pkg.Name != "fmt" {
					return true
				}
				index := 0
				switch selector.Sel.Name {
				case "Sprintf", "Printf", "Errorf":
				case "Fprintf":
					index = 1
				default:
					return true
				}
				if len(call.Args) <= index {
					return true
				}
				resource, ok := call.Args[index].(*ast.CallExpr)
				if !ok {
					return true
				}
				ru, en, ok := textArgs(resource)
				if !ok {
					return true
				}
				rus, _ := strconv.Unquote(ru.Value)
				eng, _ := strconv.Unquote(en.Value)
				formats++
				if !reflect.DeepEqual(directive.FindAllString(rus, -1), directive.FindAllString(eng, -1)) {
					t.Errorf("%s: translated format arguments differ", files.Position(call.Pos()))
				}
				return true
			})
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING || translated[literal] {
					return true
				}
				value, _ := strconv.Unquote(literal.Value)
				if cyrillic.MatchString(value) {
					t.Errorf("%s: app-owned Cyrillic text has no English resource", files.Position(literal.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if resources == 0 || formats == 0 {
		t.Fatal("resource audit did not find product strings")
	}
	t.Logf("checked %d bilingual resources, including %d format strings", resources, formats)
}
