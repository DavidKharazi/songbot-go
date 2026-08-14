package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"songbot/internal/gemini"
	"songbot/internal/songs"
	tg "songbot/internal/telegram"
)

// Bot связывает Telegram-клиент, хранилище песен и клиент Gemini.
type Bot struct {
	tg     *tg.Bot
	store  *songs.Store
	gemini *gemini.Client

	indexOf map[string]int // название песни -> позиция в store.Titles (для компактных callback_data)

	mu    sync.Mutex
	pages map[int64]int // chatID -> текущая позиция постраничного списка "Все песни"
}

// New создаёт бота поверх уже загруженного хранилища песен.
func New(token string, store *songs.Store, geminiClient *gemini.Client) *Bot {
	indexOf := make(map[string]int, len(store.Titles))
	for i, t := range store.Titles {
		indexOf[t] = i
	}
	return &Bot{
		tg:      tg.New(token),
		store:   store,
		gemini:  geminiClient,
		indexOf: indexOf,
		pages:   map[int64]int{},
	}
}

// Run запускает бесконечный цикл long polling.
func (b *Bot) Run() error {
	var offset int64
	log.Println("Бот запущен, ожидаю обновления...")
	for {
		updates, err := b.tg.GetUpdates(offset, 60)
		if err != nil {
			log.Printf("ошибка getUpdates: %v", err)
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.dispatch(u)
		}
	}
}

func (b *Bot) dispatch(u tg.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("паника при обработке обновления: %v", r)
		}
	}()

	switch {
	case u.Message != nil:
		b.handleMessage(u.Message)
	case u.CallbackQuery != nil:
		b.handleCallback(u.CallbackQuery)
	}
}

func (b *Bot) handleMessage(m *tg.Message) {
	chatID := m.Chat.ID
	text := strings.TrimSpace(m.Text)

	switch {
	case text == "/start":
		b.sendStart(chatID)
	case text == "/menu":
		b.sendMenu(chatID)
	case text == "/all_songs":
		b.sendAllSongsPage(chatID, 0)
	// Сопоставляем по вхождению ключевой фразы, а не по точному совпадению строки —
	// так кнопки меню гарантированно не попадут в поиск через Gemini, даже если
	// клиент Telegram немного иначе отрисует эмодзи в тексте кнопки.
	case strings.Contains(text, "Каталог по алфавиту"):
		b.sendMenu(chatID)
	case strings.Contains(text, "Все песни"):
		b.sendAllSongsPage(chatID, 0)
	case text == "":
		// игнорируем пустые сообщения (стикеры, фото и т.п. без подписи)
	default:
		// Gemini вызывается ТОЛЬКО здесь: когда пользователь сам написал текстовый запрос,
		// не совпавший ни с одной из кнопок меню.
		b.handleSearch(chatID, text)
	}
}

func (b *Bot) sendStart(chatID int64) {
	kb := mainReplyKeyboard()
	_, err := b.tg.SendMessage(chatID, "Привет! 👋 Выбери действие ниже или просто напиши, какую песню ищешь:", kb)
	if err != nil {
		log.Printf("sendStart: %v", err)
	}
}

func (b *Bot) sendMenu(chatID int64) {
	kb := alphabetKeyboard(b.store.AvailableLetters())
	_, err := b.tg.SendMessage(chatID, "Выбери букву названия песни 🔤", kb)
	if err != nil {
		log.Printf("sendMenu: %v", err)
	}
}

func (b *Bot) editMenu(chatID int64, messageID int) {
	kb := alphabetKeyboard(b.store.AvailableLetters())
	if err := b.tg.EditMessageText(chatID, messageID, "Выбери букву названия песни 🔤", &kb); err != nil {
		log.Printf("editMenu: %v", err)
	}
}

func (b *Bot) sendAllSongsPage(chatID int64, start int) {
	kb, end := allSongsPageKeyboard(b.store.Titles, b.indexOf, start)
	b.mu.Lock()
	b.pages[chatID] = end
	b.mu.Unlock()
	_, err := b.tg.SendMessage(chatID, "🎼 Список песен:", kb)
	if err != nil {
		log.Printf("sendAllSongsPage: %v", err)
	}
}

func (b *Bot) editAllSongsPage(chatID int64, messageID int, start int) {
	kb, end := allSongsPageKeyboard(b.store.Titles, b.indexOf, start)
	b.mu.Lock()
	b.pages[chatID] = end
	b.mu.Unlock()
	if err := b.tg.EditMessageText(chatID, messageID, "🎼 Список песен:", &kb); err != nil {
		log.Printf("editAllSongsPage: %v", err)
	}
}

func (b *Bot) handleSearch(chatID int64, query string) {
	if _, err := b.tg.SendMessage(chatID, "🔎 Ищу песни по вашему запросу...", nil); err != nil {
		log.Printf("handleSearch notify: %v", err)
	}

	result, err := b.searchWithGemini(query)
	if err != nil {
		log.Printf("gemini search error: %v", err) // подробности только в лог, пользователю — просто и по делу
		result = "😔 Не удалось найти. Попробуйте, пожалуйста, ещё раз чуть позже."
	}

	if _, err := b.tg.SendMessage(chatID, result, nil); err != nil {
		log.Printf("handleSearch result: %v", err)
	}
	if _, err := b.tg.SendMessage(chatID, "Еще ⤵:", navKeyboard()); err != nil {
		log.Printf("handleSearch nav: %v", err)
	}
}

func (b *Bot) searchWithGemini(query string) (string, error) {
	var sb strings.Builder
	for _, title := range b.store.Titles {
		sb.WriteString("Название: ")
		sb.WriteString(title)
		sb.WriteString("\nТекст песни:\n")
		sb.WriteString(b.store.Lyrics[title])
		sb.WriteString("\n---\n")
	}

	prompt := fmt.Sprintf(`Ты — помощник для поиска песен в базе данных церковных песен.

Вот запрос пользователя: "%s"

Ниже представлены все песни из нашей базы данных:

%s

Найди песни, которые наиболее соответствуют запросу пользователя. Поиск может осуществляться по:
1. Названию песни
2. Словам из текста
3. Теме или смыслу песни
4. Библейскому контексту

Формат ответа:
1. Перечисли найденные песни, отсортированные по релевантности
2. Для каждой песни укажи название и короткое обоснование почему эта песня подходит к запросу
3. Если не найдено подходящих песен, так и скажи
4. Не используй markdown в ответе, но используй абзацы и эмодзи.

Отвечай кратко и по существу.`, query, sb.String())

	return b.gemini.Generate(prompt)
}

// sendSong отправляет текст песни, а затем сразу оба файла — текст (.docx) и аккорды (.pdf),
// если они найдены. Отдельная кнопка "Аккорды" больше не нужна.
func (b *Bot) sendSong(chatID int64, title string) {
	lyrics, ok := b.store.Lyrics[title]
	if !ok {
		if _, err := b.tg.SendMessage(chatID, "Песня не найдена.", nil); err != nil {
			log.Printf("sendSong not found: %v", err)
		}
		return
	}

	if _, err := b.tg.SendMessage(chatID, lyrics, nil); err != nil {
		log.Printf("sendSong lyrics: %v", err)
	}

	if docPath, ok := b.store.Files[title]; ok {
		if err := b.tg.SendDocument(chatID, docPath, "📄 Текст песни"); err != nil {
			log.Printf("sendSong docx: %v", err)
			if _, e := b.tg.SendMessage(chatID, fmt.Sprintf("Файл %s.docx не найден.", title), nil); e != nil {
				log.Printf("sendSong docx notify: %v", e)
			}
		}
	}

	if chordPath, ok := b.store.Chords[title]; ok {
		if err := b.tg.SendDocument(chatID, chordPath, "🎸 Аккорды"); err != nil {
			log.Printf("sendSong chords: %v", err)
		}
	} else {
		if _, err := b.tg.SendMessage(chatID, "🎸 Аккорды для этой песни не найдены.", nil); err != nil {
			log.Printf("sendSong chords notify: %v", err)
		}
	}

	idx := b.indexOf[title]
	if _, err := b.tg.SendMessage(chatID, "Еще ⤵:", afterSongKeyboard(idx)); err != nil {
		log.Printf("sendSong nav: %v", err)
	}
}

func (b *Bot) sendBibleVerse(chatID int64, title string) {
	lyrics := b.store.Lyrics[title]
	if _, err := b.tg.SendMessage(chatID, "🙏 Пожалуйста, подождите...", nil); err != nil {
		log.Printf("sendBibleVerse notify: %v", err)
	}

	guidance, verse, err := b.spiritualGuidanceAndVerse(title, lyrics)
	if err != nil {
		log.Printf("spiritualGuidanceAndVerse: %v", err) // подробности только в лог
		if _, e := b.tg.SendMessage(chatID, "😔 Не удалось найти.", nil); e != nil {
			log.Printf("sendBibleVerse fail notify: %v", e)
		}
		idx := b.indexOf[title]
		if _, e := b.tg.SendMessage(chatID, "Еще ⤵:", afterSongKeyboard(idx)); e != nil {
			log.Printf("sendBibleVerse nav: %v", e)
		}
		return
	}

	if _, err := b.tg.SendMessage(chatID, "✝️ Духовное наставление:\n\n"+guidance, nil); err != nil {
		log.Printf("sendBibleVerse guidance: %v", err)
	}
	if _, err := b.tg.SendMessage(chatID, "📖 Подходящий стих из Библии:\n\n"+verse, nil); err != nil {
		log.Printf("sendBibleVerse verse: %v", err)
	}

	idx := b.indexOf[title]
	if _, err := b.tg.SendMessage(chatID, "Еще ⤵:", afterSongKeyboard(idx)); err != nil {
		log.Printf("sendBibleVerse nav: %v", err)
	}
}

func (b *Bot) spiritualGuidanceAndVerse(title, lyrics string) (string, string, error) {
	prompt := fmt.Sprintf(`Проанализируйте следующую песню с названием '%s' и текстом:

%s

Выполните две задачи:
1. Найдите подходящий стих из Библии, который соответствует тематике этой песни
2. Напишите духовное наставление в стиле глубоких богословских размышлений

Разделите ответ на две части: сначала духовное наставление, затем библейский стих.`, title, lyrics)

	text, err := b.gemini.Generate(prompt)
	if err != nil {
		return "", "", err
	}

	parts := strings.SplitN(text, "\n\n", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	return parts[0], "Не удалось найти подходящий стих.", nil
}

func (b *Bot) handleCallback(cq *tg.CallbackQuery) {
	if err := b.tg.AnswerCallbackQuery(cq.ID); err != nil {
		log.Printf("AnswerCallbackQuery: %v", err)
	}
	if cq.Message == nil {
		return
	}
	chatID := cq.Message.Chat.ID
	msgID := cq.Message.MessageID
	data := cq.Data

	switch {
	case data == "menu":
		b.editMenu(chatID, msgID)
	case data == "all_songs":
		b.editAllSongsPage(chatID, msgID, 0)
	case strings.HasPrefix(data, "more|"):
		start := parseIntOr(strings.TrimPrefix(data, "more|"), 0)
		b.editAllSongsPage(chatID, msgID, start)
	case strings.HasPrefix(data, "prev|"):
		start := parseIntOr(strings.TrimPrefix(data, "prev|"), 0)
		newStart := start - songsPerPage
		if newStart < 0 {
			newStart = 0
		}
		b.editAllSongsPage(chatID, msgID, newStart)
	case strings.HasPrefix(data, "letter|"):
		letter := strings.TrimPrefix(data, "letter|")
		titles := b.store.TitlesStartingWith(letter)
		kb := songsKeyboard(titles, b.indexOf)
		if err := b.tg.EditMessageText(chatID, msgID, "Песни на букву "+letter+":", &kb); err != nil {
			log.Printf("edit letter keyboard: %v", err)
		}
	case strings.HasPrefix(data, "song|"):
		idx := parseIntOr(strings.TrimPrefix(data, "song|"), -1)
		title := b.titleByIndex(idx)
		if title == "" {
			return
		}
		if err := b.tg.DeleteMessage(chatID, msgID); err != nil {
			log.Printf("delete song list message: %v", err)
		}
		b.sendSong(chatID, title)
	case strings.HasPrefix(data, "bible|"):
		idx := parseIntOr(strings.TrimPrefix(data, "bible|"), -1)
		title := b.titleByIndex(idx)
		if title == "" {
			return
		}
		b.sendBibleVerse(chatID, title)
	}
}

func (b *Bot) titleByIndex(idx int) string {
	if idx < 0 || idx >= len(b.store.Titles) {
		return ""
	}
	return b.store.Titles[idx]
}

func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}