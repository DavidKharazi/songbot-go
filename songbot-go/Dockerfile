# ЭТАП 1: Сборка бинарника
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Копируем файлы зависимостей из подпапки
COPY songbot-go/go.mod ./
# Если у вас появится go.sum, раскомментируйте строчку ниже:
# COPY songbot-go/go.sum ./

RUN go mod download

# Копируем весь исходный код из подпапки
COPY songbot-go/ .

# Собираем бинарник под Linux
RUN CGO_ENABLED=0 GOOS=linux go build -o songbot .

# ЭТАП 2: Легковесный образ для запуска
FROM alpine:latest

WORKDIR /app

# Сертификаты для HTTPS (Telegram, Gemini) и pdftotext (poppler-utils) —
# им бот переводит PDF с аккордами в текст для транспонирования
RUN apk --no-cache add ca-certificates poppler-utils

# Копируем собранный бинарник и необходимые папки с файлами
COPY --from=builder /app/songbot .
COPY --from=builder /app/songs ./songs
COPY --from=builder /app/chords ./chords

# Запускаем бота
CMD ["./songbot"]