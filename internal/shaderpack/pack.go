// Package shaderpack defines the versioned, single-pass LumaTape shader format.
// Validation protects the host contract; it is not a GPU driver sandbox.
package shaderpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aiwaki/lumatape/internal/locale"
)

const MaxSourceBytes = 64 << 10
const MaxParameters = 8
const MaxPacks = 128

type Parameter struct {
	Name    string  `json:"name"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Step    float64 `json:"step"`
	Default float64 `json:"default"`
}

type Descriptor struct {
	ID          string      `json:"id"`
	Version     int         `json:"version"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Coordinates string      `json:"coordinates"`
	Parameters  []Parameter `json:"parameters"`
}

type Pack struct {
	Descriptor
	Source string
	Body   string
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var entryPattern = regexp.MustCompile(`\bvec3\s+lumatape\s*\(\s*vec2\s+[A-Za-z_][A-Za-z_0-9]*\s*\)\s*\{`)
var tokens = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*|[#"']`)

func ValidID(id string) bool { return idPattern.MatchString(id) }

func Parse(source string) (Pack, error) {
	var p Pack
	if len(source) > MaxSourceBytes || !utf8.ValidString(source) || strings.ContainsRune(source, 0) {
		return p, errors.New(locale.Text("шейдер должен быть UTF-8 без NUL, размером до 64 KiB", "shader must be UTF-8 without NUL, up to 64 KiB"))
	}
	source = strings.TrimSpace(strings.TrimPrefix(source, "\ufeff"))
	// AI responses commonly wrap a complete file in one fenced block.
	if strings.HasPrefix(source, "```") {
		line := strings.IndexByte(source, '\n')
		if line < 0 || !strings.HasSuffix(source, "```") {
			return p, errors.New(locale.Text("вставьте один полный блок кода шейдера", "paste one complete shader code block"))
		}
		language := strings.TrimSpace(source[3:line])
		if language != "" && language != "glsl" {
			return p, errors.New(locale.Text("ожидается блок GLSL", "a GLSL block is required"))
		}
		source = strings.TrimSpace(source[line+1 : len(source)-3])
	}
	source = strings.ReplaceAll(source, "\r\n", "\n")
	const prefix = "/* LumaTape"
	if !strings.HasPrefix(source, prefix) {
		return p, errors.New(locale.Text("нет заголовка /* LumaTape с JSON версии 1; скопируйте шаблон запроса", "missing /* LumaTape header with version 1 JSON; use the README template"))
	}
	end := strings.Index(source, "*/")
	if end < len(prefix) || end > 8192 {
		return p, errors.New(locale.Text("заголовок LumaTape не завершён или длиннее 8 KiB", "LumaTape header is incomplete or exceeds 8 KiB"))
	}
	decoder := json.NewDecoder(strings.NewReader(source[len(prefix):end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p.Descriptor); err != nil {
		return p, fmt.Errorf(locale.Text("заголовок шейдера: %w", "shader header: %w"), err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return p, errors.New(locale.Text("в заголовке должен быть один JSON-объект", "the header must contain one JSON object"))
	}
	if p.ID != "" {
		return p, errors.New(locale.Text("id создаётся приложением; уберите его из заголовка", "id is created by the application; remove it from the header"))
	}
	if p.Version != 1 {
		return p, errors.New(locale.Text("поддерживается формат шейдера LumaTape версии 1", "LumaTape shader format version 1 is supported"))
	}
	if !plainText(p.Name, 80) || len(strings.TrimSpace(p.Name)) == 0 || !plainText(p.Description, 512) {
		return p, errors.New(locale.Text("задайте непустое имя до 80 символов и описание до 512 символов без управляющих знаков", "provide a non-empty name up to 80 characters and description up to 512 characters without control characters"))
	}
	if p.Coordinates != "preserve" && p.Coordinates != "warp" {
		return p, errors.New(locale.Text("coordinates должен быть preserve или warp", "coordinates must be preserve or warp"))
	}
	if len(p.Parameters) > MaxParameters {
		return p, errors.New(locale.Text("допустимо не более 8 параметров", "at most 8 parameters are allowed"))
	}
	if p.Parameters == nil {
		p.Parameters = []Parameter{}
	}
	names := map[string]bool{}
	for _, param := range p.Parameters {
		if !plainText(param.Name, 48) || strings.TrimSpace(param.Name) == "" || names[param.Name] {
			return p, errors.New(locale.Text("параметрам нужны уникальные непустые имена до 48 символов", "parameters require unique non-empty names up to 48 characters"))
		}
		names[param.Name] = true
		if !finite(param.Min) || !finite(param.Max) || !finite(param.Step) || !finite(param.Default) || math.Abs(param.Min) > 1e6 || math.Abs(param.Max) > 1e6 || param.Min >= param.Max || param.Default < param.Min || param.Default > param.Max || param.Step <= 0 || param.Step > param.Max-param.Min {
			return p, fmt.Errorf(locale.Text("неверный диапазон, шаг или значение параметра %q", "invalid range, step or value for parameter %q"), param.Name)
		}
	}
	p.Body = strings.TrimSpace(source[end+2:])
	body, err := withoutComments(p.Body)
	if err != nil {
		return p, err
	}
	if !entryPattern.MatchString(body) {
		return p, errors.New(locale.Text("добавьте функцию vec3 lumatape(vec2 uv)", "add the function vec3 lumatape(vec2 uv)"))
	}
	for _, token := range tokens.FindAllString(body, -1) {
		switch token {
		case "#", "\"", "'", "main", "discard", "uniform", "layout", "in", "out", "inout", "buffer", "shared", "attribute", "varying", "for", "while", "do":
			return p, fmt.Errorf(locale.Text("%q не входит в формат LumaTape v1; используйте функции и развёрнутые выборки из шаблона", "%q is not part of LumaTape v1; use functions and unrolled samples from the template"), token)
		}
		if strings.HasPrefix(token, "gl_") || strings.HasPrefix(token, "uSource") || token == "frag" {
			return p, fmt.Errorf(locale.Text("служебное имя %q недоступно; используйте ltSample и ltResolution", "reserved name %q is unavailable; use ltSample and ltResolution"), token)
		}
	}
	p.Source = source + "\n"
	if len(p.Source) > MaxSourceBytes {
		return Pack{}, errors.New(locale.Text("шейдер после нормализации превышает 64 KiB", "the normalized shader exceeds 64 KiB"))
	}
	sum := sha256.Sum256([]byte(p.Source))
	p.ID = hex.EncodeToString(sum[:])
	return p, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func plainText(v string, max int) bool {
	if utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func (p Pack) Defaults() (values [MaxParameters]float64) {
	for i, parameter := range p.Parameters {
		values[i] = parameter.Default
	}
	return
}

func (p Pack) ValidateParams(values [MaxParameters]float64) error {
	for i, v := range values {
		if !finite(v) {
			return errors.New(locale.Text("параметр шейдера не является конечным числом", "shader parameter is not a finite number"))
		}
		if i >= len(p.Parameters) {
			if v != 0 {
				return errors.New(locale.Text("неиспользуемые параметры шейдера должны быть нулевыми", "unused shader parameters must be zero"))
			}
			continue
		}
		parameter := p.Parameters[i]
		if v < parameter.Min || v > parameter.Max {
			return fmt.Errorf(locale.Text("%s: значение вне диапазона", "%s: value outside the allowed range"), parameter.Name)
		}
	}
	return nil
}

func withoutComments(source string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(source); {
		if strings.HasPrefix(source[i:], "//") {
			end := strings.IndexByte(source[i:], '\n')
			if end < 0 {
				break
			}
			i += end
			b.WriteByte('\n')
			continue
		}
		if strings.HasPrefix(source[i:], "/*") {
			end := strings.Index(source[i+2:], "*/")
			if end < 0 {
				return "", errors.New(locale.Text("незавершённый комментарий GLSL", "unterminated GLSL comment"))
			}
			i += end + 4
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(source[i])
		i++
	}
	return b.String(), nil
}
