package view

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"argus-frontend/web"
)

// pages перечисляет страницы и шаблон, который для каждой определяет "content".
var pages = map[string]string{
	"overview":  "templates/overview.html",
	"history":   "templates/history.html",
	"container": "templates/container.html",
	"waiting":   "templates/waiting.html",
}

// Templates - разобранный набор шаблонов дашборда.
//
// Каждая страница получает собственный набор: layout вызывает "content",
// и если сложить все страницы в один набор, определения перетрут друг друга.
// Партиалы общие - их имена уникальны.
type Templates struct {
	src  fs.FS
	live bool

	// version - отпечаток статики, посчитанный один раз при разборе.
	version string

	pages    map[string]*template.Template
	partials *template.Template
}

// ParseTemplates разбирает шаблоны из вшитой файловой системы.
//
// Ошибка здесь означает опечатку в шаблоне, поэтому вызывать её нужно
// на старте процесса, а не на первом запросе.
func ParseTemplates() (*Templates, error) {
	return newTemplates(web.Assets, false)
}

// Live читает шаблоны и статику с диска и перечитывает шаблоны на каждый
// запрос. Нужен только при разработке: без него правка в CSS или в шаблоне
// видна лишь после пересборки бинарника, потому что go:embed вшивает копию.
// Каталог - тот, что содержит templates/ и static/ (обычно ./web).
func Live(dir string) (*Templates, error) {
	_, err := os.Stat(filepath.Join(dir, "templates", "layout.html"))
	if err != nil {
		return nil, fmt.Errorf("в %s нет templates/layout.html: %w", dir, err)
	}

	return newTemplates(os.DirFS(dir), true)
}

func newTemplates(src fs.FS, live bool) (*Templates, error) {
	t := &Templates{src: src, live: live}
	if !live {
		t.version = assetVersion(src)
	}

	return t, t.parse()
}

func (t *Templates) parse() error {
	partials, err := template.New("partials").Funcs(t.funcs()).
		ParseFS(t.src, "templates/partials/*.html")
	if err != nil {
		return fmt.Errorf("партиалы: %w", err)
	}

	set := make(map[string]*template.Template, len(pages))
	for name, file := range pages {
		page, err := template.Must(partials.Clone()).
			ParseFS(t.src, "templates/layout.html", file)
		if err != nil {
			return fmt.Errorf("страница %s: %w", name, err)
		}
		set[name] = page
	}

	t.partials, t.pages = partials, set

	return nil
}

// funcs - хелперы, доступные шаблонам.
func (t *Templates) funcs() template.FuncMap {
	return template.FuncMap{
		"styleWidth": styleWidth,
		"asset":      t.asset,
	}
}

// asset дописывает к пути отпечаток сборки: /static/css/argus.css?v=1a2b3c4d
//
// Ассеты меняются только вместе с бинарником, поэтому их можно кэшировать
// надолго - но после деплоя браузер обязан увидеть новый адрес. В live-режиме
// вместо отпечатка идёт текущее время, чтобы правки были видны сразу.
func (t *Templates) asset(path string) string {
	if t.live {
		return path + "?v=" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}

	return path + "?v=" + t.version
}

// Static возвращает файловую систему со статикой - вшитой или дисковой.
func (t *Templates) Static() (fs.FS, error) {
	return fs.Sub(t.src, "static")
}

// Live сообщает, работает ли набор в режиме разработки.
func (t *Templates) IsLive() bool { return t.live }

// Page рендерит целую страницу.
func (t *Templates) Page(w http.ResponseWriter, name string, data Layout) error {
	err := t.reloadIfLive()
	if err != nil {
		return err
	}
	set, ok := t.pages[name]
	if !ok {
		return fmt.Errorf("неизвестная страница %q", name)
	}

	return render(w, set, "layout", data)
}

// Partial рендерит один партиал - ответ на hx-get.
func (t *Templates) Partial(w http.ResponseWriter, name string, data any) error {
	err := t.reloadIfLive()
	if err != nil {
		return err
	}

	return render(w, t.partials, name, data)
}

func (t *Templates) reloadIfLive() error {
	if !t.live {
		return nil
	}

	return t.parse()
}

// render пишет шаблон в буфер и только потом в ResponseWriter: иначе ошибка
// на середине шаблона оставит наполовину отданный HTML со статусом 200.
func render(w http.ResponseWriter, set *template.Template, name string, data any) error {
	var buf bytes.Buffer

	err := set.ExecuteTemplate(&buf, name, data)
	if err != nil {
		return fmt.Errorf("рендер %s: %w", name, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Партиалы обновляются поллингом - кэш промежуточных прокси только мешает.
	w.Header().Set("Cache-Control", "no-store")

	_, err = io.Copy(w, &buf)

	return err
}
