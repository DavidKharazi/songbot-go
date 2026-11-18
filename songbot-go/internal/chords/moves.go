package chords

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
)

// ChordID — постоянный адрес аккорда в исходном тексте песни: номер строки и номер
// токена в ней. Не меняется при транспонировании, переносе строк и перемещениях аккордов,
// поэтому по нему можно хранить правки пользователя.
type ChordID struct {
	Line  int
	Token int
}

// Move — куда переместить аккорд: Delta — сдвиг в символах относительно исходного
// столбца, Line — строка аккордов, в которую его перенести (-1 — остаться в своей).
type Move struct {
	Delta int
	Line  int
}

// Edit — правка аккорда пользователем.
//
// Для аккорда из текста песни (ID.Line >= 0): Delta и Line — как в Move; Text, если не
// пусто, заменяет аккорд (записан в тональности со сдвигом At относительно исходной);
// Delete убирает аккорд.
//
// Для нового аккорда (ID.Line == -1, ID.Token — его номер): Line — строка аккордов,
// куда он поставлен, Delta — столбец в ней, Text и At — сам аккорд.
type Edit struct {
	Delta  int
	Line   int
	Text   string
	At     int
	Delete bool
	// Order — когда аккорд двигали (больше — позже). Если два перемещённых аккорда
	// претендуют на одно место, его получает тот, кого двигали последним.
	Order int64
}

// NewChordLine — значение ChordID.Line у аккордов, добавленных пользователем.
const NewChordLine = -1

// NormalizeChord проверяет, что text — аккорд (или несколько через дефис, "Em-D"),
// и переводит его из тональности со сдвигом fromShift в исходную тональность песни.
func (s *Song) NormalizeChord(text string, fromShift int) (string, bool) {
	text = strings.TrimSpace(text)
	parts := parseCompound(text)
	if parts == nil || latinChords(parts) == 0 || strings.ContainsAny(text, " \t") {
		return "", false
	}
	sp := spellerFor(s.n, s.Key)
	var b strings.Builder
	for _, p := range parts {
		if c, ok := transposeChord(p.text, -fromShift, s.n, sp); p.isChord && ok {
			b.WriteString(c)
		} else {
			b.WriteString(p.text)
		}
	}
	return b.String(), true
}

// Rev — короткий отпечаток исходного текста. Если текст песни изменится (например,
// в репозитории поправят .txt), старые правки к нему уже не подходят.
func (s *Song) Rev() string {
	sum := sha1.Sum([]byte(strings.Join(s.base().lines, "\n")))
	return hex.EncodeToString(sum[:4])
}

func (s *Song) base() *Song {
	if s.orig != nil {
		return s.orig
	}
	return s
}

// OrigCol — столбец, в котором аккорд стоит в исходном тексте (до правок);
// для добавленных пользователем аккордов — 0 (их Delta и есть столбец).
func (s *Song) OrigCol(id ChordID) int {
	b := s.base()
	if id.Line < 0 || id.Line >= len(b.lines) {
		return 0
	}
	toks := tokenize([]rune(b.lines[id.Line]))
	if id.Token < 0 || id.Token >= len(toks) {
		return 0
	}
	return toks[id.Token].start
}

// idAt — исходный адрес k-го токена строки line с учётом перемещений.
func (s *Song) idAt(line, k int) ChordID {
	if ids, ok := s.ids[line]; ok && k < len(ids) {
		return ids[k]
	}
	return ChordID{Line: line, Token: k}
}

// WithMoves возвращает копию песни с перемещёнными аккордами (см. WithEdits).
func (s *Song) WithMoves(moves map[ChordID]Move) *Song {
	edits := make(map[ChordID]Edit, len(moves))
	for id, m := range moves {
		edits[id] = Edit{Delta: m.Delta, Line: m.Line}
	}
	s2, _ := s.WithEdits(edits)
	return s2
}

// WithEdits возвращает копию песни с правками пользователя: перемещёнными, изменёнными,
// удалёнными и добавленными аккордами. Двигать и принимать аккорды могут только строки,
// состоящие из одних аккордов. Если аккорд «встал» на соседа, перемещённый занимает
// своё место, а сосед отодвигается; между аккордами всегда остаётся пробел.
// bad — правки, которые не удалось применить (например, введён не аккорд).
func (s *Song) WithEdits(edits map[ChordID]Edit) (out *Song, bad []ChordID) {
	b := s.base()
	isChordLine := make([]bool, len(b.lines))
	for i, l := range b.lines {
		isChordLine[i] = IsChordLine(l)
	}
	validLine := func(l int) bool { return l >= 0 && l < len(b.lines) && isChordLine[l] }

	type item struct {
		id    ChordID
		text  string
		want  int
		moved bool
		order int64
	}
	buckets := map[int][]item{}
	touched := map[int]bool{}

	// Новые аккорды.
	for id, e := range edits {
		if id.Line != NewChordLine {
			continue
		}
		text, ok := b.NormalizeChord(e.Text, e.At)
		if !ok || !validLine(e.Line) || e.Delete {
			if !e.Delete {
				bad = append(bad, id)
			}
			continue
		}
		want := e.Delta
		if want < 0 {
			want = 0
		}
		touched[e.Line] = true
		buckets[e.Line] = append(buckets[e.Line], item{id, text, want, true, e.Order})
	}
	// Правки аккордов из текста: какие строки они затрагивают.
	for id, e := range edits {
		if id.Line == NewChordLine || !validLine(id.Line) {
			continue
		}
		touched[id.Line] = true
		if validLine(e.Line) {
			touched[e.Line] = true
		}
	}
	if len(touched) == 0 {
		return b, bad
	}

	for li := range b.lines {
		if !isChordLine[li] {
			continue
		}
		for k, t := range tokenize([]rune(b.lines[li])) {
			id := ChordID{Line: li, Token: k}
			target, want, moved, text, order := li, t.start, false, t.text, int64(0)
			if e, ok := edits[id]; ok {
				order = e.Order
				if e.Delete {
					continue
				}
				want += e.Delta
				moved = e.Delta != 0 || validLine(e.Line)
				if validLine(e.Line) {
					target = e.Line
				}
				if e.Text != "" {
					if norm, ok := b.NormalizeChord(e.Text, e.At); ok {
						text = norm
					} else {
						bad = append(bad, id)
					}
				}
			}
			if !touched[target] {
				continue
			}
			if want < 0 {
				want = 0
			}
			buckets[target] = append(buckets[target], item{id, text, want, moved, order})
		}
	}

	cp := *b
	cp.orig = b
	cp.lines = append([]string(nil), b.lines...)
	cp.ids = map[int][]ChordID{}
	for li := range touched {
		items := buckets[li]
		in := make([]lineItem, len(items))
		for i, it := range items {
			in[i] = lineItem{n: len([]rune(it.text)), want: it.want, pinned: it.moved, order: it.order}
		}
		var out []rune
		ids := make([]ChordID, 0, len(items))
		for _, p := range layoutLine(in) {
			for len(out) < p.start {
				out = append(out, ' ')
			}
			out = append(out, []rune(items[p.idx].text)...)
			ids = append(ids, items[p.idx].id)
		}
		cp.lines[li] = string(out)
		cp.ids[li] = ids
	}
	return &cp, bad
}

// Span — участок строки с аккордом. ID задан, если аккорд можно двигать;
// OrigCol — его столбец в исходном тексте (до правок).
type Span struct {
	Start, End int // в рунах, [Start, End)
	ID         *ChordID
	OrigCol    int
}

// RenderedLine — строка, готовая к показу: текст, где в нём аккорды, и откуда
// она взята в тексте песни (символ в столбце x — это столбец Offset+x строки Src).
type RenderedLine struct {
	Text        string
	Spans       []Span
	Src, Offset int
	ChordLine   bool // строка аккордов, в которую можно перетащить аккорд
}

// Render транспонирует песню на shift полутонов, переносит строки по ширине width
// (меньше 16 — без переноса) и отмечает аккорды с их постоянными адресами.
func (s *Song) Render(shift, width int) []RenderedLine {
	b := s.base()
	transposed := strings.Split(s.Transpose(shift), "\n")

	var wrapped []WrappedLine
	if width >= 16 {
		wrapped = WrapMapped(strings.Join(transposed, "\n"), width)
	} else {
		for i, l := range transposed {
			wrapped = append(wrapped, WrappedLine{Text: l, Src: i})
		}
	}

	chordLine := map[int]bool{}
	isChord := func(src int) bool {
		v, ok := chordLine[src]
		if !ok {
			v = src < len(b.lines) && IsChordLine(b.lines[src])
			chordLine[src] = v
		}
		return v
	}

	tokCache := map[int][]token{}
	out := make([]RenderedLine, len(wrapped))
	for i, w := range wrapped {
		movable := isChord(w.Src)
		out[i] = RenderedLine{Text: w.Text, Src: w.Src, Offset: w.Offset, ChordLine: movable}
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
						id := s.idAt(w.Src, k)
						span.ID = &id
						span.OrigCol = s.OrigCol(id)
						break
					}
				}
			}
			out[i].Spans = append(out[i].Spans, span)
		}
	}
	return out
}

type lineItem struct {
	n      int // длина аккорда
	want   int // желаемый столбец
	pinned bool
	order  int64
}

type placed struct{ idx, start int }

// layoutLine расставляет аккорды в строке (возвращает их в порядке слева направо).
//   - Перемещённые пользователем аккорды встают ровно туда, куда их поставили.
//     Если два таких претендуют на одно место, его получает перемещённый последним,
//     а другой встаёт в ближайшее свободное место.
//   - Остальные сохраняют свой порядок и встают в ближайшее к своему месту свободное
//     место — справа или слева.
//
// Между аккордами всегда остаётся хотя бы один пробел.
func layoutLine(items []lineItem) []placed {
	type span struct{ from, to int } // занято [from, to) вместе с пробелами вокруг
	var busy []span
	free := func(start, n int) bool {
		if start < 0 {
			return false
		}
		for _, b := range busy {
			if start < b.to && start+n > b.from {
				return false
			}
		}
		return true
	}
	// nearest — ближайшее к want свободное место не левее lower.
	nearest := func(want, n, lower int) int {
		if want < lower {
			want = lower
		}
		for d := 0; ; d++ {
			if free(want+d, n) {
				return want + d
			}
			if want-d >= lower && free(want-d, n) {
				return want - d
			}
		}
	}

	var pinned, rest []int
	for i, it := range items {
		if it.pinned {
			pinned = append(pinned, i)
		} else {
			rest = append(rest, i)
		}
	}
	sort.SliceStable(pinned, func(a, b int) bool { return items[pinned[a]].order > items[pinned[b]].order })
	sort.SliceStable(rest, func(a, b int) bool { return items[rest[a]].want < items[rest[b]].want })

	out := make([]placed, 0, len(items))
	for _, i := range pinned {
		pos := nearest(items[i].want, items[i].n, 0)
		busy = append(busy, span{pos - 1, pos + items[i].n + 1})
		out = append(out, placed{i, pos})
	}
	lower := 0
	for _, i := range rest {
		pos := nearest(items[i].want, items[i].n, lower)
		busy = append(busy, span{pos - 1, pos + items[i].n + 1})
		lower = pos + items[i].n + 1
		out = append(out, placed{i, pos})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].start < out[b].start })
	return out
}
