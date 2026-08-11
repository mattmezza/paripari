package view

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDesignShellRenders(t *testing.T) {
	fsys := os.DirFS("../../templates")
	v, err := New(fsys, "https://paripari.app", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ page, path string; want []string }{
		{"auth-login", "/login", []string{"SoftwareApplication", "Household finance", `action="/login"`, "wordmark__b"}},
		{"auth-signup", "/signup", []string{"SoftwareApplication", `name="invite"`}},
		{"dashboard", "/", []string{`class="sidebar"`, `class="tabbar"`, "more-sheet", "noindex"}},
		{"transfers", "/transfers", []string{`class="tabbar"`, "aria-current=\"page\""}},
		{"styleguide", "/styleguide", []string{"split-legend", "data-tick"}},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		w := httptest.NewRecorder()
		v.Render(w, r, tc.page, nil)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", tc.page, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if strings.Contains(body, "ZgotmplZ") {
			t.Errorf("%s: escaping failure (ZgotmplZ)", tc.page)
		}
		for _, s := range tc.want {
			if !strings.Contains(body, s) {
				t.Errorf("%s: missing %q", tc.page, s)
			}
		}
	}
}
