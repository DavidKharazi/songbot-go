package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"songbot/internal/chords"
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

	catalog string // все песни одной строкой для запроса к Gemini (собирается один раз)

	chordSongs map[string]*chords.Song // название -> разобранный текст с аккордами (для транспонирования)

	mu    sync.Mutex
	pages map[int64]int // chatID -> текущая позиция постраничного списка "Все песни"

	sem       chan struct{}  // ограничивает число одновременно обрабатываемых обновлений
	chatLocks sync.Map       // chatID -> *sync.Mutex: обновления одного чата обрабатываются по одному
	fileIDs   sync.Map       // путь к файлу -> file_id в Telegram (чтобы не загружать файл повторно)
	wg        sync.WaitGroup // незавершённые обработчики — ждём их при остановке
}

// maxConcurrentUpdates — сколько обновлений (от разных пользователей) обрабатывается одновременно.
const maxConcurrentUpdates = 32

// New создаёт бота поверх уже загруженного хранилища песен.
func New(token string, store *songs.Store, geminiClient *gemini.Client) *Bot {
	indexOf := make(map[string]int, len(store.Titles))
	for i, t := range store.Titles {
		indexOf[t] = i
	}
	return &Bot{
		tg:         tg.New(token),
		store:      store,
		gemini:     geminiClient,
		indexOf:    indexOf,
		catalog:    buildCatalog(store),
		chordSongs: parseChordSongs(store.ChordTexts, store.Chords),
		pages:      map[int64]int{},
		sem:        make(chan struct{}, maxConcurrentUpdates),
	}
}

// buildCatalog склеивает названия и тексты всех песен для промпта Gemini.
func buildCatalog(store *songs.Store) string {
	var sb strings.Builder
	for _, title := range store.Titles {
		sb.WriteString("Название: ")
		sb.WriteString(title)
		sb.WriteString("\nТекст песни:\n")
		sb.WriteString(store.Lyrics[title])
		sb.WriteString("\n---\n")
	}
	return sb.String()
}

// Run запускает цикл long polling. Каждое обновление обрабатывается в отдельной горутине,
// поэтому долгий запрос к Gemini одного пользователя не задерживает остальных.
// При отмене ctx новые обновления больше не принимаются, а Run дожидается
// завершения уже запущенных обработчиков.
func (b *Bot) Run(ctx context.Context) error {
	var offset int64
	log.Println("Бот запущен, ожидаю обновления...")
	for {
		updates, err := b.tg.GetUpdates(ctx, offset, 60)
		if ctx.Err() != nil {
			log.Println("остановка: жду завершения обработчиков...")
			b.wg.Wait()
			// Подтверждаем Telegram уже обработанные обновления, иначе после перезапуска
			// бот получит их повторно и ответит пользователям второй раз.
			confirmCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := b.tg.GetUpdates(confirmCtx, offset, 0)
			cancel()
			if err != nil {
				log.Printf("не удалось подтвердить обновления при остановке: %v", err)
			}
			return nil
		}
		if err != nil {
			log.Printf("ошибка getUpdates: %v", err)
			time.Sleep(time.Second) // не крутим цикл вхолостую, если нет сети
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.sem <- struct{}{} // если все слоты заняты — ждём, пока какой-то обработчик освободится
			b.wg.Add(1)
			go func(u tg.Update) {
				defer b.wg.Done()
				defer func() { <-b.sem }()
				b.dispatch(u)
			}(u)
		}
	}
}

// chatLock возвращает мьютекс чата: пока обрабатывается одно обновление пользователя,
// следующее от него же ждёт — так ответы в одном чате не перемешиваются.
func (b *Bot) chatLock(chatID int64) *sync.Mutex {
	m, _ := b.chatLocks.LoadOrStore(chatID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (b *Bot) dispatch(u tg.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("паника при обработке обновления: %v", r)
		}
	}()

	switch {
	case u.Message != nil:
		l := b.chatLock(u.Message.Chat.ID)
		l.Lock()
		defer l.Unlock()
		b.handleMessage(u.Message)
	case u.CallbackQuery != nil:
		cq := u.CallbackQuery
		// Отвечаем на нажатие сразу, не дожидаясь блокировки чата, — иначе у пользователя
		// будут крутиться «часики» на кнопке, пока идёт предыдущий запрос к Gemini.
		if err := b.tg.AnswerCallbackQuery(cq.ID); err != nil {
			log.Printf("AnswerCallbackQuery: %v", err)
		}
		if cq.Message == nil {
			return
		}
		l := b.chatLock(cq.Message.Chat.ID)
		l.Lock()
		defer l.Unlock()
		b.handleCallback(cq)
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

Отвечай кратко и по существу.`, query, b.catalog)

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
		if err := b.sendFile(chatID, docPath, "📄 Текст песни"); err != nil {
			log.Printf("sendSong docx: %v", err)
			if _, e := b.tg.SendMessage(chatID, fmt.Sprintf("Файл %s.docx не найден.", title), nil); e != nil {
				log.Printf("sendSong docx notify: %v", e)
			}
		}
	}

	if chordPath, ok := b.store.Chords[title]; ok {
		if err := b.sendFile(chatID, chordPath, "🎸 Аккорды"); err != nil {
			log.Printf("sendSong chords: %v", err)
		}
	} else {
		if _, err := b.tg.SendMessage(chatID, "🎸 Аккорды для этой песни не найдены.", nil); err != nil {
			log.Printf("sendSong chords notify: %v", err)
		}
	}

	idx := b.indexOf[title]
	if _, err := b.tg.SendMessage(chatID, "Еще ⤵:", b.afterSongKeyboard(idx)); err != nil {
		log.Printf("sendSong nav: %v", err)
	}
}

// sendFile отправляет файл с диска. После первой загрузки Telegram возвращает file_id,
// и все последующие отправки того же файла идут по нему — без повторной загрузки.
func (b *Bot) sendFile(chatID int64, path, caption string) error {
	if id, ok := b.fileIDs.Load(path); ok {
		err := b.tg.SendDocumentByFileID(chatID, id.(string), caption)
		if err == nil {
			return nil
		}
		log.Printf("отправка по file_id не удалась, загружаю файл заново: %v", err)
		b.fileIDs.Delete(path)
	}
	fileID, err := b.tg.SendDocument(chatID, path, caption)
	if err != nil {
		return err
	}
	if fileID != "" {
		b.fileIDs.Store(path, fileID)
	}
	return nil
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
		if _, e := b.tg.SendMessage(chatID, "Еще ⤵:", b.afterSongKeyboard(idx)); e != nil {
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
	if _, err := b.tg.SendMessage(chatID, "Еще ⤵:", b.afterSongKeyboard(idx)); err != nil {
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

// handleCallback обрабатывает нажатие инлайн-кнопки. AnswerCallbackQuery и проверка
// cq.Message != nil уже выполнены в dispatch.
func (b *Bot) handleCallback(cq *tg.CallbackQuery) {
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
	case strings.HasPrefix(data, "chords|"):
		idx := parseIntOr(strings.TrimPrefix(data, "chords|"), -1)
		if b.titleByIndex(idx) == "" {
			return
		}
		b.sendChords(chatID, idx)
	case strings.HasPrefix(data, "tr|"):
		// tr|<индекс песни>|<сдвиг>
		f := strings.Split(data, "|")
		if len(f) != 3 {
			return
		}
		idx := parseIntOr(f[1], -1)
		if b.titleByIndex(idx) == "" {
			return
		}
		b.editChords(chatID, msgID, idx, parseIntOr(f[2], 0))
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
