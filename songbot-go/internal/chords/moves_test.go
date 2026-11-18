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
	got := strings.Split(s.WithMoves(map[ChordID]Move{{0, 1}: {3, -1}, {0, 0}: {-2, -1}}).Transpose(0), "\n")[0]
	if got != "Em             Am      Hm" {
		t.Fatalf("получено %q", got)
	}
	// Влево за соседа: аккорды меняются местами, между ними остаётся пробел.
	got = strings.Split(s.WithMoves(map[ChordID]Move{{0, 1}: {-50, -1}}).Transpose(0), "\n")[0]
	if got != "Am Em"+strings.Repeat(" ", 18)+"Hm" { // Hm остаётся в своём столбце 23
		t.Fatalf("получено %q", got)
	}
	// Строка со словами не меняется.
	if got := strings.Split(s.WithMoves(map[ChordID]Move{{1, 0}: {5, -1}}).Transpose(0), "\n")[1]; !strings.HasPrefix(got, "Благо") {
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

func TestMoveToAnotherLine(t *testing.T) {
	text := "Em          Am\n" +
		"Благо есть славить Господа\n" +
		"C        D\n" +
		"и петь имени Твоему"
	s := Parse(text, "")
	// Am (строка 0, токен 1, столбец 12) переносим во вторую строку аккордов, в столбец 4.
	moved := s.WithMoves(map[ChordID]Move{{0, 1}: {4 - 12, 2}})
	lines := strings.Split(moved.Transpose(0), "\n")
	if lines[0] != "Em" || lines[2] != "C   Am   D" {
		t.Fatalf("получено %q", lines)
	}
	// Перенесённый аккорд сохраняет свой адрес — его можно двигать дальше.
	r := moved.Render(0, 0)
	if sp := r[2].Spans[1]; sp.ID == nil || *sp.ID != (ChordID{0, 1}) || sp.OrigCol != 12 {
		t.Fatalf("адрес перенесённого аккорда: %+v", sp)
	}
	if !r[0].ChordLine || r[1].ChordLine {
		t.Fatalf("строки аккордов отмечены неверно")
	}
	// В строку со словами перенести нельзя — аккорд остаётся на месте.
	if got := strings.Split(s.WithMoves(map[ChordID]Move{{0, 1}: {0, 1}}).Transpose(0), "\n")[0]; got != "Em          Am" {
		t.Fatalf("получено %q", got)
	}
}

func TestEditReplaceDeleteAdd(t *testing.T) {
	s := Parse(movesSample, "") // "Em          Am         Hm"
	first := func(s *Song, shift int) string { return strings.Split(s.Transpose(shift), "\n")[0] }

	// Замена: пользователь смотрел в тональности +2 (F#m) и ввёл "C#7" вместо C#m (бывший Hm).
	// В исходной тональности это H7, а при +2 снова C#7.
	ed, bad := s.WithEdits(map[ChordID]Edit{{0, 2}: {Text: "C#7", At: 2, Line: -1}})
	if len(bad) != 0 || first(ed, 0) != "Em          Am         H7" || first(ed, 2) != "F#m         Hm         C#7" {
		t.Fatalf("замена: %q / %q (bad %v)", first(ed, 0), first(ed, 2), bad)
	}

	// Удаление.
	ed, _ = s.WithEdits(map[ChordID]Edit{{0, 1}: {Delete: true, Line: -1}})
	if first(ed, 0) != "Em"+strings.Repeat(" ", 21)+"Hm" {
		t.Fatalf("удаление: %q", first(ed, 0))
	}

	// Добавление: новый аккорд D в столбец 6 строки 0; у него свой адрес, его можно двигать.
	ed, _ = s.WithEdits(map[ChordID]Edit{{NewChordLine, 1}: {Line: 0, Delta: 6, Text: "D"}})
	if first(ed, 0) != "Em    D     Am         Hm" {
		t.Fatalf("добавление: %q", first(ed, 0))
	}
	if sp := ed.Render(0, 0)[0].Spans[1]; sp.ID == nil || *sp.ID != (ChordID{NewChordLine, 1}) || sp.OrigCol != 0 {
		t.Fatalf("адрес нового аккорда: %+v", sp)
	}

	// Не аккорд — правка не применяется и возвращается как ошибочная.
	ed, bad = s.WithEdits(map[ChordID]Edit{{0, 0}: {Text: "Привет", Line: -1}, {NewChordLine, 2}: {Line: 0, Text: "xyz"}})
	if len(bad) != 2 || first(ed, 0) != "Em          Am         Hm" {
		t.Fatalf("ошибочные правки: %q bad=%v", first(ed, 0), bad)
	}
}

func TestLayoutLinePinnedExact(t *testing.T) {
	// Неподвижные аккорды в столбцах 0, 4, 8; перетащенный (длина 3) — точно в столбец 3,
	// поверх второго. Он встаёт ровно на 3, а соседи раздвигаются, сохраняя порядок.
	got := layoutLine([]lineItem{
		{n: 2, want: 0}, {n: 2, want: 4}, {n: 2, want: 8},
		{n: 3, want: 3, pinned: true, order: 1},
	})
	pos := map[int]int{}
	for _, p := range got {
		pos[p.idx] = p.start
	}
	if pos[3] != 3 {
		t.Fatalf("перетащенный аккорд не на своём месте: %v", got)
	}
	if !(pos[0] < pos[1] && pos[1] < pos[2]) {
		t.Fatalf("неподвижные аккорды поменялись местами: %v", got)
	}
	for i := range got {
		for j := range got {
			a, b := got[i], got[j]
			if i != j && a.start <= b.start && a.start+[]int{2, 2, 2, 3}[a.idx] >= b.start {
				t.Fatalf("аккорды налезают или без пробела: %v", got)
			}
		}
	}
}

func TestLayoutLineNewestPinnedWins(t *testing.T) {
	// Два перетащенных аккорда хотят столбец 5 — его получает перетащенный последним.
	got := layoutLine([]lineItem{
		{n: 2, want: 5, pinned: true, order: 10},
		{n: 2, want: 5, pinned: true, order: 20},
	})
	for _, p := range got {
		if p.idx == 1 && p.start != 5 {
			t.Fatalf("последний перетащенный должен стоять на 5: %v", got)
		}
	}
}
