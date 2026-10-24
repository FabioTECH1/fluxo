package server

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"fluxo/internal/services/phpupdate"
)

func TestPHPNotInstalledErrorIsActionable(t *testing.T) {
	w := httptest.NewRecorder()
	phpError(w, fmt.Errorf("%w: PHP 8.5; install this version first", phpupdate.ErrNotInstalled))
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"code":"runtime_not_installed"`) || !strings.Contains(w.Body.String(), "PHP 8.5") {
		t.Fatalf("missing PHP runtime was mislabeled: %d %s", w.Code, w.Body.String())
	}
}
