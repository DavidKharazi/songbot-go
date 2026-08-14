# Этап 1: Сборка Go-приложения.
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Копируем модули и загружаем зависимости
COPY go.mod ./
# COPY go.sum ./  (раскомментируйте, если файл go.sum появится)
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем исполняемый файл
RUN CGO_ENABLED=0 GOOS=linux go build -o songbot ./main.go

# Этап 2: Минимальный финальный образ
FROM alpine:latest

# Устанавливаем сертификаты для работы HTTPS (Telegram и Gemini API)
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Копируем бинарник из первого этапа
COPY --from=builder /app/songbot .

# Копируем папки с песнями и аккордами
COPY --from=builder /app/chords ./chords
COPY --from=builder /app/songs ./songs
COPY --from=builder /app/internal/songs ./internal/songs

CMD ["./songbot"]