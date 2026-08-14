// Package docxread извлекает текст абзацев из .docx файлов.
// .docx — это zip-архив с XML внутри, поэтому сторонние библиотеки не нужны:
// используются только archive/zip и encoding/xml из стандартной библиотеки.
package docxread

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ExtractText открывает .docx файл и возвращает его текст, абзац за абзацем,
// разделённые переводом строки — аналогично join([p.text for p in doc.paragraphs]) в python-docx.
func ExtractText(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("не удалось открыть %s: %w", path, err)
	}
	defer r.Close()

	var docXML *zip.File
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			docXML = f
			break
		}
	}
	if docXML == nil {
		return "", fmt.Errorf("в %s не найден word/document.xml — это не docx?", path)
	}

	rc, err := docXML.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	return parseParagraphs(data)
}

// parseParagraphs проходит по XML-токенам document.xml и собирает текст по абзацам <w:p>,
// внутри которых текстовые фрагменты лежат в <w:t>, а переносы — в <w:br>/<w:tab>.
func parseParagraphs(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var paragraphs []string
	var current strings.Builder
	inText := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Некоторые docx содержат нестандартные сущности; не валим весь разбор,
			// а просто останавливаемся на том, что успели собрать.
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "br", "tab", "cr":
				current.WriteString(" ")
			}
		case xml.CharData:
			if inText {
				current.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				paragraphs = append(paragraphs, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		paragraphs = append(paragraphs, current.String())
	}
	return strings.Join(paragraphs, "\n"), nil
}
