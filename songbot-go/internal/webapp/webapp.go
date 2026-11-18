// Package webapp — Telegram Mini App для просмотра аккордов на весь экран.
//
// Сервер отдаёт одну HTML-страницу (static/index.html) и небольшое JSON API.
// Транспонирование и перенос строк делаются тем же кодом, что и в чате (пакет chords),
// поэтому результат совпадает; страница лишь сообщает, сколько символов помещается
// в строку на экране, и получает текст, перенесённый ровно под эту ширину.
package webapp

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"songbot/internal/chords"
)

//go:embed static/index.html
var static embed.FS

// Server обслуживает мини-приложение.
type Server struct {
	songs  map[string]*chords.Song
	titles []string // отсортированные названия песен, у которых есть аккорды текстом
}

// New создаёт сервер поверх уже разобранных песен с аккордами.
func New(songs map[string]*chords.Song) *Server {
	titles := make([]string, 0, len(songs))
	for t := range songs {
		titles = append(titles, t)
	}
	sort.Strings(titles)
	return &Server{songs: songs, titles: titles}
}

// Handler возвращает маршруты мини-приложения.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/songs", s.handleSongs)
	mux.HandleFunc("/api/song", s.handleSong)
	return mux
}

// ListenAndServe запускает сервер и останавливает его при отмене ctx.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	log.Printf("мини-приложение слушает %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, _ := static.ReadFile("static/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(page)
}

func (s *Server) handleSongs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.titles)
}

type keyOption struct {
	Shift int    `json:"shift"`
	Name  string `json:"name"`
}

type line struct {
	Text string `json:"t"`
	// Где в строке аккорды: [начало, конец] или, если аккорд можно двигать,
	// [начало, конец, строка, номер, исходный столбец] — постоянный адрес аккорда
	// в исходном тексте и его столбец там до правок.
	Chords [][]int `json:"c,omitempty"`
	// Откуда строка взята в тексте песни: символ в столбце x — это столбец o+x строки s.
	Src    int  `json:"s"`
	Offset int  `json:"o"`
	Drop   bool `json:"d,omitempty"` // строка аккордов: сюда можно перетащить аккорд
}

type songResponse struct {
	Title   string      `json:"title"`
	Shift   int         `json:"shift"`   // 0..11
	Display int         `json:"display"` // сдвиг для подписи: -5..+6
	HasKey  bool        `json:"hasKey"`
	Key     string      `json:"key,omitempty"`
	OrigKey string      `json:"origKey,omitempty"`
	Capo    int         `json:"capo,omitempty"` // лад каподастра для игры аккордами исходной тональности
	Keys    []keyOption `json:"keys,omitempty"`
	Rev     string      `json:"rev"` // отпечаток текста песни: правки привязаны к нему
	Lines   []line      `json:"lines"`
}

// handleSong: /api/song?title=...&shift=0..11&width=N
// width — сколько символов помещается в строку на экране; 0 — без переноса.
func (s *Server) handleSong(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	title := q.Get("title")
	song, ok := s.songs[title]
	if !ok {
		http.Error(w, "песня не найдена", http.StatusNotFound)
		return
	}
	shift, _ := strconv.Atoi(q.Get("shift"))
	shift = ((shift % 12) + 12) % 12
	width, _ := strconv.Atoi(q.Get("width"))

	rev := song.Rev()
	// Правки положения аккордов хранятся только в телефоне и приходят с запросом,
	// если они сделаны для этой же версии текста песни.
	if q.Get("rev") == rev {
		song = song.WithMoves(parseMoves(q.Get("moves")))
	}

	resp := songResponse{Title: title, Shift: shift, Display: shift, HasKey: song.HasKey, Rev: rev}
	if resp.Display > 6 {
		resp.Display -= 12
	}
	if shift >= 1 && shift <= 7 {
		resp.Capo = shift
	}
	if song.HasKey {
		resp.Key, resp.OrigKey = song.KeyName(shift), song.KeyName(0)
		for k := 0; k < 12; k++ {
			resp.Keys = append(resp.Keys, keyOption{Shift: k, Name: song.KeyName(k)})
		}
		// По порядку нот от до, как на клавиатуре в чате.
		root := song.Key.Root
		sort.Slice(resp.Keys, func(i, j int) bool {
			return (root+resp.Keys[i].Shift)%12 < (root+resp.Keys[j].Shift)%12
		})
	}
	for _, rl := range song.Render(shift, width) {
		l := line{Text: rl.Text, Src: rl.Src, Offset: rl.Offset, Drop: rl.ChordLine}
		for _, sp := range rl.Spans {
			if sp.ID != nil {
				l.Chords = append(l.Chords, []int{sp.Start, sp.End, sp.ID.Line, sp.ID.Token, sp.OrigCol})
			} else {
				l.Chords = append(l.Chords, []int{sp.Start, sp.End})
			}
		}
		resp.Lines = append(resp.Lines, l)
	}
	writeJSON(w, resp)
}

// parseMoves разбирает правки вида "строка.номер.сдвиг[.новая_строка],...".
// Некорректные части пропускаются; размер и величина сдвигов ограничены.
func parseMoves(raw string) map[chords.ChordID]chords.Move {
	moves := map[chords.ChordID]chords.Move{}
	if raw == "" {
		return moves
	}
	for i, item := range strings.Split(raw, ",") {
		if i >= 2000 {
			break
		}
		f := strings.Split(item, ".")
		if len(f) != 3 && len(f) != 4 {
			continue
		}
		var n [4]int
		n[3] = -1
		ok := true
		for j, part := range f {
			v, err := strconv.Atoi(part)
			if err != nil {
				ok = false
				break
			}
			n[j] = v
		}
		if !ok || n[0] < 0 || n[1] < 0 || n[2] < -500 || n[2] > 500 {
			continue
		}
		moves[chords.ChordID{Line: n[0], Token: n[1]}] = chords.Move{Delta: n[2], Line: n[3]}
	}
	return moves
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("webapp: %v", err)
	}
}
