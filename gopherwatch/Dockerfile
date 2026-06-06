# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Bağımlılıkları kopyala ve indir
COPY go.mod go.sum ./
RUN go mod download

# Kaynak kodları kopyala
COPY . .

# Statik binary olarak derle (CGO_ENABLED=0)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o gopherwatch ./cmd/gopherwatch

# Final stage
FROM alpine:3.19

WORKDIR /app

# Sertifikalar için ca-certificates ekle 
RUN apk --no-cache add ca-certificates tzdata

# Builder'dan derlenmiş binary'i al
COPY --from=builder /app/gopherwatch /usr/local/bin/

# Gerekli dizinleri oluştur
RUN mkdir -p /etc/gopherwatch /var/lib/gopherwatch

ENTRYPOINT ["gopherwatch"]
CMD ["start", "-config", "/etc/gopherwatch/config.yaml"]
