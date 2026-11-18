package chords

import "strings"

// Wrap переносит строки длиннее width символов, чтобы текст читался на экране телефона.
// Пара «строка аккордов + строка слов под ней» переносится в одном и том же столбце,
// поэтому аккорды остаются над своими слогами. Место переноса выбирается там,
// где в словах пробел, а в аккордах ничего не разрывается.
func Wrap(text string, width int) string {
	wrapped := WrapMapped(text, width)
	out := make([]string, len(wrapped))
	for i, l := range wrapped {
		out[i] = l.Text
	}
	return strings.Join(out, "\n")
}

// WrappedLine — строка после переноса и её место в исходном тексте:
// символ в столбце x этой строки — это символ в столбце Offset+x строки Src.
type WrappedLine struct {
	Text   string
	Src    int
	Offset int
}

// WrapMapped делает то же, что Wrap, но для каждой строки результата сообщает,
// из какой строки и с какого столбца исходного текста она взята.
func WrapMapped(text string, width int) []WrappedLine {
	lines := strings.Split(text, "\n")
	var out []WrappedLine
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " ")
		if IsChordLine(line) && i+1 < len(lines) {
			next := strings.TrimRight(lines[i+1], " ")
			if strings.TrimSpace(next) != "" && !IsChordLine(next) {
				out = append(out, wrapPair([]rune(line), []rune(next), width, i)...)
				i++
				continue
			}
		}
		out = append(out, wrapSingle([]rune(line), width, i)...)
	}
	return out
}

func at(r []rune, i int) rune {
	if i < len(r) {
		return r[i]
	}
	return ' '
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wrapPair переносит пару «аккорды + слова» (строки src и src+1).
func wrapPair(chordLine, lyric []rune, width, src int) []WrappedLine {
	var out []WrappedLine
	offset := 0
	for maxInt(len(chordLine), len(lyric)) > width {
		cut := -1
		// Ищем самый правый пробел в словах, в котором не разрывается аккорд.
		for c := width; c > width/3; c-- {
			if at(lyric, c) == ' ' && at(chordLine, c) == ' ' {
				cut = c
				break
			}
		}
		// Если подходящего пробела нет — режем хотя бы не посреди аккорда.
		if cut < 0 {
			for c := width; c > 0; c-- {
				if at(chordLine, c) == ' ' {
					cut = c
					break
				}
			}
		}
		if cut <= 0 {
			break
		}
		out = append(out,
			WrappedLine{rtrim(slice(chordLine, 0, cut)), src, offset},
			WrappedLine{rtrim(slice(lyric, 0, cut)), src + 1, offset})
		chordLine, lyric = slice(chordLine, cut, len(chordLine)), slice(lyric, cut, len(lyric))
		// Убираем общий отступ, сохраняя взаимное положение аккордов и слов.
		trim := minInt(leading(chordLine), leading(lyric))
		chordLine, lyric = slice(chordLine, trim, len(chordLine)), slice(lyric, trim, len(lyric))
		offset += cut + minInt(trim, 1<<20)
	}
	return append(out, WrappedLine{rtrim(chordLine), src, offset}, WrappedLine{rtrim(lyric), src + 1, offset})
}

// wrapSingle переносит одиночную строку по пробелам. Сначала пробует разрезать по широкому
// промежутку (два и больше пробела) — так название песни отделяется от аккордов,
// записанных справа от него, а не рвётся посреди "// Em - Em //".
func wrapSingle(line []rune, width, src int) []WrappedLine {
	var out []WrappedLine
	offset := 0
	for len(line) > width {
		cut := -1
		for c := width; c > 0; c-- {
			if line[c] == ' ' && line[c-1] == ' ' {
				cut = c
				break
			}
		}
		if cut < 0 {
			for c := width; c > 0; c-- {
				if line[c] == ' ' {
					cut = c
					break
				}
			}
		}
		if cut <= 0 {
			break
		}
		out = append(out, WrappedLine{rtrim(line[:cut]), src, offset})
		line = line[cut:]
		lead := minInt(leading(line), len(line))
		line = line[lead:]
		offset += cut + lead
	}
	return append(out, WrappedLine{rtrim(line), src, offset})
}

func slice(r []rune, from, to int) []rune {
	if from > len(r) {
		return nil
	}
	if to > len(r) {
		to = len(r)
	}
	return r[from:to]
}

func leading(r []rune) int {
	n := 0
	for n < len(r) && r[n] == ' ' {
		n++
	}
	if n == len(r) {
		return 1 << 30 // пустая строка не ограничивает общий отступ
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func rtrim(r []rune) string {
	return strings.TrimRight(string(r), " ")
}
