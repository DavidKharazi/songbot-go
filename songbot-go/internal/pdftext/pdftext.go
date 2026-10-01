// Package pdftext превращает PDF с аккордами в текстовые файлы, с которыми бот
// умеет работать (транспонировать, присылать текстом).
//
// Обычные PDF (с настоящим текстом внутри) разбираются утилитой pdftotext из пакета
// poppler-utils в режиме -layout, который сохраняет выравнивание аккордов над словами.
// Если в PDF текста нет (это скан-картинка), файл распознаётся через OCR (Gemini).
//
// Результат кэшируется в txtDir: если .txt уже есть, PDF повторно не разбирается.
// Так ручные правки в .txt не затираются; чтобы пересоздать текст, удалите .txt.
package pdftext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"songbot/internal/chords"
)

// OCR распознаёт текст с аккордами из PDF-скана.
type OCR interface {
	ExtractChordsFromPDF(ctx context.Context, pdf []byte) (string, error)
}

// TxtPath возвращает путь к текстовому файлу для данного PDF.
func TxtPath(txtDir, pdfPath string) string {
	base := strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))
	return filepath.Join(txtDir, base+".txt")
}

// Convert создаёт .txt для всех PDF из pdfDir, у которых его ещё нет.
// Ошибки отдельных файлов только логируются: бот должен запуститься в любом случае.
func Convert(ctx context.Context, pdfDir, txtDir string, ocr OCR) error {
	entries, err := os.ReadDir(pdfDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(txtDir, 0o755); err != nil {
		return err
	}

	var todo []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
			continue
		}
		pdf := filepath.Join(pdfDir, e.Name())
		if _, err := os.Stat(TxtPath(txtDir, pdf)); err == nil {
			continue // уже сконвертирован (или поправлен вручную)
		}
		todo = append(todo, pdf)
	}
	if len(todo) == 0 {
		return nil
	}

	_, lookErr := exec.LookPath("pdftotext")
	if lookErr != nil {
		log.Printf("pdftotext не найден (установите poppler-utils) — %d PDF не будут переведены в текст", len(todo))
		return nil
	}
	log.Printf("перевожу в текст PDF с аккордами: %d шт.", len(todo))

	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pdf := range jobs {
				if err := convertOne(ctx, pdf, TxtPath(txtDir, pdf), ocr); err != nil {
					log.Printf("не удалось перевести %s в текст: %v", filepath.Base(pdf), err)
				}
			}
		}()
	}
	for _, pdf := range todo {
		jobs <- pdf
	}
	close(jobs)
	wg.Wait()
	return nil
}

func convertOne(ctx context.Context, pdf, txt string, ocr OCR) error {
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", pdf, "-")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pdftotext: %w", err)
	}
	text := Clean(stdout.String())

	if len([]rune(strings.Join(strings.Fields(text), ""))) < 20 {
		// Текста почти нет — значит, это скан. Пробуем распознать.
		if ocr == nil {
			return errors.New("PDF без текстового слоя, а OCR не настроен")
		}
		data, err := os.ReadFile(pdf)
		if err != nil {
			return err
		}
		log.Printf("распознаю скан через Gemini: %s", filepath.Base(pdf))
		text, err = ocr.ExtractChordsFromPDF(ctx, data)
		if err != nil {
			return fmt.Errorf("OCR: %w", err)
		}
		text = Clean(text)
		if text == "" {
			return errors.New("OCR вернул пустой текст")
		}
	}
	return os.WriteFile(txt, []byte(text+"\n"), 0o644)
}

var (
	fenceRe     = regexp.MustCompile("(?m)^```[a-z]*\\s*$")
	manyBlankRe = regexp.MustCompile(`\n{3,}`)
)

// Clean приводит текст к единому виду: без невидимых символов, табов, разрывов страниц,
// хвостовых пробелов и лишних пустых строк.
func Clean(text string) string {
	text = fenceRe.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\f", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	text = strings.ReplaceAll(text, " ", " ")
	text = chords.StripFormatChars(text)

	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	text = strings.Join(lines, "\n")
	text = manyBlankRe.ReplaceAllString(text, "\n\n")
	return strings.Trim(text, "\n")
}
