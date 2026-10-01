// Package songs отвечает за загрузку текстов песен (.docx) и файлов аккордов (.pdf),
// а также за сопоставление аккордов с песнями по похожести имени файла —
// аналог difflib.get_close_matches из оригинального python-скрипта.
package songs

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"

	"songbot/internal/docxread"
	"songbot/internal/pdftext"
)

// Store хранит все загруженные песни и аккорды, а также стабильный отсортированный
// список названий — по нему строятся индексы для callback_data кнопок.
type Store struct {
	Titles []string          // отсортированные названия песен
	Lyrics map[string]string // название -> текст песни
	Chords map[string]string // название -> путь к pdf с аккордами
	Files  map[string]string // название -> путь к исходному .docx (для пересылки файлом)

	// ChordTexts — название -> текст с аккордами над словами (из chords/txt/*.txt).
	// Есть только у песен, чей PDF удалось перевести в текст.
	ChordTexts map[string]string
}

// ChordTextDir — папка с текстовыми версиями PDF с аккордами.
func ChordTextDir(chordsDir string) string {
	return filepath.Join(chordsDir, "txt")
}

// Load читает все .docx из songsDir и все .pdf из chordsDir и связывает их между собой.
func Load(songsDir, chordsDir string) (*Store, error) {
	lyrics := map[string]string{}
	files := map[string]string{}

	entries, err := os.ReadDir(songsDir)
	if err != nil {
		return nil, err
	}

	// .docx разбираются параллельно пулом воркеров (по числу ядер CPU).
	// Карты lyrics/files заполняет только текущая горутина, поэтому мьютекс не нужен.
	type parsed struct{ title, path, text string }
	jobs := make(chan string)
	results := make(chan parsed)

	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for full := range jobs {
				text, err := docxread.ExtractText(full)
				if err != nil {
					// Пропускаем повреждённый файл, но не роняем весь бот.
					continue
				}
				name := filepath.Base(full)
				results <- parsed{title: strings.TrimSuffix(name, filepath.Ext(name)), path: full, text: text}
			}
		}()
	}

	go func() {
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".docx") {
				continue
			}
			jobs <- filepath.Join(songsDir, e.Name())
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		lyrics[r.title] = r.text
		files[r.title] = r.path
	}

	titles := make([]string, 0, len(lyrics))
	for t := range lyrics {
		titles = append(titles, t)
	}
	sort.Strings(titles)

	chords := map[string]string{}
	if chordEntries, err := os.ReadDir(chordsDir); err == nil {
		for _, e := range chordEntries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
				continue
			}
			base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			clean := cleanForMatch(base)

			bestTitle, bestScore := "", 0.0
			for _, title := range titles {
				score := similarity(clean, cleanForMatch(title))
				if score > bestScore {
					bestScore, bestTitle = score, title
				}
			}

			full := filepath.Join(chordsDir, e.Name())
			if bestScore >= 0.6 {
				chords[bestTitle] = full
			} else {
				chords[base] = full
			}
		}
	}

	chordTexts := map[string]string{}
	for title, pdf := range chords {
		if b, err := os.ReadFile(pdftext.TxtPath(ChordTextDir(chordsDir), pdf)); err == nil {
			chordTexts[title] = string(b)
		}
	}

	return &Store{Titles: titles, Lyrics: lyrics, Chords: chords, Files: files, ChordTexts: chordTexts}, nil
}

// cleanForMatch оставляет только буквы/цифры/пробелы и переводит в нижний регистр,
// как это делал python: ”.join(c for c in name if c.isalnum() or c.isspace()).lower().
func cleanForMatch(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// similarity возвращает степень похожести двух строк в диапазоне [0,1],
// основанную на расстоянии Левенштейна — практический аналог difflib.SequenceMatcher.ratio().
func similarity(a, b string) float64 {
	if a == "" && b == "" {
		return 1
	}
	ar, br := []rune(a), []rune(b)
	dist := levenshtein(ar, br)
	maxLen := len(ar)
	if len(br) > maxLen {
		maxLen = len(br)
	}
	if maxLen == 0 {
		return 1
	}
	return 1 - float64(dist)/float64(maxLen)
}

func levenshtein(a, b []rune) int {
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			min := del
			if ins < min {
				min = ins
			}
			if sub < min {
				min = sub
			}
			curr[j] = min
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// TitlesStartingWith возвращает отсортированные названия песен, начинающиеся на заданную букву.
func (s *Store) TitlesStartingWith(letter string) []string {
	var out []string
	for _, t := range s.Titles {
		if t == "" {
			continue
		}
		first := strings.ToUpper(string([]rune(t)[0]))
		if first == letter {
			out = append(out, t)
		}
	}
	return out
}

// AvailableLetters возвращает отсортированный список первых букв всех песен.
func (s *Store) AvailableLetters() []string {
	set := map[string]bool{}
	for _, t := range s.Titles {
		r := []rune(t)
		if len(r) == 0 || !unicode.IsLetter(r[0]) {
			continue
		}
		set[strings.ToUpper(string(r[0]))] = true
	}
	letters := make([]string, 0, len(set))
	for l := range set {
		letters = append(letters, l)
	}
	sort.Strings(letters)
	return letters
}

// Search ищет песни, у которых запрос встречается в названии или в тексте (простой резервный поиск).
func (s *Store) Search(query string) []string {
	q := strings.ToLower(query)
	var out []string
	for _, t := range s.Titles {
		if strings.Contains(strings.ToLower(t), q) || strings.Contains(strings.ToLower(s.Lyrics[t]), q) {
			out = append(out, t)
		}
	}
	return out
}
