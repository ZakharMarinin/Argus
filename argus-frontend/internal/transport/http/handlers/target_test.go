package handlers_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"argus-frontend/internal/domain/entity"
)

func send(t *testing.T, handler http.Handler, method, path string, form url.Values, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		r.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	return w
}

func TestCreateTarget(t *testing.T) {
	form := url.Values{"name": {"api"}, "url": {"https://api.local/health"}}

	cases := []struct {
		name        string
		err         error
		wantTrigger bool
		wantBody    string
	}{
		{"created", nil, true, `hx-post="/targets"`},
		{"rejected", &entity.ValidationError{Reason: "This URL is already monitored"}, false, "This URL is already monitored"},
		{"collector down", fmt.Errorf("%w: dial", entity.ErrNotReady), false, "The collector is unavailable"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := send(t, newServer(t, fakeUseCase{writeErr: c.err}), http.MethodPost, "/targets", form, nil)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			if got := strings.Contains(w.Header().Get("HX-Trigger"), "refreshNow"); got != c.wantTrigger {
				t.Errorf("HX-Trigger = %q", w.Header().Get("HX-Trigger"))
			}
			if !strings.Contains(w.Body.String(), c.wantBody) {
				t.Errorf("body has no %q:\n%s", c.wantBody, w.Body.String())
			}
			if c.err != nil && !strings.Contains(w.Body.String(), `value="https://api.local/health"`) {
				t.Errorf("form lost the entered URL:\n%s", w.Body.String())
			}
		})
	}
}

func TestDeleteTarget(t *testing.T) {
	w := send(t, newServer(t, fakeUseCase{}), http.MethodDelete, "/targets/t1", nil, nil)
	if w.Code != http.StatusOK || w.Header().Get("HX-Trigger") != "refreshNow" {
		t.Errorf("status = %d, HX-Trigger = %q", w.Code, w.Header().Get("HX-Trigger"))
	}

	w = send(t, newServer(t, fakeUseCase{writeErr: fmt.Errorf("%w: dial", entity.ErrNotReady)}), http.MethodDelete, "/targets/t1", nil, nil)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("HX-Trigger") != "" {
		t.Errorf("collector down: status = %d, HX-Trigger = %q", w.Code, w.Header().Get("HX-Trigger"))
	}
}

func TestWritesRejectCrossOrigin(t *testing.T) {
	handler := newServer(t, fakeUseCase{})
	cross := map[string]string{"Sec-Fetch-Site": "cross-site"}

	for _, r := range []struct{ method, path string }{
		{http.MethodPost, "/targets"},
		{http.MethodDelete, "/targets/t1"},
	} {
		w := send(t, handler, r.method, r.path, url.Values{"name": {"x"}, "url": {"http://x"}}, cross)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s cross-site: status = %d, want 403", r.method, r.path, w.Code)
		}
	}

	if w := send(t, handler, http.MethodDelete, "/targets/t1", nil, map[string]string{"Sec-Fetch-Site": "same-origin"}); w.Code != http.StatusOK {
		t.Errorf("same-origin delete: status = %d", w.Code)
	}
}

func TestUptimeTargetsHaveDeleteButton(t *testing.T) {
	w := get(t, newServer(t, filledUseCase()), "/partials/targets")
	if !strings.Contains(w.Body.String(), `hx-delete="/targets/`) {
		t.Errorf("no delete button in uptime targets:\n%s", w.Body.String())
	}
}
