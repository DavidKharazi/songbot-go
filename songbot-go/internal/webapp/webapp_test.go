package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"songbot/internal/chords"
)

func testServer() *Server {
	return New(map[string]*chords.Song{
		"Песня": chords.Parse("Em        Am\nБлаго есть славить Господа, и петь имени Твоему", ""),
	})
}

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestSongTransposedAndWrapped(t *testing.T) {
	rec := get(t, testServer(), "/api/song?"+url.Values{"title": {"Песня"}, "shift": {"2"}, "width": {"20"}}.Encode())
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body)
	}
	var resp songResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Key != "F#m" || resp.OrigKey != "Em" || resp.Capo != 2 || len(resp.Keys) != 12 {
		t.Fatalf("неверная тональность: %+v", resp)
	}
	if resp.Lines[0].Text != "F#m       Bm" || len(resp.Lines[0].Chords) != 2 {
		t.Fatalf("первая строка: %+v", resp.Lines[0])
	}
	for _, l := range resp.Lines {
		if n := len([]rune(l.Text)); n > 20 {
			t.Fatalf("строка длиннее ширины экрана (%d): %q", n, l.Text)
		}
	}
}

func TestUnknownSongAndIndex(t *testing.T) {
	s := testServer()
	if rec := get(t, s, "/api/song?title=нет"); rec.Code != http.StatusNotFound {
		t.Fatalf("ожидался 404, получен %d", rec.Code)
	}
	if rec := get(t, s, "/"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "telegram-web-app.js") {
		t.Fatalf("страница не отдаётся: %d", rec.Code)
	}
	if rec := get(t, s, "/api/songs"); !strings.Contains(rec.Body.String(), "Песня") {
		t.Fatalf("список песен: %s", rec.Body)
	}
}
