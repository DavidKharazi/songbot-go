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
	Text   string   `json:"t"`
	Chords [][2]int `json:"c,omitempty"` // где в строке аккорды (для подсветки)
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

	text := song.Transpose(shift)
	if width >= 16 {
		text = chords.Wrap(text, width)
	}

	resp := songResponse{Title: title, Shift: shift, Display: shift, HasKey: song.HasKey}
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
	for _, t := range splitLines(text) {
		resp.Lines = append(resp.Lines, line{Text: t, Chords: chords.ChordSpans(t)})
	}
	writeJSON(w, resp)
}

func splitLines(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	return append(out, text[start:])
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("webapp: %v", err)
	}
}
