package chords

import (
	"strings"
	"testing"
)

const movesSample = "Em          Am         Hm\n" +
	"Благо есть славить Господа, и петь имени Твоему"

func TestWithMovesShiftsChord(t *testing.T) {
	s := Parse(movesSample, "")
	// Am — второй токен первой строки; сдвигаем на 3 вправо, Em — на 2 влево (упрётся в 0).
	got := strings.Split(s.WithMoves(map[ChordID]int{{0, 1}: 3, {0, 0}: -2}).Transpose(0), "\n")[0]
	if got != "Em             Am      Hm" {
		t.Fatalf("получено %q", got)
	}
	// Влево до соседа: между аккордами остаётся пробел.
	got = strings.Split(s.WithMoves(map[ChordID]int{{0, 1}: -50}).Transpose(0), "\n")[0]
	if got != "Em Am"+strings.Repeat(" ", 18)+"Hm" { // Hm остаётся в своём столбце 23
		t.Fatalf("получено %q", got)
	}
	// Строка со словами не меняется.
	if got := strings.Split(s.WithMoves(map[ChordID]int{{1, 0}: 5}).Transpose(0), "\n")[1]; !strings.HasPrefix(got, "Благо") {
		t.Fatalf("строка со словами изменилась: %q", got)
	}
}

func TestRenderIDsSurviveWrapAndTranspose(t *testing.T) {
	s := Parse(movesSample, "")
	lines := s.Render(2, 20)
	var ids []ChordID
	var names []string
	for _, l := range lines {
		r := []rune(l.Text)
		for _, sp := range l.Spans {
			if sp.ID == nil {
				t.Fatalf("у аккорда в строке аккордов нет адреса: %+v", l)
			}
			ids = append(ids, *sp.ID)
			names = append(names, string(r[sp.Start:sp.End]))
		}
	}
	want := []ChordID{{0, 0}, {0, 1}, {0, 2}}
	if len(ids) != 3 || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("адреса %v, аккорды %v", ids, names)
	}
	if strings.Join(names, " ") != "F#m Hm C#m" {
		t.Fatalf("аккорды %v", names)
	}
}
