# Stage 1: Builder
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Instalar dependencias del sistema necesarias para compilar
RUN apk add --no-cache git ca-certificates tzdata

# Copiar go.mod y go.sum primero para cachear dependencias
COPY go.mod go.sum ./
RUN go mod download

# Copiar código fuente
COPY . .

# Compilar binario estático
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o unibot ./main.go

# Stage 2: Runtime mínimo
FROM gcr.io/distroless/static-debian12

# Copiar certificados CA y zona horaria
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copiar binario
COPY --from=builder /app/unibot /unibot

# Puerto expuesto (Railway asigna $PORT automáticamente)
EXPOSE 8080

# Usuario no-root
USER nonroot:nonroot

# Comando de inicio
ENTRYPOINT ["/unibot"]
