// Package gemini — минимальный клиент Google Generative Language API (Gemini)
// поверх net/http, без официального SDK.
package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultModel = "gemini-2.5-flash"

// maxConcurrent — сколько запросов к Gemini может выполняться одновременно.
// Остальные ждут своей очереди, чтобы не упереться в лимиты API (ошибка 429).
const maxConcurrent = 5

type Client struct {
	apiKey string
	model  string
	http   *http.Client
	sem    chan struct{} // семафор: ограничивает число одновременных запросов
}

// New создаёт клиента Gemini. Если model пустая строка, используется defaultModel.
// Актуальный список доступных моделей и их поддерживаемых методов можно получить через
// GET https://generativelanguage.googleapis.com/v1beta/models?key=API_KEY (ListModels).
func New(apiKey, model string) *Client {
	if model == "" {
		model = defaultModel
	}
	return &Client{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 60 * time.Second},
		sem:    make(chan struct{}, maxConcurrent),
	}
}

type generateRequest struct {
	Contents []content `json:"contents"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inlineData,omitempty"`
}

// inlineData — файл (например, PDF), переданный прямо в запросе в base64.
type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type generateResponse struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
}

// Generate отправляет prompt модели Gemini и возвращает сгенерированный текст.
// Безопасен для вызова из нескольких горутин одновременно.
func (c *Client) Generate(prompt string) (string, error) {
	return c.generate(context.Background(), []part{{Text: prompt}})
}

// ExtractChordsFromPDF распознаёт PDF-скан с текстом песни и аккордами и возвращает
// его как обычный текст, где аккорды стоят строкой над словами.
func (c *Client) ExtractChordsFromPDF(ctx context.Context, pdf []byte) (string, error) {
	const prompt = `Это скан листа с текстом христианской песни и гитарными аккордами.
Перепиши его как обычный текст:
- аккорды пиши отдельной строкой НАД словами, выравнивая пробелами так, чтобы каждый аккорд
  стоял над тем слогом, над которым он стоит на скане;
- сохрани названия частей (Куплет, Припев, Проигрыш и т.п.) и пустые строки между ними;
- аккорды пиши латиницей ровно так, как на скане (H, B, Hm, F#m, D/F# и т.д.);
- ничего не добавляй от себя, не используй markdown и не пиши пояснений.`
	return c.generate(ctx, []part{
		{InlineData: &inlineData{MimeType: "application/pdf", Data: base64.StdEncoding.EncodeToString(pdf)}},
		{Text: prompt},
	})
}

func (c *Client) generate(ctx context.Context, parts []part) (string, error) {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.sem }()

	// Ключ передаётся заголовком, а не в URL: URL попадает в текст сетевых ошибок и в логи.
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", c.model)

	body, err := json.Marshal(generateRequest{Contents: []content{{Parts: parts}}})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("запрос к Gemini API не выполнен: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API вернул статус %d: %s", resp.StatusCode, string(raw))
	}

	var gr generateResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", fmt.Errorf("не удалось разобрать ответ Gemini: %w", err)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini не вернул ни одного варианта ответа")
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}
