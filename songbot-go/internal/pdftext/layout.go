package pdftext

import (
	"bytes"
	"encoding/xml"
	"io"
	"math"
	"sort"
	"strings"

	"songbot/internal/chords"
)

// В PDF текст набран пропорциональным шрифтом, а аккорды часто ещё и другого размера,
// поэтому `pdftotext -layout` (который переводит координаты в столбцы по средней ширине
// символа) сдвигает аккорды относительно слов. Здесь раскладка строится заново по точным
// координатам слов из `pdftotext -bbox-layout`: каждый аккорд ставится над той буквой
// строки текста под ним, над которой он стоит в PDF.

type word struct {
	x0, y0, x1, y1 float64
	text           string
}

type row struct {
	words  []word
	y0, y1 float64
}

func (r row) text() string {
	parts := make([]string, len(r.words))
	for i, w := range r.words {
		parts[i] = w.text
	}
	return strings.Join(parts, " ")
}

// parseBBox читает XHTML от `pdftotext -bbox-layout` и возвращает слова постранично.
func parseBBox(data []byte) ([][]word, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	var pages [][]word
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "page":
			pages = append(pages, nil)
		case "word":
			var w struct {
				XMin float64 `xml:"xMin,attr"`
				YMin float64 `xml:"yMin,attr"`
				XMax float64 `xml:"xMax,attr"`
				YMax float64 `xml:"yMax,attr"`
				Text string  `xml:",chardata"`
			}
			if err := dec.DecodeElement(&w, &se); err != nil {
				return nil, err
			}
			t := strings.TrimSpace(chords.StripFormatChars(w.Text))
			if t == "" || len(pages) == 0 {
				continue
			}
			pages[len(pages)-1] = append(pages[len(pages)-1], word{w.XMin, w.YMin, w.XMax, w.YMax, t})
		}
	}
	return pages, nil
}

// groupRows собирает слова в визуальные строки по вертикали.
func groupRows(words []word) []row {
	sort.SliceStable(words, func(i, j int) bool { return words[i].y0 < words[j].y0 })
	var rows []row
	for _, w := range words {
		if n := len(rows); n > 0 {
			r := &rows[n-1]
			h := math.Min(r.y1-r.y0, w.y1-w.y0)
			if math.Abs(w.y0-r.y0) < h*0.4 {
				r.words = append(r.words, w)
				r.y0, r.y1 = math.Min(r.y0, w.y0), math.Max(r.y1, w.y1)
				continue
			}
		}
		rows = append(rows, row{words: []word{w}, y0: w.y0, y1: w.y1})
	}
	for i := range rows {
		sort.Slice(rows[i].words, func(a, b int) bool { return rows[i].words[a].x0 < rows[i].words[b].x0 })
	}
	return rows
}

// placedLine — строка текста и x-координата начала каждого её символа.
type placedLine struct {
	runes []rune
	xs    []float64
}

// placeLyric раскладывает строку текста: буквы внутри слова распределяются равномерно
// по его ширине, широкие промежутки между словами сохраняются несколькими пробелами.
func placeLyric(r row, margin, charW float64) placedLine {
	var p placedLine
	indent := int(math.Round((r.words[0].x0 - margin) / charW))
	for i := 0; i < indent; i++ {
		p.runes = append(p.runes, ' ')
		p.xs = append(p.xs, margin+float64(i)*charW)
	}
	for i, w := range r.words {
		if i > 0 {
			prev := r.words[i-1]
			gap := w.x0 - prev.x1
			if gap < charW*0.3 {
				// Части одного слова: PDF иногда хранит слово кусками ("в" + "ерю").
				gap = 0
			}
			n := int(math.Round(gap / charW))
			if n < 1 && gap > 0 {
				n = 1
			}
			for k := 0; k < n; k++ {
				p.runes = append(p.runes, ' ')
				p.xs = append(p.xs, prev.x1+gap*float64(k)/float64(n))
			}
		}
		rs := []rune(w.text)
		step := (w.x1 - w.x0) / float64(len(rs))
		for k, ch := range rs {
			p.runes = append(p.runes, ch)
			p.xs = append(p.xs, w.x0+step*float64(k))
		}
	}
	return p
}

// columnFor — столбец строки текста, над которым стоит x (ближайшее начало буквы);
// правее конца строки столбцы продолжаются со средней шириной символа.
func columnFor(p placedLine, x, margin, charW float64) int {
	if len(p.xs) == 0 {
		return int(math.Round((x - margin) / charW))
	}
	last := len(p.xs) - 1
	end := p.xs[last] + charW
	if x >= end {
		return last + 1 + int(math.Round((x-end)/charW))
	}
	best, bestD := 0, math.Inf(1)
	for i, cx := range p.xs {
		if d := math.Abs(cx - x); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// placeChords ставит аккорды строки в нужные столбцы, не давая им слипнуться.
func placeChords(r row, col func(x float64) int) string {
	var out []rune
	for _, w := range r.words {
		c := col(w.x0)
		if c < 0 {
			c = 0
		}
		if len(out) > 0 && c <= len(out) {
			c = len(out) + 1
		}
		for len(out) < c {
			out = append(out, ' ')
		}
		out = append(out, []rune(w.text)...)
	}
	return string(out)
}

// layoutPage превращает слова страницы в текст с аккордами над словами.
func layoutPage(words []word) []string {
	if len(words) == 0 {
		return nil
	}
	rows := groupRows(words)
	isChord := make([]bool, len(rows))
	margin := math.Inf(1)
	var widths []float64
	for i, r := range rows {
		isChord[i] = chords.IsChordLine(r.text())
		margin = math.Min(margin, r.words[0].x0)
		if !isChord[i] {
			for _, w := range r.words {
				if n := len([]rune(w.text)); n > 1 {
					widths = append(widths, (w.x1-w.x0)/float64(n))
				}
			}
		}
	}
	charW := 6.0
	if len(widths) > 0 {
		sort.Float64s(widths)
		charW = widths[len(widths)/2]
	}

	// Пустая строка там, где вертикальный промежуток заметно больше обычного.
	blankBefore := func(i int) bool {
		if i == 0 {
			return false
		}
		gap := rows[i].y0 - rows[i-1].y1
		h := math.Min(rows[i].y1-rows[i].y0, rows[i-1].y1-rows[i-1].y0)
		return gap > h*0.6
	}

	var lines []string
	for i := 0; i < len(rows); i++ {
		if blankBefore(i) {
			lines = append(lines, "")
		}
		r := rows[i]
		if isChord[i] && i+1 < len(rows) && !isChord[i+1] && !blankBefore(i+1) {
			lyric := placeLyric(rows[i+1], margin, charW)
			lines = append(lines,
				placeChords(r, func(x float64) int { return columnFor(lyric, x, margin, charW) }),
				strings.TrimRight(string(lyric.runes), " "))
			i++
			continue
		}
		if isChord[i] {
			lines = append(lines, placeChords(r, func(x float64) int { return int(math.Round((x - margin) / charW)) }))
			continue
		}
		lines = append(lines, strings.TrimRight(string(placeLyric(r, margin, charW).runes), " "))
	}
	return lines
}

// layoutFromBBox строит текст всего документа по выводу `pdftotext -bbox-layout`.
func layoutFromBBox(data []byte) (string, error) {
	pages, err := parseBBox(data)
	if err != nil {
		return "", err
	}
	var all []string
	for i, p := range pages {
		if i > 0 && len(all) > 0 {
			all = append(all, "")
		}
		for ci, col := range splitColumns(p) {
			if ci > 0 {
				all = append(all, "")
			}
			all = append(all, layoutPage(col)...)
		}
	}
	return strings.Join(all, "\n"), nil
}

// splitColumns делит страницу на колонки, если текст набран в несколько колонок:
// ищет вертикальную полосу, которую не пересекает ни одно слово.
func splitColumns(words []word) [][]word {
	if len(words) == 0 {
		return nil
	}
	var widths []float64
	minX, maxX := math.Inf(1), math.Inf(-1)
	for _, w := range words {
		if n := len([]rune(w.text)); n > 1 {
			widths = append(widths, (w.x1-w.x0)/float64(n))
		}
		minX, maxX = math.Min(minX, w.x0), math.Max(maxX, w.x1)
	}
	charW := 6.0
	if len(widths) > 0 {
		sort.Float64s(widths)
		charW = widths[len(widths)/2]
	}

	// Объединяем горизонтальные отрезки слов и ищем широкий промежуток в средней части страницы.
	ws := append([]word(nil), words...)
	sort.Slice(ws, func(i, j int) bool { return ws[i].x0 < ws[j].x0 })
	gutter := -1.0
	end := ws[0].x1
	for _, w := range ws[1:] {
		if w.x0-end > charW*3 {
			mid := (w.x0 + end) / 2
			if mid > minX+(maxX-minX)*0.25 && mid < minX+(maxX-minX)*0.75 {
				gutter = mid
				break
			}
		}
		end = math.Max(end, w.x1)
	}
	if gutter < 0 {
		return [][]word{words}
	}

	var left, right []word
	for _, w := range words {
		if w.x0 < gutter {
			left = append(left, w)
		} else {
			right = append(right, w)
		}
	}
	// Колонка справа тоже может делиться дальше.
	return append([][]word{left}, splitColumns(right)...)
}
