# UniBot

Asistente universitario inteligente que opera como bot de Telegram. Gestiona la vida académica mediante lenguaje natural usando **DeepSeek V4 Flash** (Function Calling) e integración con Google Calendar, Classroom, Drive, Vision y Notion. Backend en **Go**, desplegado en **Railway** con Docker multi-stage.

## Stack

- **Go 1.23+** — binarios nativos ultraligeros (~5-10 MB), ~10-20 MB RAM idle.
- **DeepSeek V4 Flash** (`deepseek-v4-flash`) — 1M context window, Function Calling nativo.
- **Telegram**: `go-telegram/bot` (zero dependencies, Bot API 10.2).
- **Google**: SDKs oficiales `google.golang.org/api` (Calendar, Classroom, Drive, Vision) con refresh OAuth2 automático.
- **Notion**: `jomei/notionapi`.
- **Memoria**: Supabase (historial de conversación + tokens OAuth).
- **Deploy**: Railway + Docker multi-stage (imagen final ~15-25 MB).

## Estructura

```
├── main.go                  # Servidor Gin, webhook, graceful shutdown
├── config/                  # Carga de variables de entorno
├── tools/                   # Definición declarativa de tools (JSON Schema)
├── googleauth/              # OAuth2 con refresh automático
├── executor/                # Ejecución de tools
├── orchestrator/            # Conversación con DeepSeek (function calling)
├── bot/                     # Handlers de Telegram (texto, fotos, voz)
├── Dockerfile               # Build multi-stage (builder → distroless)
├── docker-compose.yml       # Desarrollo local
└── railway.toml             # Configuración de deploy en Railway
```

## Herramientas (Tools)

| Tool | Servicio |
| --- | --- |
| `create_calendar_event` | Google Calendar |
| `list_classroom_tasks` | Google Classroom |
| `save_note` | Notion |
| `upload_image` | Google Drive + Vision OCR |

## Requisitos

- Go 1.23+
- Token de bot de Telegram (@BotFather)
- API key de DeepSeek
- Proyecto Google Cloud con OAuth 2.0 (scopes: Calendar, Classroom, Drive, Vision)
- Integración de Notion
- Supabase (para TokensStore e historial)

## Configuración

```bash
cp .env.example .env
# Completar credenciales en .env

go mod tidy
go build -o unibot ./main.go
./unibot
```

Servidor en `http://localhost:8080`, healthcheck en `GET /health`.

### Desarrollo local con Docker

```bash
docker compose up -d
```

## Deploy en Railway

1. Vincular el repositorio con Railway.
2. Configurar las variables de entorno del Dashboard (ver `.env.example`).
3. Railway detecta el `Dockerfile` (builder `DOCKERFILE` en `railway.toml`) y despliega en ~15 s.
4. Configurar `WEBHOOK_URL` con el dominio asignado.

## Seguridad

- Tokens OAuth cifrados (AES-256) en Supabase, nunca en texto plano.
- Scope mínimo de Google (sin Gmail/contactos).
- HTTPS automático vía Railway.
- Imagen distroless + usuario nonroot.
- `/.env` nunca al repositorio.

## Documentación

La documentación completa (arquitectura, costos, roadmap, código) vive en el vault de Obsidian **UniBot** (secciones Producto, Negocio y Técnico).
