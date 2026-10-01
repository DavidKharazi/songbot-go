// Package chords транспонирует текст песни с аккордами, записанными строкой над словами,
// сохраняя выравнивание аккордов по столбцам, а также переносит длинные строки
// так, чтобы аккорды оставались над нужными слогами и на узком экране телефона.
package chords

import (
	"regexp"
	"strings"
	"unicode"
)

// Нотации: в немецкой (принятой у многих русскоязычных музыкантов) H — это си,
// а B — си-бемоль (при выводе пишем его как Bb, чтобы не было путаницы); в английской B — это си, а си-бемоль пишется Bb или A#.
type notation int

const (
	english notation = iota
	german
)

// Корень аккорда. Кириллические А, В, С, Е, Н выглядят как латинские и часто
// попадают в аккорды при наборе на русской раскладке.
const rootClass = `[A-HАВСЕН]`

var (
	chordRe = regexp.MustCompile(`^(` + rootClass + `)(is|[#b♯♭]?)((?:maj|min|dim|aug|sus|add|m|M|\+|°|ø|\d)*)` +
		`(?:/(` + rootClass + `)(is|[#b♯♭]?)|\((` + rootClass + `)(is|[#b♯♭]?)\))?$`)
	bassOnlyRe = regexp.MustCompile(`^/(` + rootClass + `)(is|[#b♯♭]?)$`)
	// Разделители, которые встречаются в строках аккордов: такты, повторы, размер, скобки.
	separatorRe = regexp.MustCompile(`^(?:[|\-–—/\\.,:;()\[\]*¾½]+|\|?\d+/\d+\|?|\(?[xх×]?\d+[xх×]?\)?|\d*раза?|N\.?C\.?)$`)
	// Тональность в имени файла: "О благодать (D)", "Буду тверд ... Твоих E".
	fileKeyRe = regexp.MustCompile(`(?:\((` + rootClass + `[#b]?m?)\)|\s(` + rootClass + `[#b]?m?))\s*$`)
)

// Ноты от до (0) до си (11).
var latinRoot = map[rune]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11, 'H': 11}

func normalizeRoot(r rune) rune {
	switch r {
	case 'А':
		return 'A'
	case 'В':
		return 'B'
	case 'С':
		return 'C'
	case 'Е':
		return 'E'
	case 'Н':
		return 'H'
	}
	return r
}

func isLatinRoot(s string) bool {
	return s != "" && s[0] >= 'A' && s[0] <= 'H'
}

// noteValue переводит корень и альтерацию в номер полутона 0..11.
func noteValue(root, acc string, n notation) int {
	r := normalizeRoot([]rune(root)[0])
	v := latinRoot[r]
	if r == 'B' && n == german {
		// В немецкой нотации B — уже си-бемоль; "Bb" тоже понимаем как си-бемоль.
		if acc == "b" || acc == "♭" {
			return 10
		}
		v = 10
	}
	switch acc {
	case "#", "♯", "is":
		v++
	case "b", "♭":
		v--
	}
	return (v + 12) % 12
}

var (
	sharpNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	flatNames  = [12]string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}
)

// speller выбирает, как записать ноту: диезом или бемолем и в какой нотации.
type speller struct {
	n     notation
	flats bool
}

func (s speller) name(v int) string {
	v = ((v % 12) + 12) % 12
	if s.n == german {
		// Си пишем как H. Си-бемоль — как Bb, а не B: так запись однозначна
		// для музыкантов, привыкших к любой из нотаций.
		switch v {
		case 10:
			return "Bb"
		case 11:
			return "H"
		}
	}
	if s.flats {
		return flatNames[v]
	}
	return sharpNames[v]
}

// Тональности, в которых принято писать бемоли.
var flatMajor = map[int]bool{5: true, 10: true, 3: true, 8: true, 1: true, 6: true} // F Bb Eb Ab Db Gb
var flatMinor = map[int]bool{2: true, 7: true, 0: true, 5: true, 10: true, 3: true} // Dm Gm Cm Fm Bbm Ebm

func spellerFor(n notation, key Key) speller {
	if key.Minor {
		return speller{n: n, flats: flatMinor[key.Root]}
	}
	return speller{n: n, flats: flatMajor[key.Root]}
}

// transposeChord транспонирует один аккорд; ok=false, если это не аккорд.
func transposeChord(tok string, shift int, n notation, sp speller) (string, bool) {
	if m := bassOnlyRe.FindStringSubmatch(tok); m != nil {
		return "/" + sp.name(noteValue(m[1], m[2], n)+shift), true
	}
	m := chordRe.FindStringSubmatch(tok)
	if m == nil {
		return "", false
	}
	out := sp.name(noteValue(m[1], m[2], n)+shift) + m[3]
	switch {
	case m[4] != "":
		out += "/" + sp.name(noteValue(m[4], m[5], n)+shift)
	case m[6] != "":
		out += "(" + sp.name(noteValue(m[6], m[7], n)+shift) + ")"
	}
	return out, true
}

// ---------- Составные токены ----------

// Один токен может содержать несколько аккордов: "Em-D-C", "(F#)", "|C", "FG", "C-G/H".
// part — кусок токена: либо аккорд, либо разделитель между аккордами.
type part struct {
	text    string
	isChord bool
}

func isWrapper(r rune) bool { return strings.ContainsRune("([|", r) }
func isCloser(r rune) bool  { return strings.ContainsRune(")]|.,", r) }
func isJoiner(r rune) bool  { return strings.ContainsRune("-–—|", r) }

// splitGlued делит слитно записанные аккорды: "CGC" -> C, G, C; "EH" -> E, H.
// Новый аккорд начинается с латинской A–H, если перед ней не "/" и не "(".
func splitGlued(s []rune) []string {
	var out []string
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'H' && s[i-1] != '/' && s[i-1] != '(' {
			out = append(out, string(s[start:i]))
			start = i
		}
	}
	return append(out, string(s[start:]))
}

// parseCompound разбирает токен на аккорды и разделители. Возвращает nil, если в токене
// есть что-то, кроме аккордов (то есть это слово из текста песни).
func parseCompound(tok string) []part {
	r := []rune(tok)
	var parts []part
	i, j := 0, len(r)
	for i < j && isWrapper(r[i]) {
		i++
	}
	for j > i && isCloser(r[j-1]) && !(r[j-1] == ')' && strings.ContainsRune(string(r[i:j-1]), '(')) {
		j--
	}
	if i > 0 {
		parts = append(parts, part{text: string(r[:i])})
	}
	chords := 0
	for k := i; k < j; {
		if isJoiner(r[k]) {
			e := k
			for e < j && isJoiner(r[e]) {
				e++
			}
			parts = append(parts, part{text: string(r[k:e])})
			k = e
			continue
		}
		e := k
		for e < j && !isJoiner(r[e]) {
			e++
		}
		for _, c := range splitGlued(r[k:e]) {
			if !chordRe.MatchString(c) && !bassOnlyRe.MatchString(c) {
				return nil
			}
			parts = append(parts, part{text: c, isChord: true})
			chords++
		}
		k = e
	}
	if chords == 0 {
		return nil
	}
	if j < len(r) {
		parts = append(parts, part{text: string(r[j:])})
	}
	return parts
}

// latinChords считает аккорды с латинским корнем (кириллические "А", "В", "С"
// без латинских соседей скорее всего слова из текста, а не аккорды).
func latinChords(parts []part) int {
	n := 0
	for _, p := range parts {
		if p.isChord && isLatinRoot(strings.TrimPrefix(p.text, "/")) {
			n++
		}
	}
	return n
}

// ---------- Разбор строк ----------

type token struct {
	start int // позиция в рунах
	text  string
	parts []part // не nil, если токен состоит из аккордов
}

func tokenize(line []rune) []token {
	var toks []token
	for i := 0; i < len(line); {
		if unicode.IsSpace(line[i]) {
			i++
			continue
		}
		j := i
		for j < len(line) && !unicode.IsSpace(line[j]) {
			j++
		}
		text := string(line[i:j])
		toks = append(toks, token{start: i, text: text, parts: parseCompound(text)})
		i = j
	}
	return toks
}

func (t token) isSeparator() bool { return t.parts == nil && separatorRe.MatchString(t.text) }

// chordTokenIndexes возвращает индексы токенов строки, которые нужно транспонировать.
// Строка делится на непрерывные участки из аккордов и разделителей. Участок считается
// аккордами, если в нём не меньше двух аккордов, или если это вся строка, или если он
// идёт после подписи ("Вступление: C Dm | Am G"), слова "Тональность" или внутри // ... //.
func chordTokenIndexes(toks []token) []int {
	var idx []int
	for i := 0; i < len(toks); {
		if toks[i].parts == nil && !toks[i].isSeparator() {
			i++
			continue
		}
		j := i
		latin, total, slashes := 0, 0, false
		for j < len(toks) && (toks[j].parts != nil || toks[j].isSeparator()) {
			if toks[j].parts != nil {
				latin += latinChords(toks[j].parts)
				for _, p := range toks[j].parts {
					if p.isChord {
						total++
					}
				}
			}
			if toks[j].text == "//" {
				slashes = true
			}
			j++
		}
		whole := i == 0 && j == len(toks)
		afterLabel := i > 0 && (strings.HasSuffix(toks[i-1].text, ":") ||
			strings.HasPrefix(strings.ToLower(toks[i-1].text), "тональност"))
		if latin >= 1 && (total >= 2 || whole || afterLabel || slashes) {
			for k := i; k < j; k++ {
				if toks[k].parts != nil {
					idx = append(idx, k)
				}
			}
		}
		i = j
	}
	return idx
}

// IsChordLine сообщает, состоит ли строка только из аккордов (и разделителей).
func IsChordLine(line string) bool {
	toks := tokenize([]rune(line))
	idx := chordTokenIndexes(toks)
	if len(idx) == 0 {
		return false
	}
	for _, t := range toks {
		if t.parts == nil && !t.isSeparator() {
			return false
		}
	}
	return true
}

// rebuild собирает строку заново, заменяя токены и сохраняя их столбцы.
// Если новый аккорд длиннее старого, следующий токен сдвигается, но между ними
// всегда остаётся хотя бы один пробел.
func rebuild(toks []token, replace map[int]string) string {
	var out []rune
	for i, t := range toks {
		text := t.text
		if r, ok := replace[i]; ok {
			text = r
		}
		if len(out) < t.start {
			for len(out) < t.start {
				out = append(out, ' ')
			}
		} else if i > 0 && out[len(out)-1] != ' ' {
			out = append(out, ' ')
		}
		out = append(out, []rune(text)...)
	}
	return string(out)
}

// ---------- Тональность ----------

// Key — тональность: корень (0..11) и минор/мажор.
type Key struct {
	Root  int
	Minor bool
}

func parseKey(s string, n notation) (Key, bool) {
	m := chordRe.FindStringSubmatch(s)
	if m == nil {
		return Key{}, false
	}
	return Key{Root: noteValue(m[1], m[2], n), Minor: strings.HasPrefix(m[3], "m") && !strings.HasPrefix(m[3], "maj")}, true
}

// Song — разобранный текст песни с аккордами.
type Song struct {
	lines  []string
	n      notation
	Key    Key  // исходная тональность
	HasKey bool // удалось ли определить тональность
}

// Parse разбирает текст. fileName (без расширения) используется для поиска тональности
// в названии файла, например "О благодать (D)".
func Parse(text, fileName string) *Song {
	s := &Song{lines: strings.Split(StripFormatChars(text), "\n"), n: english}

	var firstChord string
	for _, line := range s.lines {
		toks := tokenize([]rune(line))
		for _, i := range chordTokenIndexes(toks) {
			for _, p := range toks[i].parts {
				if !p.isChord {
					continue
				}
				if firstChord == "" && !strings.HasPrefix(p.text, "/") {
					firstChord = p.text
				}
				if hasH(p.text) {
					s.n = german
				}
			}
		}
	}

	// Приоритет: явная "Тональность X" в тексте, затем имя файла, затем первый аккорд.
	for _, line := range s.lines {
		toks := tokenize([]rune(line))
		for k, t := range toks {
			if strings.HasPrefix(strings.ToLower(t.text), "тональность") && k+1 < len(toks) {
				if key, ok := parseKey(toks[k+1].text, s.n); ok {
					s.Key, s.HasKey = key, true
					return s
				}
			}
		}
	}
	if m := fileKeyRe.FindStringSubmatch(fileName); m != nil {
		k := m[1]
		if k == "" {
			k = m[2]
		}
		if key, ok := parseKey(k, s.n); ok {
			s.Key, s.HasKey = key, true
			return s
		}
	}
	if key, ok := parseKey(firstChord, s.n); ok {
		s.Key, s.HasKey = key, true
	}
	return s
}

// StripFormatChars удаляет невидимые символы управления направлением текста
// (U+202D и т.п.), которые попадают в текст из некоторых PDF и сбивают выравнивание.
func StripFormatChars(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
}

// hasH — встречается ли в аккорде нота H (признак немецкой нотации).
func hasH(c string) bool {
	if m := bassOnlyRe.FindStringSubmatch(c); m != nil {
		return normalizeRoot([]rune(m[1])[0]) == 'H'
	}
	m := chordRe.FindStringSubmatch(c)
	if m == nil {
		return false
	}
	for _, g := range []string{m[1], m[4], m[6]} {
		if g != "" && normalizeRoot([]rune(g)[0]) == 'H' {
			return true
		}
	}
	return false
}

// KeyName возвращает название тональности, сдвинутой на shift полутонов.
func (s *Song) KeyName(shift int) string {
	k := Key{Root: (s.Key.Root + shift%12 + 12) % 12, Minor: s.Key.Minor}
	name := spellerFor(s.n, k).name(k.Root)
	if k.Minor {
		name += "m"
	}
	return name
}

// Transpose возвращает текст, сдвинутый на shift полутонов. При shift, кратном 12,
// текст возвращается как есть — без переписывания аккордов.
func (s *Song) Transpose(shift int) string {
	shift = ((shift % 12) + 12) % 12
	if shift == 0 {
		return strings.Join(s.lines, "\n")
	}
	target := Key{Root: (s.Key.Root + shift) % 12, Minor: s.Key.Minor}
	sp := spellerFor(s.n, target)

	out := make([]string, len(s.lines))
	for li, line := range s.lines {
		toks := tokenize([]rune(line))
		idx := chordTokenIndexes(toks)
		if len(idx) == 0 {
			out[li] = line
			continue
		}
		repl := make(map[int]string, len(idx))
		for _, i := range idx {
			var b strings.Builder
			for _, p := range toks[i].parts {
				if c, ok := transposeChord(p.text, shift, s.n, sp); p.isChord && ok {
					b.WriteString(c)
				} else {
					b.WriteString(p.text)
				}
			}
			repl[i] = b.String()
		}
		// Сохраняем исходный отступ строки: rebuild восстанавливает столбцы токенов.
		out[li] = rebuild(toks, repl)
	}
	return strings.Join(out, "\n")
}
