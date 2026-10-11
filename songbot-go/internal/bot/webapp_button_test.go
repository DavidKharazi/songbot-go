package bot

import (
	"net/url"
	"testing"
)

func TestWebAppButton(t *testing.T) {
	b := &Bot{webAppURL: "https://songbot.up.railway.app"}

	// Личный чат (id > 0) — кнопка мини-приложения.
	btn, ok := b.webAppButton(42, "О благодать (D)", 14)
	if !ok || btn.WebApp == nil || btn.URL != "" {
		t.Fatalf("в личном чате ожидалась web_app-кнопка: %+v", btn)
	}
	u, err := url.Parse(btn.WebApp.URL)
	if err != nil || u.Query().Get("title") != "О благодать (D)" || u.Query().Get("shift") != "2" {
		t.Fatalf("неверная ссылка: %q", btn.WebApp.URL)
	}

	// Группа (id < 0) — web_app там запрещены, поэтому обычная ссылка.
	btn, _ = b.webAppButton(-100123, "Песня", 0)
	if btn.WebApp != nil || btn.URL == "" {
		t.Fatalf("в группе ожидалась ссылка: %+v", btn)
	}

	// Без WEBAPP_URL кнопки нет.
	if _, ok := (&Bot{}).webAppButton(42, "Песня", 0); ok {
		t.Fatal("кнопка не должна показываться без WEBAPP_URL")
	}
}
