package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

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

	log.Printf("[%s] msg from @%s (%d): %q", time.Now().Format("15:04:05"), update.Message.From.UserName, chatID, text)

	bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))

	start := time.Now()
	resp, err := client.Chat(systemPrompt, history, text)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		log.Printf("[%s] llm error after %dms: %v", time.Now().Format("15:04:05"), latency, err)
		database.AddRequestLog(chatID, update.Message.From.UserName, text, "", "", "", latency, err.Error())
		send(bot, chatID, fmt.Sprintf("AI error: %v", err))
		return
	}

	var reply, toolName, toolArgs string

	if resp.ToolCall != nil {
		toolName = resp.ToolCall.Name
		if b, e := json.Marshal(resp.ToolCall.Arguments); e == nil {
			toolArgs = string(b)
		}
		log.Printf("[%s] tool=%s args=%s latency=%dms", time.Now().Format("15:04:05"), toolName, toolArgs, latency)
		action := toolCallToAction(resp.ToolCall)
		reply = ExecuteAction(database, lw, user.ID, action)
		if reply == "" {
			reply = "Done."
		}
	} else {
		reply = strings.TrimSpace(resp.Message.Content)
		if reply == "" {
			reply = "Done."
		}
		log.Printf("[%s] text response latency=%dms", time.Now().Format("15:04:05"), latency)
	}

	log.Printf("[%s] reply: %q", time.Now().Format("15:04:05"), reply)
	database.AddRequestLog(chatID, update.Message.From.UserName, text, toolName, toolArgs, reply, latency, "")
	database.AddMessage(session.ID, "user", text)
	database.AddMessage(session.ID, "assistant", reply)
	send(bot, chatID, reply)
}

func toolCallToAction(tc *groq.ToolCall) *Action {
	a := &Action{Action: tc.Name}
	args := tc.Arguments
	if v, ok := args["content"].(string); ok {
		a.Content = v
	}
	if v, ok := args["id"].(float64); ok {
		a.ID = int64(v)
	}
	if v, ok := args["amount"].(float64); ok {
		a.Amount = v
	} else if s, ok := args["amount"].(string); ok {
		a.Amount, _ = strconv.ParseFloat(s, 64)
	}
	if v, ok := args["balance"].(float64); ok {
		a.Balance = v
	} else if s, ok := args["balance"].(string); ok {
		a.Balance, _ = strconv.ParseFloat(s, 64)
	}
	if v, ok := args["name"].(string); ok {
		a.Name = v
	}
	if v, ok := args["account"].(string); ok {
		a.Account = v
	}
	if v, ok := args["merchant"].(string); ok {
		a.Merchant = v
	}
	if v, ok := args["category"].(string); ok {
		a.Category = v
	}
	if v, ok := args["day"].(float64); ok {
		a.Day = int(v)
	} else if s, ok := args["day"].(string); ok {
		if n, err := strconv.Atoi(s); err == nil {
			a.Day = n
		}
	}
	if v, ok := args["due_date"].(string); ok {
		a.DueDate = v
	}
	return a
}

func send(bot *tgbotapi.BotAPI, chatID int64, text string) {
	if _, err := bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		log.Printf("send to %d: %v", chatID, err)
	}
}
