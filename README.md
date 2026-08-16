<div align="center">

# 🎓 UniBot

**Asistente Universitario Inteligente para Telegram**

Gestiona tu vida académica mediante lenguaje natural: calendario, tareas, apuntes y fotos del pizarrón, todo en un solo chat.

![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat-square&logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-yellow?style=flat-square)
![Telegram](https://img.shields.io/badge/Telegram-Bot_API_10.2-26A5E4?style=flat-square&logo=telegram&logoColor=white)
![Deploy](https://img.shields.io/badge/Deploy-Railway-131415?style=flat-square&logo=railway&logoColor=white)

</div>

## 📖 Descripción

UniBot es un asistente universitario que opera como bot de **Telegram**. Usa **DeepSeek V4 Flash** con **Function Calling** para interpretar intenciones en lenguaje natural y ejecutar acciones en Google Calendar, Google Classroom, Google Drive, Google Vision y Notion, eliminando el context-switching entre aplicaciones académicas.

Backend en **Go 1.23+** con binarios ultraligeros (~10-20 MB RAM en idle) y despliegue en **Railway** mediante Docker multi-stage (imagen final ~15-25 MB).

## ✨ Características

| Capacidad | Tool | Servicio |
| --- | --- | --- |
| 📅 Crear eventos de parciales y clases | `create_calendar_event` | Google Calendar |
| 🎓 Consultar tareas pendientes por curso | `list_classroom_tasks` | Google Classroom |
| 📝 Guardar notas estructuradas con tags | `save_note` | Notion |
| 📷 Procesar fotos del pizarrón (OCR) | `upload_image` | Google Drive + Vision |
| 🧠 Memoria de conversación (20 msgs) | — | Supabase |
| 📊 Resumen semanal unificado | `get_weekly_summary` *(roadmap)* | Calendar + Classroom + Notion |

Formatos de entrada: mensajes de texto, imágenes y notas de voz.

## 🏗️ Arquitectura

```
┌──────────┐  webhook   ┌───────────────────┐
│ Telegram │ ─────────► │ Gin Web Server     │
└──────────┘            │ (Railway, HTTPS)   │
                        └─────────┬─────────┘
                                  │
                        ┌─────────▼─────────┐   historial   ┌──────────┐
                        │  Orchestrator      │ ────────────► │ Supabase │
                        │  (DeepSeek V4      │   tokens      │          │
                        │   Flash + Tools)   │ ◄──────────── │          │
                        └─────────┬─────────┘               └──────────┘
                                  │ tool calls
                        ┌─────────▼─────────┐
                        │  Executor          │
                        └────┬─────┬────┬────┘
                             │     │    │
              ┌──────────────┘     │    └──────────────┐
       ┌──────▼─────┐     ┌────────▼─────┐     ┌───────▼──────┐
       │ Google      │     │ Google       │     │ Google       │
       │ Calendar    │     │ Classroom    │     │ Drive+Vision │
       └─────────────┘     └──────────────┘     └──────────────┘
                              ┌────────┐
                              │ Notion │
                              └────────┘
```

## 📁 Estructura del Proyecto

```
├── main.go                  # Servidor Gin, webhook de Telegram, graceful shutdown
├── config/                  # Carga de variables de entorno (godotenv)
├── tools/                   # Definición declarativa de tools (JSON Schema)
├── googleauth/              # OAuth2 con refresh automático (TokenSource)
├── executor/                # Ejecución de las tools sobre las APIs
├── orchestrator/            # Conversación con DeepSeek (function calling, 2 pasadas)
├── bot/                     # Handlers de Telegram (texto, fotos, voz)
├── Dockerfile               # Build multi-stage (golang:alpine → distroless)
├── docker-compose.yml       # Desarrollo local
├── railway.toml             # Configuración de deploy en Railway
└── .env.example             # Variables de entorno de referencia
```

## 🚀 Empezar

### Requisitos

- Go 1.23+
- Token de bot de Telegram (crear con [@BotFather](https://t.me/BotFather))
- API key de [DeepSeek](https://platform.deepseek.com)
- Proyecto Google Cloud con OAuth 2.0 (scopes: Calendar, Classroom, Drive, Vision)
- Integración de [Notion](https://www.notion.so/my-integrations)
- Proyecto Supabase (TokenStore + historial de conversación)

### Configuración

```bash
# 1. Clonar y configurar entorno
cp .env.example .env
#    → completar credenciales en .env

# 2. Compilar y ejecutar
go mod tidy
go build -o unibot ./main.go
./unibot
```

Servidor en `http://localhost:8080` — healthcheck en `GET /health`.

### Desarrollo local con Docker

```bash
docker compose up -d
```

## ☁️ Deploy en Railway

1. Vincular el repositorio con [Railway](https://railway.app).
2. Configurar las variables de entorno en el Dashboard (ver `.env.example`).
3. Railway detecta el `Dockerfile` automáticamente (`builder = "DOCKERFILE"` en `railway.toml`) y despliega en ~15 s.
4. Configurar `WEBHOOK_URL` con el dominio asignado por Railway.
5. Verificar: `GET /health` → `{"status":"healthy"}`.

> El plan **Starter** (gratuito, $5 de crédito/mes) es suficiente para un bot personal: el contenedor consume ~10-20 MB de RAM.

## 🔐 Seguridad

- Tokens OAuth cifrados (AES-256) en Supabase, nunca en texto plano.
- Scope mínimo de Google (sin Gmail ni contactos).
- Sanitización de entradas y validación de esquema JSON en tools (anti prompt-injection).
- Rate limiting por usuario (protege costos de API).
- HTTPS automático vía Railway.
- Imagen base **distroless** + usuario **nonroot** (sin shell en producción).
- `.env` nunca versionado.

## 🗺️ Roadmap

| Fase | Alcance |
| --- | --- |
| **1. Fundamentos** | Bot + DeepSeek conectado *(en curso)* |
| **2. Integración Google** | OAuth2, Calendar, Classroom |
| **3. Knowledge Base** | Supabase, Notion, Drive, OCR |
| **4. Inteligencia Avanzada** | RAG, Whisper, recordatorios automáticos |
| **5. Producción** | Webhooks, monitoreo, dominio personalizado |

## 📄 Licencia

[MIT](LICENSE) © 2026 Deiver Vásquez
