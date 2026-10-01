package chords

import (
	"strings"
	"testing"
)

func TestTransposeKeepsAlignment(t *testing.T) {
	text := "Em                   Am       Hm\n" +
		"Благо есть славить Господа, и петь"
	s := Parse(text, "")
	if got := s.KeyName(0); got != "Em" {
		t.Fatalf("исходная тональность = %q, ожидалась Em", got)
	}
	got := s.Transpose(2)
	want := "F#m                  Hm       C#m\n" +
		"Благо есть славить Господа, и петь"
	if got != want {
		t.Fatalf("получено:\n%s\nожидалось:\n%s", got, want)
	}
}

func TestGermanNotation(t *testing.T) {
	// В песне есть H — значит, B это си-бемоль, а результат пишется через H/B.
	s := Parse("G   D/F#   Em   H7\nслова", "")
	if got := strings.Split(s.Transpose(2), "\n")[0]; got != "A   E/G#   F#m  C#7" {
		t.Fatalf("got %q", got)
	}
	s = Parse("C   G/H   Am\nслова", "")
	if got := strings.Split(s.Transpose(-2), "\n")[0]; got != "Bb  F/A   Gm" {
		t.Fatalf("got %q", got)
	}
}

func TestEnglishFlatsInFlatKey(t *testing.T) {
	// Из C в F английской нотации: си-бемоль пишется Bb, а не A#.
	s := Parse("C   F   G7\nслова", "")
	if got := strings.Split(s.Transpose(5), "\n")[0]; got != "F   Bb  C7" {
		t.Fatalf("got %q", got)
	}
}

func TestCompoundTokensAndLabels(t *testing.T) {
	cases := map[string]string{
		"Вступление: C Dm | Am G | - 2x": "Вступление: D Em | Hm A | - 2x",
		"Em-D-C   (F#) CGC":              "F#m-E-D  (G#) DAD",
		"Любовь Христа велика A E H E":   "Любовь Христа велика H F# C# F#",
		"Тональность C":                  "Тональность D",
		"Название  // Am - C/E - F //":   "Название  // Hm - D/F# - G //",
		"F# /D - Asus":                   "G# /E - Hsus",
	}
	for in, want := range cases {
		// Первая строка задаёт тональность C и немецкую нотацию (есть H), чтобы результат
		// (тональность D, диезы) был однозначным.
		s := Parse("C G/H\n"+in, "")
		got := strings.Split(s.Transpose(2), "\n")[1]
		if got != want {
			t.Errorf("%q: получено %q, ожидалось %q", in, got, want)
		}
	}
}

func TestLyricsUntouched(t *testing.T) {
	text := "А я пою\nВ сердце моём С Тобой"
	if got := Parse(text, "").Transpose(3); got != text {
		t.Fatalf("текст изменился: %q", got)
	}
}

func TestKeyFromFileName(t *testing.T) {
	if got := Parse("G C D\nслова", "О благодать (D)").KeyName(0); got != "D" {
		t.Fatalf("got %q", got)
	}
	if got := Parse("G C D\nслова", "Буду тверд в обетованиях E").KeyName(0); got != "E" {
		t.Fatalf("got %q", got)
	}
}

func TestWrapKeepsChordsAboveWords(t *testing.T) {
	text := "C                    Am           F            G\n" +
		"Братья все, ликуйте: славный день настал, радость нам дана!"
	got := Wrap(text, 30)
	for _, l := range strings.Split(got, "\n") {
		if len([]rune(l)) > 30 {
			t.Fatalf("строка длиннее 30: %q\n%s", l, got)
		}
	}
	want := "C                    Am\n" +
		"Братья все, ликуйте: славный\n" +
		"     F            G\n" +
		"день настал, радость нам дана!"
	if got != want {
		t.Fatalf("получено:\n%s\nожидалось:\n%s", got, want)
	}
}

func TestWrapSplitsTitleFromChords(t *testing.T) {
	got := Wrap("Благо есть славить Господа     // Em - Em //", 36)
	want := "Благо есть славить Господа\n// Em - Em //"
	if got != want {
		t.Fatalf("получено %q, ожидалось %q", got, want)
	}
}
