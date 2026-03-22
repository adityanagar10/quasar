package main

import (
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramBotToken string
	DBPath           string
	GroqAPIKey       string
	GroqModel        string
	LedgerPath       string
	AllowedChatIDs   map[int64]bool // empty = open to all
	TimeLogChatID    int64
}

func LoadConfig() *Config {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/bot.db"
	}

	groqKey := os.Getenv("GROQ_API_KEY")
	if groqKey == "" {
		log.Fatal("GROQ_API_KEY is required")
	}

	groqModel := os.Getenv("GROQ_MODEL")
	if groqModel == "" {
		groqModel = "llama-3.1-8b-instant"
	}

	ledgerPath := os.Getenv("LEDGER_PATH")
	if ledgerPath == "" {
		ledgerPath = "/data/expenses.ledger"
	}

	allowedIDs := map[int64]bool{}
	if raw := os.Getenv("ALLOWED_CHAT_IDS"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				allowedIDs[id] = true
			}
		}
	}

	var timeLogChatID int64
	if raw := os.Getenv("TIME_LOG_CHAT_ID"); raw != "" {
		timeLogChatID, _ = strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	}

	return &Config{
		TelegramBotToken: token,
		DBPath:           dbPath,
		GroqAPIKey:       groqKey,
		GroqModel:        groqModel,
		LedgerPath:       ledgerPath,
		AllowedChatIDs:   allowedIDs,
		TimeLogChatID:    timeLogChatID,
	}
}
