// Package telegram реализует минимальный клиент Telegram Bot API поверх net/http.
// Внешние зависимости не используются, чтобы проект собирался только стандартной библиотекой.
package telegram

import (
	"bytes"
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
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := b.client.Post(b.base+"/"+method, "application/json", bytes.NewReader(body))
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
	MessageID int    `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text,omitempty"`
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

// GetUpdates забирает обновления через long polling.
func (b *Bot) GetUpdates(offset int64, timeout int) ([]Update, error) {
	payload := map[string]interface{}{
		"offset":  offset,
		"timeout": timeout,
	}
	var updates []Update
	if err := b.callJSON("getUpdates", payload, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// SendMessage отправляет текстовое сообщение, опционально с клавиатурой.
func (b *Bot) SendMessage(chatID int64, text string, markup interface{}) (*Message, error) {
	payload := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
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
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
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

// SendDocument отправляет файл с диска (multipart/form-data).
func (b *Bot) SendDocument(chatID int64, filePath string, caption string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)

	if err := w.WriteField("chat_id", fmt.Sprintf("%d", chatID)); err != nil {
		return err
	}
	if caption != "" {
		if err := w.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := w.CreateFormFile("document", filepath.Base(filePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, b.base+"/sendDocument", buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

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
		return fmt.Errorf("не удалось разобрать ответ sendDocument: %w", err)
	}
	if !ar.OK {
		return fmt.Errorf("не удалось отправить файл %s: %s", filePath, ar.Description)
	}
	return nil
}

// EscapeCallback безопасно готовит строку для query-параметра (не используется Telegram напрямую,
// оставлено для единообразия при необходимости построения url.Values в других методах).
func EscapeCallback(s string) string {
	return url.QueryEscape(s)
}
