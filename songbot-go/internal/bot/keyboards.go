package bot

import (
	"fmt"

	tg "songbot/internal/telegram"
)

const songsPerPage = 10

// mainReplyKeyboard — стартовая клавиатура (обновлённый набор эмодзи/подписей).
func mainReplyKeyboard() tg.ReplyKeyboardMarkup {
	return tg.ReplyKeyboardMarkup{
		Keyboard: [][]tg.KeyboardButton{
			{{Text: "🗂️ Каталог по алфавиту"}},
			{{Text: "🎼 Все песни"}},
		},
		ResizeKeyboard: true,
	}
}

// alphabetKeyboard строит инлайн-клавиатуру из доступных первых букв названий песен.
func alphabetKeyboard(letters []string) tg.InlineKeyboardMarkup {
	var rows [][]tg.InlineKeyboardButton
	var row []tg.InlineKeyboardButton
	for _, l := range letters {
		row = append(row, tg.InlineKeyboardButton{
			Text:         "" + l,
			CallbackData: "letter|" + l,
		})
		if len(row) == 4 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return tg.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// songsKeyboard строит список песен на заданную букву.
func songsKeyboard(titles []string, indexOf map[string]int) tg.InlineKeyboardMarkup {
	var rows [][]tg.InlineKeyboardButton
	for _, t := range titles {
		rows = append(rows, []tg.InlineKeyboardButton{
			{Text: "" + t, CallbackData: fmt.Sprintf("song|%d", indexOf[t])},
		})
	}
	return tg.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// allSongsPageKeyboard строит страницу общего списка песен с навигацией "Назад"/"Ещё".
func allSongsPageKeyboard(allTitles []string, indexOf map[string]int, start int) (tg.InlineKeyboardMarkup, int) {
	end := start + songsPerPage
	if end > len(allTitles) {
		end = len(allTitles)
	}
	page := allTitles[start:end]

	var rows [][]tg.InlineKeyboardButton
	for _, t := range page {
		rows = append(rows, []tg.InlineKeyboardButton{
			{Text: "" + t, CallbackData: fmt.Sprintf("song|%d", indexOf[t])},
		})
	}

	var nav []tg.InlineKeyboardButton
	if start > 0 {
		nav = append(nav, tg.InlineKeyboardButton{Text: "⇦ Назад", CallbackData: fmt.Sprintf("prev|%d", start)})
	}
	if end < len(allTitles) {
		nav = append(nav, tg.InlineKeyboardButton{Text: "⇨ Ещё", CallbackData: fmt.Sprintf("more|%d", end)})
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	return tg.InlineKeyboardMarkup{InlineKeyboard: rows}, end
}

// afterSongKeyboard — клавиатура после отправки песни. Кнопка "Аккорды" больше не нужна,
// т.к. файл с аккордами теперь отправляется сразу вместе с текстом песни.
func afterSongKeyboard(songIndex int) tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{{Text: "✝️ Библейский стих к песне", CallbackData: fmt.Sprintf("bible|%d", songIndex)}},
			{{Text: "🗂️ Каталог по алфавиту", CallbackData: "menu"}},
			{{Text: "🎼 Все песни", CallbackData: "all_songs"}},
		},
	}
}

// navKeyboard — простая клавиатура возврата в каталог/список песен.
func navKeyboard() tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{{Text: "🗂️ Каталог по алфавиту", CallbackData: "menu"}},
			{{Text: "🎼 Все песни", CallbackData: "all_songs"}},
		},
	}
}
