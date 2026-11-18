package chords

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// ChordID — постоянный адрес аккорда в исходном тексте песни: номер строки и номер
// токена в ней. Не меняется при транспонировании, переносе строк и сдвигах аккордов,
// поэтому по нему можно хранить правки пользователя.
type ChordID struct {
	Line  int
	Token int
}

// Rev — короткий отпечаток исходного текста. Если текст песни изменится (например,
// в репозитории поправят .txt), старые правки к нему уже не подходят.
func (s *Song) Rev() string {
	sum := sha1.Sum([]byte(strings.Join(s.lines, "\n")))
	return hex.EncodeToString(sum[:4])
}

// WithMoves возвращает копию песни, в которой аккорды сдвинуты на заданное число
// символов (отрицательное — влево). Сдвигаются только аккорды в строках, состоящих из
// одних аккордов. Аккорды не налезают друг на друга: между ними остаётся хотя бы
// один пробел, а левее начала строки аккорд не уходит.
func (s *Song) WithMoves(moves map[ChordID]int) *Song {
	if len(moves) == 0 {
		return s
	}
	byLine := map[int]map[int]int{}
	for id, d := range moves {
		if id.Line < 0 || id.Line >= len(s.lines) || d == 0 {
			continue
		}
		if byLine[id.Line] == nil {
			byLine[id.Line] = map[int]int{}
		}
		byLine[id.Line][id.Token] = d
	}
	if len(byLine) == 0 {
		return s
	}

	cp := *s
	cp.lines = append([]string(nil), s.lines...)
	for li, m := range byLine {
		if !IsChordLine(cp.lines[li]) {
			continue
		}
		var out []rune
		for k, t := range tokenize([]rune(cp.lines[li])) {
			want := t.start + m[k]
			if want < 0 {
				want = 0
			}
			if k > 0 && want < len(out)+1 {
				want = len(out) + 1
			}
			for len(out) < want {
				out = append(out, ' ')
			}
			out = append(out, []rune(t.text)...)
		}
		cp.lines[li] = string(out)
	}
	return &cp
}

// Span — участок строки с аккордом. ID задан, если аккорд можно двигать.
type Span struct {
	Start, End int // в рунах, [Start, End)
	ID         *ChordID
}

// RenderedLine — строка, готовая к показу: текст и где в нём аккорды.
type RenderedLine struct {
	Text  string
	Spans []Span
}

// Render транспонирует песню на shift полутонов, переносит строки по ширине width
// (меньше 16 — без переноса) и отмечает аккорды с их постоянными адресами.
func (s *Song) Render(shift, width int) []RenderedLine {
	transposed := strings.Split(s.Transpose(shift), "\n")

	var wrapped []WrappedLine
	if width >= 16 {
		wrapped = WrapMapped(strings.Join(transposed, "\n"), width)
	} else {
		for i, l := range transposed {
			wrapped = append(wrapped, WrappedLine{Text: l, Src: i})
		}
	}

	tokCache := map[int][]token{}
	out := make([]RenderedLine, len(wrapped))
	for i, w := range wrapped {
		out[i].Text = w.Text
		movable := w.Src < len(s.lines) && IsChordLine(s.lines[w.Src])
		for _, sp := range ChordSpans(w.Text) {
			span := Span{Start: sp[0], End: sp[1]}
			if movable {
				toks, ok := tokCache[w.Src]
				if !ok {
					toks = tokenize([]rune(transposed[w.Src]))
					tokCache[w.Src] = toks
				}
				col := w.Offset + sp[0]
				for k, t := range toks {
					if t.start == col {
						span.ID = &ChordID{Line: w.Src, Token: k}
						break
					}
				}
			}
			out[i].Spans = append(out[i].Spans, span)
		}
	}
	return out
}
