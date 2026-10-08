package shaderpack

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aiwaki/lumatape/internal/locale"
)

const extension = ".lumatape.glsl"

func Load(directory, id string) (Pack, error) {
	if !ValidID(id) {
		return Pack{}, errors.New(locale.Text("неверный идентификатор шейдера", "invalid shader identifier"))
	}
	f, err := os.Open(filepath.Join(directory, id+extension))
	if err != nil {
		return Pack{}, err
	}
	defer f.Close()
	source, err := io.ReadAll(io.LimitReader(f, MaxSourceBytes+1))
	if err != nil {
		return Pack{}, err
	}
	p, err := Parse(string(source))
	if err != nil {
		return Pack{}, err
	}
	if p.ID != id {
		return Pack{}, errors.New(locale.Text("содержимое сохранённого шейдера изменилось; импортируйте файл заново", "the saved shader contents changed; import the file again"))
	}
	return p, nil
}

// Save is called only after a successful real-driver compile. Immutable content
// IDs leave an existing working shader available when a new import fails.
func Save(directory string, p Pack) error {
	verified, err := Parse(p.Source)
	if err != nil {
		return err
	}
	if verified.ID != p.ID {
		return errors.New(locale.Text("идентификатор не соответствует содержимому шейдера", "identifier does not match shader contents"))
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if existing, e := Load(directory, p.ID); e == nil && existing.Source == p.Source {
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), extension) {
			count++
		}
	}
	if count >= MaxPacks {
		return fmt.Errorf(locale.Text("в библиотеке уже %d шейдеров", "the library already contains %d shaders"), MaxPacks)
	}
	f, err := os.CreateTemp(directory, ".import-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.WriteString(p.Source); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replaceFile(tmp, filepath.Join(directory, p.ID+extension))
}

// List returns valid packs alongside a diagnostic if some files are broken.
// Callers can still expose the usable library rather than losing every item.
func List(directory string) ([]Descriptor, error) {
	items := []Descriptor{}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return items, err
	}
	var failures []error
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), extension) {
			continue
		}
		count++
		if count > MaxPacks {
			failures = append(failures, errors.New(locale.Text("библиотека превышает 128 шейдеров", "the library exceeds 128 shaders")))
			break
		}
		p, e := Load(directory, strings.TrimSuffix(entry.Name(), extension))
		if e != nil {
			failures = append(failures, errors.New(locale.Text("один из файлов библиотеки повреждён; импортируйте его заново", "one of the library files is damaged; import it again")))
			continue
		}
		items = append(items, p.Descriptor)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items, errors.Join(failures...)
}

func Remove(directory, id string) error {
	if !ValidID(id) {
		return errors.New(locale.Text("неверный идентификатор шейдера", "invalid shader identifier"))
	}
	return os.Remove(filepath.Join(directory, id+extension))
}
