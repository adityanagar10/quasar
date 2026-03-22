# Quasar

A personal Telegram bot powered by Groq LLM. Talk to it naturally — it understands intent and executes actions without commands or menus.

## Features

- **Notes** — save, list, delete
- **Tasks** — add, complete, delete, bulk-complete
- **Finance** — track accounts, log expenses, monthly/weekly summaries, SIPs, yearly expense reminders
- **Reminders** — one-time and recurring (cron-based)
- **Time logging** — hourly prompts to log what you worked on, EOD summary
- **Goals & activities** — track personal goals by category

## Stack

- **Go 1.23**
- **Groq API** — LLM inference
- **SQLite** — local persistence
- **Telegram Bot API**
- **Docker**

## Setup

### 1. Clone and configure

```bash
git clone https://github.com/adityanagar10/quasar.git
cd quasar
cp .env.example .env
```

Edit `.env`:

```
TELEGRAM_BOT_TOKEN=your_telegram_bot_token
GROQ_API_KEY=your_groq_api_key
GROQ_MODEL=llama-3.1-8b-instant
```

### 2. Run with Docker

```bash
docker build -t quasar .
docker run -d \
  --env-file .env \
  -e DB_PATH=/data/bot.db \
  -e ALLOWED_CHAT_IDS=your_chat_id \
  -v $(pwd)/data:/data \
  quasar
```

### 3. Run locally

```bash
go build -o bot .
DB_PATH=./bot.db $(cat .env | xargs) ./bot
```

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `TELEGRAM_BOT_TOKEN` | Yes | — | Bot token from @BotFather |
| `GROQ_API_KEY` | Yes | — | Groq API key |
| `GROQ_MODEL` | No | `llama-3.1-8b-instant` | Groq model ID |
| `DB_PATH` | No | `/data/bot.db` | SQLite database path |
| `LEDGER_PATH` | No | `/data/expenses.ledger` | Plaintext ledger export path |
| `ALLOWED_CHAT_IDS` | No | open to all | Comma-separated Telegram chat IDs |
| `TIME_LOG_CHAT_ID` | No | disabled | Chat ID to receive hourly time log prompts |

## Usage

Just message the bot naturally:

```
note that I need to review the PR tomorrow
add a task to call the dentist
spent 450 at Swiggy from HDFC
what did I spend this month?
remind me at 6pm to take medicine
worked on the auth refactor
show my time logs
```
