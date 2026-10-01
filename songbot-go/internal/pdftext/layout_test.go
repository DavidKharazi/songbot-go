package pdftext

import (
	"strings"
	"testing"
)

// Фрагмент вывода `pdftotext -bbox-layout` для «Дружба верная, радость полная»:
// аккорды набраны другим шрифтом, поэтому их столбцы нельзя считать по средней ширине символа.
const bboxSample = `<html><body><doc><page width="595" height="842"><flow><block>
<line><word xMin="96.49" yMin="155.94" xMax="101.61" yMax="167.64">E</word></line>
<line><word xMin="138.81" yMin="155.94" xMax="162.35" yMax="167.64">C#m</word></line>
<line><word xMin="223.57" yMin="155.94" xMax="230.64" yMax="167.64">A</word></line>
<line><word xMin="262.33" yMin="155.94" xMax="267.82" yMax="167.64">B</word></line>
<line>
<word xMin="93.84" yMin="169.50" xMax="131.32" yMax="183.61">Только</word>
<word xMin="134.41" yMin="169.50" xMax="155.78" yMax="183.61">под</word>
<word xMin="159.01" yMin="169.50" xMax="193.39" yMax="183.61">рукой</word>
<word xMin="196.61" yMin="169.50" xMax="271.37" yMax="183.61">Всевышнего.</word>
</line>
</block></flow></page></doc></body></html>`

func TestLayoutPutsChordsOverLetters(t *testing.T) {
	text, err := layoutFromBBox([]byte(bboxSample))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		t.Fatalf("ожидалось 2 строки, получено:\n%s", text)
	}
	lyric := []rune(lines[1])
	// В PDF A стоит над «ы» в «Всевышнего» (x=223.6), C#m — над «о» в «под» (x=138.8).
	for chord, letter := range map[string]rune{"A": 'ы', "C#m": 'о'} {
		col := len([]rune(lines[0][:strings.Index(lines[0], chord)]))
		if lyric[col] != letter {
			t.Errorf("%s стоит над %q, ожидалось над %q:\n%s", chord, lyric[col], letter, text)
		}
	}
}

func TestSplitColumns(t *testing.T) {
	words := []word{
		{10, 10, 60, 20, "левая"}, {10, 30, 60, 40, "колонка"},
		{300, 10, 350, 20, "правая"}, {300, 30, 350, 40, "колонка"},
	}
	cols := splitColumns(words)
	if len(cols) != 2 || len(cols[0]) != 2 || len(cols[1]) != 2 {
		t.Fatalf("ожидались 2 колонки по 2 слова, получено %v", cols)
	}
}
