// Package gemini — минимальный клиент Google Generative Language API (Gemini)
// поверх net/http, без официального SDK.
package gemini

import (
	"bytes"
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
	Text string `json:"text"`
}

type generateResponse struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
}

// Generate отправляет prompt модели Gemini и возвращает сгенерированный текст.
// Безопасен для вызова из нескольких горутин одновременно.
func (c *Client) Generate(prompt string) (string, error) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		c.model, c.apiKey,
	)

	body, err := json.Marshal(generateRequest{Contents: []content{{Parts: []part{{Text: prompt}}}}})
	if err != nil {
		return "", err
	}

	resp, err := c.http.Post(url, "application/json", bytes.NewReader(body))
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
