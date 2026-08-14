// git pull origin master - если были изменения в гите
package main

import (
	"bufio"
	"log"
	"os"
	"strings"

	"songbot/internal/bot"
	"songbot/internal/gemini"
	"songbot/internal/songs"
)

func main() {
	loadDotEnv(".env")

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("не задана переменная окружения BOT_TOKEN")
	}
	googleAPIKey := os.Getenv("GOOGLE_API_KEY")
	if googleAPIKey == "" {
		log.Fatal("не задана переменная окружения GOOGLE_API_KEY")
	}

	store, err := songs.Load("songs", "chords")
	if err != nil {
		log.Fatalf("не удалось загрузить песни: %v", err)
	}
	log.Printf("загружено песен: %d, файлов аккордов: %d", len(store.Titles), len(store.Chords))

	geminiClient := gemini.New(googleAPIKey, os.Getenv("GEMINI_MODEL"))
	b := bot.New(token, store, geminiClient)

	if err := b.Run(); err != nil {
		log.Fatalf("бот остановлен с ошибкой: %v", err)
	}
}

// loadDotEnv — простой загрузчик .env (KEY=VALUE построчно), без внешних зависимостей.
// Не перезаписывает переменные, уже заданные в окружении процесса.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // .env не обязателен — переменные могут быть заданы в окружении напрямую
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}
