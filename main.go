package main

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"adityanagar.com/ad-bot/internal/db"
	"adityanagar.com/ad-bot/internal/groq"
	"adityanagar.com/ad-bot/internal/ledger"
)

func main() {
	cfg := LoadConfig()

	database, err := db.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	lw := ledger.NewWriter(cfg.LedgerPath)

	bot, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}
	log.Printf("Authorized on account %s", bot.Self.UserName)

	client := groq.New(cfg.GroqAPIKey, cfg.GroqModel)

	scheduler := NewScheduler(database, bot, cfg.TimeLogChatID)
	scheduler.Start()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	for update := range bot.GetUpdatesChan(u) {
		if update.Message == nil || strings.TrimSpace(update.Message.Text) == "" {
			continue
		}
		if len(cfg.AllowedChatIDs) > 0 && !cfg.AllowedChatIDs[update.Message.Chat.ID] {
			bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Access denied."))
			continue
		}
		go handleMessage(update, database, lw, bot, client)
	}
}

func handleMessage(update tgbotapi.Update, database *db.DB, lw *ledger.Writer, bot *tgbotapi.BotAPI, client *groq.Client) {
	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)

	if text == "/start" {
		send(bot, chatID, "Hey! Just talk to me naturally.\n\nExamples:\n• \"note that I need to buy milk\"\n• \"what are my notes?\"\n• \"add a task to call the dentist\"\n• \"what tasks do I have?\"\n• \"delete note 3\"\n\nOr just chat with me!")
		return
	}

	if text == "/new" {
		user, err := database.GetOrCreateUser(chatID, update.Message.From.UserName)
		if err != nil {
			send(bot, chatID, "Error.")
			return
		}
		s, err := database.CreateSession(user.ID, "new")
		if err != nil {
			send(bot, chatID, "Error.")
			return
		}
		database.SetActiveSession(user.ID, s.ID)
		send(bot, chatID, "Fresh start. What's on your mind?")
		return
	}

	user, err := database.GetOrCreateUser(chatID, update.Message.From.UserName)
	if err != nil {
		send(bot, chatID, "Error getting user.")
		return
	}
	session, err := database.GetActiveSession(user)
	if err != nil {
		send(bot, chatID, "Error getting session.")
		return
	}

	history, _ := database.GetRecentMessages(session.ID, 20)
	goals, _ := database.ListGoals(user.ID)
	systemPrompt := groq.BuildSystemPrompt(goals)

	bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))

	resp, err := client.Chat(systemPrompt, history, text)
	if err != nil {
		send(bot, chatID, fmt.Sprintf("AI error: %v", err))
		return
	}

	raw := strings.TrimSpace(resp.Message.Content)
	raw = stripThinkTags(raw)
	log.Printf("llm raw: %q", raw)

	action := ParseAction(raw)
	var reply string

	if action != nil {
		log.Printf("action: %s id=%d content=%q", action.Action, action.ID, action.Content)
		reply = ExecuteAction(database, lw, user.ID, action)
		if reply == "" {
			// Unknown action — treat as plain conversation
			reply = raw
		}
	} else {
		// Plain conversation — but don't send raw JSON noise to the user
		if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
			reply = "Sorry, I didn't quite get that. Could you rephrase?"
		} else {
			reply = raw
		}
	}

	if reply == "" {
		reply = "Done."
	}

	database.AddMessage(session.ID, "user", text)
	database.AddMessage(session.ID, "assistant", reply)
	send(bot, chatID, reply)
}

// stripThinkTags removes <think>...</think> blocks produced by reasoning models
// like deepseek-r1 before the actual response content.
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			s = strings.TrimSpace(s[:start])
			break
		}
		s = strings.TrimSpace(s[:start] + s[end+len("</think>"):])
	}
	return strings.TrimSpace(s)
}

func send(bot *tgbotapi.BotAPI, chatID int64, text string) {
	if _, err := bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		log.Printf("send to %d: %v", chatID, err)
	}
}
