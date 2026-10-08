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

// pages lists the pages and, for each, the template that defines "content".
var pages = map[string]string{
	"overview":  "templates/overview.html",
	"history":   "templates/history.html",
	"container": "templates/container.html",
	"waiting":   "templates/waiting.html",
}

// Templates is the parsed set of dashboard templates.
//
// Each page gets its own set: layout calls "content", and if all pages were
// put into one set, the definitions would overwrite each other.
// Partials are shared: their names are unique.
type Templates struct {
	src  fs.FS
	live bool

	// version is the static files fingerprint, computed once at parse time.
	version string

	pages    map[string]*template.Template
	partials *template.Template
}

// ParseTemplates parses templates from the embedded file system.
//
// An error here means a typo in a template, so it must be called at process
// startup, not on the first request.
func ParseTemplates() (*Templates, error) {
	return newTemplates(web.Assets, false)
}

// Live reads templates and static files from disk and re-reads templates on
// every request. Only needed during development: without it a change in CSS or a
// template shows up only after rebuilding the binary, because go:embed embeds a copy.
// The directory is the one that contains templates/ and static/ (usually ./web).
func Live(dir string) (*Templates, error) {
	_, err := os.Stat(filepath.Join(dir, "templates", "layout.html"))
	if err != nil {
		return nil, fmt.Errorf("no templates/layout.html in %s: %w", dir, err)
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
		return fmt.Errorf("partials: %w", err)
	}

	set := make(map[string]*template.Template, len(pages))
	for name, file := range pages {
		page, err := template.Must(partials.Clone()).
			ParseFS(t.src, "templates/layout.html", file)
		if err != nil {
			return fmt.Errorf("page %s: %w", name, err)
		}
		set[name] = page
	}

	t.partials, t.pages = partials, set

	return nil
}

// funcs holds the helpers available to templates.
func (t *Templates) funcs() template.FuncMap {
	return template.FuncMap{
		"styleWidth": styleWidth,
		"asset":      t.asset,
	}
}

// asset appends the build fingerprint to a path: /static/css/argus.css?v=1a2b3c4d
//
// Assets change only together with the binary, so they can be cached for a
// long time, but after a deploy the browser must see a new address. In live mode
// the current time is used instead of the fingerprint, so changes show up right away.
func (t *Templates) asset(path string) string {
	if t.live {
		return path + "?v=" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}

	return path + "?v=" + t.version
}

// Static returns the file system with static files, embedded or on disk.
func (t *Templates) Static() (fs.FS, error) {
	return fs.Sub(t.src, "static")
}

// Live reports whether the set runs in development mode.
func (t *Templates) IsLive() bool { return t.live }

// Page renders a whole page.
func (t *Templates) Page(w http.ResponseWriter, name string, data Layout) error {
	err := t.reloadIfLive()
	if err != nil {
		return err
	}
	set, ok := t.pages[name]
	if !ok {
		return fmt.Errorf("unknown page %q", name)
	}

	return render(w, set, "layout", data)
}

// Partial renders a single partial, the response to an hx-get.
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

// render writes the template to a buffer and only then to the ResponseWriter:
// otherwise an error halfway through the template would leave half-sent HTML with status 200.
func render(w http.ResponseWriter, set *template.Template, name string, data any) error {
	var buf bytes.Buffer

	err := set.ExecuteTemplate(&buf, name, data)
	if err != nil {
		return fmt.Errorf("render %s: %w", name, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Partials are refreshed by polling: caching by intermediate proxies only gets in the way.
	w.Header().Set("Cache-Control", "no-store")

	_, err = io.Copy(w, &buf)

	return err
}
