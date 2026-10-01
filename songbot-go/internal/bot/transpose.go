package bot

import (
	"fmt"
	"html"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"songbot/internal/chords"
	tg "songbot/internal/telegram"
)

// chordsWidth — ширина строки, после которой текст с аккордами переносится.
// Подобрана так, чтобы моноширинный блок не ломался на экране телефона.
const chordsWidth = 36

// telegramLimit — максимальная длина сообщения в Telegram (с запасом).
const telegramLimit = 4000

// parseChordSongs разбирает тексты с аккордами всех песен один раз при старте.
func parseChordSongs(texts map[string]string, pdfPaths map[string]string) map[string]*chords.Song {
	out := make(map[string]*chords.Song, len(texts))
	for title, text := range texts {
		base := strings.TrimSuffix(filepath.Base(pdfPaths[title]), filepath.Ext(pdfPaths[title]))
		out[title] = chords.Parse(text, base)
	}
	return out
}

// renderChords готовит сообщение с аккордами в тональности, сдвинутой на shift полутонов.
func renderChords(title string, song *chords.Song, shift int) string {
	shift = ((shift % 12) + 12) % 12
	display := shift // для подписи показываем сдвиг в диапазоне -5..+6
	if display > 6 {
		display -= 12
	}

	var sb strings.Builder
	sb.WriteString("🎸 <b>" + html.EscapeString(title) + "</b>\n")
	switch {
	case song.HasKey && shift == 0:
		fmt.Fprintf(&sb, "Тональность: <b>%s</b> (исходная)\n", song.KeyName(0))
	case song.HasKey:
		fmt.Fprintf(&sb, "Тональность: <b>%s</b> (исходная %s, %+d)\n", song.KeyName(shift), song.KeyName(0), display)
	case shift != 0:
		fmt.Fprintf(&sb, "Сдвиг: %+d полутон(а)\n", display)
	}
	if shift >= 1 && shift <= 7 {
		orig := "исходной тональности"
		if song.HasKey {
			orig = song.KeyName(0)
		}
		fmt.Fprintf(&sb, "<i>или каподастр на %d лад и аккорды из %s</i>\n", shift, orig)
	}

	body := chords.Wrap(song.Transpose(shift), chordsWidth)
	// Не выходим за лимит Telegram: при необходимости обрезаем хвост.
	room := telegramLimit - len([]rune(sb.String())) - 20
	if r := []rune(body); len(r) > room {
		body = string(r[:room]) + "\n…"
	}
	sb.WriteString("<pre>" + html.EscapeString(body) + "</pre>")
	return sb.String()
}

// transposeKeyboard — кнопки сдвига на полтона и выбора тональности.
// callback_data: "tr|<индекс песни>|<сдвиг 0..11>".
func transposeKeyboard(songIndex int, song *chords.Song, shift int) tg.InlineKeyboardMarkup {
	shift = ((shift % 12) + 12) % 12
	cb := func(s int) string { return fmt.Sprintf("tr|%d|%d", songIndex, ((s%12)+12)%12) }

	rows := [][]tg.InlineKeyboardButton{{
		{Text: "➖ ½ тона", CallbackData: cb(shift - 1)},
		{Text: "↺ Исходная", CallbackData: cb(0)},
		{Text: "➕ ½ тона", CallbackData: cb(shift + 1)},
	}}

	if song.HasKey {
		// 12 тональностей по порядку нот от до, по 6 в ряд.
		type key struct {
			note, shift int
			name        string
		}
		keys := make([]key, 12)
		for s := 0; s < 12; s++ {
			keys[s] = key{note: (song.Key.Root + s) % 12, shift: s, name: song.KeyName(s)}
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].note < keys[j].note })
		var row []tg.InlineKeyboardButton
		for _, k := range keys {
			text := k.name
			if k.shift == shift {
				text = "• " + text
			}
			row = append(row, tg.InlineKeyboardButton{Text: text, CallbackData: cb(k.shift)})
			if len(row) == 6 {
				rows = append(rows, row)
				row = nil
			}
		}
	}
	return tg.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// sendChords присылает новое сообщение с аккордами в исходной тональности.
func (b *Bot) sendChords(chatID int64, idx int) {
	title := b.titleByIndex(idx)
	song, ok := b.chordSongs[title]
	if !ok {
		if _, err := b.tg.SendMessage(chatID, "🎸 Для этой песни нет аккордов в текстовом виде.", nil); err != nil {
			log.Printf("sendChords not found: %v", err)
		}
		return
	}
	kb := transposeKeyboard(idx, song, 0)
	if _, err := b.tg.SendMessageHTML(chatID, renderChords(title, song, 0), kb); err != nil {
		log.Printf("sendChords: %v", err)
	}
}

// editChords перерисовывает то же сообщение в новой тональности — без спама в чате.
func (b *Bot) editChords(chatID int64, msgID, idx, shift int) {
	title := b.titleByIndex(idx)
	song, ok := b.chordSongs[title]
	if !ok {
		return
	}
	kb := transposeKeyboard(idx, song, shift)
	err := b.tg.EditMessageTextHTML(chatID, msgID, renderChords(title, song, shift), &kb)
	// Повторное нажатие на текущую тональность — не ошибка.
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		log.Printf("editChords: %v", err)
	}
}
