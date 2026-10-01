package chords

import "strings"

// Wrap переносит строки длиннее width символов, чтобы текст читался на экране телефона.
// Пара «строка аккордов + строка слов под ней» переносится в одном и том же столбце,
// поэтому аккорды остаются над своими слогами. Место переноса выбирается там,
// где в словах пробел, а в аккордах ничего не разрывается.
func Wrap(text string, width int) string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " ")
		if IsChordLine(line) && i+1 < len(lines) {
			next := strings.TrimRight(lines[i+1], " ")
			if strings.TrimSpace(next) != "" && !IsChordLine(next) {
				out = append(out, wrapPair([]rune(line), []rune(next), width)...)
				i++
				continue
			}
		}
		out = append(out, wrapSingle([]rune(line), width)...)
	}
	return strings.Join(out, "\n")
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

// wrapPair переносит пару «аккорды + слова».
func wrapPair(chordLine, lyric []rune, width int) []string {
	var out []string
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
		out = append(out, rtrim(slice(chordLine, 0, cut)), rtrim(slice(lyric, 0, cut)))
		chordLine, lyric = slice(chordLine, cut, len(chordLine)), slice(lyric, cut, len(lyric))
		// Убираем общий отступ, сохраняя взаимное положение аккордов и слов.
		trim := minInt(leading(chordLine), leading(lyric))
		chordLine, lyric = slice(chordLine, trim, len(chordLine)), slice(lyric, trim, len(lyric))
	}
	return append(out, rtrim(chordLine), rtrim(lyric))
}

// wrapSingle переносит одиночную строку по пробелам. Сначала пробует разрезать по широкому
// промежутку (два и больше пробела) — так название песни отделяется от аккордов,
// записанных справа от него, а не рвётся посреди "// Em - Em //".
func wrapSingle(line []rune, width int) []string {
	var out []string
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
		out = append(out, rtrim(line[:cut]))
		line = line[cut:]
		line = slice(line, leading(line), len(line))
	}
	return append(out, rtrim(line))
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
