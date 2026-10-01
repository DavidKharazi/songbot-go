// Package telegram реализует минимальный клиент Telegram Bot API поверх net/http.
// Внешние зависимости не используются, чтобы проект собирался только стандартной библиотекой.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Bot — клиент Telegram Bot API.
type Bot struct {
	token  string
	base   string
	client *http.Client
}

// New создаёт нового клиента бота с указанным токеном.
func New(token string) *Bot {
	return &Bot{
		token:  token,
		base:   "https://api.telegram.org/bot" + token,
		client: &http.Client{Timeout: 65 * time.Second},
	}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// callJSON вызывает метод API, отправляя payload как JSON, и декодирует result в out.
func (b *Bot) callJSON(method string, payload interface{}, out interface{}) error {
	return b.callJSONCtx(context.Background(), method, payload, out)
}

// callJSONCtx — то же, что callJSON, но запрос прерывается при отмене ctx
// (нужно для long polling, чтобы бот мог быстро остановиться).
func (b *Bot) callJSONCtx(ctx context.Context, method string, payload interface{}, out interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var ar apiResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return fmt.Errorf("не удалось разобрать ответ %s: %w (тело: %s)", method, err, string(raw))
	}
	if !ar.OK {
		return fmt.Errorf("telegram API %s вернул ошибку: %s", method, ar.Description)
	}
	if out != nil && ar.Result != nil {
		if err := json.Unmarshal(ar.Result, out); err != nil {
			return fmt.Errorf("не удалось разобрать result %s: %w", method, err)
		}
	}
	return nil
}

// ---------- Типы ----------

type Chat struct {
	ID int64 `json:"id"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

type Message struct {
	MessageID int       `json:"message_id"`
	Chat      Chat      `json:"chat"`
	Text      string    `json:"text,omitempty"`
	Document  *Document `json:"document,omitempty"`
}

// Document — файл, отправленный в Telegram. FileID можно переиспользовать
// для повторной отправки без загрузки файла заново.
type Document struct {
	FileID string `json:"file_id"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// InlineKeyboardButton — кнопка инлайн-клавиатуры.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// InlineKeyboardMarkup — инлайн-клавиатура (под сообщением).
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// ReplyKeyboardMarkup — обычная клавиатура (вместо системной клавиатуры устройства).
type ReplyKeyboardMarkup struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool               `json:"resize_keyboard"`
	OneTimeKeyboard bool               `json:"one_time_keyboard,omitempty"`
}

type KeyboardButton struct {
	Text string `json:"text"`
}

// ---------- Методы API ----------

// GetUpdates забирает обновления через long polling. Запрос прерывается при отмене ctx.
func (b *Bot) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	payload := map[string]interface{}{
		"offset":  offset,
		"timeout": timeout,
	}
	var updates []Update
	if err := b.callJSONCtx(ctx, "getUpdates", payload, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// SendMessage отправляет текстовое сообщение, опционально с клавиатурой.
func (b *Bot) SendMessage(chatID int64, text string, markup interface{}) (*Message, error) {
	return b.sendMessage(chatID, text, "", markup)
}

// SendMessageHTML отправляет сообщение с HTML-разметкой (<b>, <pre> и т.п.).
// Спецсимволы &, <, > в тексте нужно экранировать через html.EscapeString.
func (b *Bot) SendMessageHTML(chatID int64, text string, markup interface{}) (*Message, error) {
	return b.sendMessage(chatID, text, "HTML", markup)
}

func (b *Bot) sendMessage(chatID int64, text, parseMode string, markup interface{}) (*Message, error) {
	payload := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	var msg Message
	if err := b.callJSON("sendMessage", payload, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// EditMessageText редактирует текст ранее отправленного сообщения.
func (b *Bot) EditMessageText(chatID int64, messageID int, text string, markup *InlineKeyboardMarkup) error {
	return b.editMessageText(chatID, messageID, text, "", markup)
}

// EditMessageTextHTML редактирует сообщение с HTML-разметкой.
func (b *Bot) EditMessageTextHTML(chatID int64, messageID int, text string, markup *InlineKeyboardMarkup) error {
	return b.editMessageText(chatID, messageID, text, "HTML", markup)
}

func (b *Bot) editMessageText(chatID int64, messageID int, text, parseMode string, markup *InlineKeyboardMarkup) error {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	return b.callJSON("editMessageText", payload, nil)
}

// DeleteMessage удаляет сообщение.
func (b *Bot) DeleteMessage(chatID int64, messageID int) error {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	return b.callJSON("deleteMessage", payload, nil)
}

// AnswerCallbackQuery подтверждает получение нажатия на инлайн-кнопку.
func (b *Bot) AnswerCallbackQuery(callbackID string) error {
	payload := map[string]interface{}{
		"callback_query_id": callbackID,
	}
	return b.callJSON("answerCallbackQuery", payload, nil)
}

// SendDocumentByFileID отправляет уже загруженный ранее в Telegram файл по его file_id —
// без повторной загрузки содержимого, поэтому это почти мгновенно.
func (b *Bot) SendDocumentByFileID(chatID int64, fileID string, caption string) error {
	payload := map[string]interface{}{
		"chat_id":  chatID,
		"document": fileID,
	}
	if caption != "" {
		payload["caption"] = caption
	}
	return b.callJSON("sendDocument", payload, nil)
}

// SendDocument отправляет файл с диска (multipart/form-data) и возвращает его file_id,
// который можно передать в SendDocumentByFileID для последующих отправок.
func (b *Bot) SendDocument(chatID int64, filePath string, caption string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)

	if err := w.WriteField("chat_id", fmt.Sprintf("%d", chatID)); err != nil {
		return "", err
	}
	if caption != "" {
		if err := w.WriteField("caption", caption); err != nil {
			return "", err
		}
	}
	part, err := w.CreateFormFile("document", filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, b.base+"/sendDocument", buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var ar apiResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return "", fmt.Errorf("не удалось разобрать ответ sendDocument: %w", err)
	}
	if !ar.OK {
		return "", fmt.Errorf("не удалось отправить файл %s: %s", filePath, ar.Description)
	}
	var msg Message
	if err := json.Unmarshal(ar.Result, &msg); err != nil || msg.Document == nil {
		return "", nil // файл отправлен, просто не удалось получить file_id — не ошибка
	}
	return msg.Document.FileID, nil
}

// EscapeCallback безопасно готовит строку для query-параметра (не используется Telegram напрямую,
// оставлено для единообразия при необходимости построения url.Values в других методах).
func EscapeCallback(s string) string {
	return url.QueryEscape(s)
}
