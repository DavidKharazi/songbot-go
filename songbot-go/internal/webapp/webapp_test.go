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

func TestSongWithLocalMoves(t *testing.T) {
	s := testServer()
	var plain songResponse
	json.Unmarshal(get(t, s, "/api/song?"+url.Values{"title": {"Песня"}}.Encode()).Body.Bytes(), &plain)
	am := plain.Lines[0].Chords[1] // [начало, конец, строка, номер]
	if len(am) != 4 || am[0] != 10 {
		t.Fatalf("у Am нет адреса или неверный столбец: %v", am)
	}

	moved := func(rev string) []int {
		var r songResponse
		q := url.Values{"title": {"Песня"}, "rev": {rev}, "moves": {"0.1.3,мусор,9.9"}}
		json.Unmarshal(get(t, s, "/api/song?"+q.Encode()).Body.Bytes(), &r)
		return r.Lines[0].Chords[1]
	}
	if got := moved(plain.Rev); got[0] != 13 {
		t.Fatalf("Am должен сдвинуться на 3 вправо: %v", got)
	}
	// Правки к другой версии текста не применяются.
	if got := moved("старый"); got[0] != 10 {
		t.Fatalf("правки к другой версии текста не должны применяться: %v", got)
	}
}
