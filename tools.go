package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"adityanagar.com/ad-bot/internal/db"
	"adityanagar.com/ad-bot/internal/ledger"
)

// Action is a normalised intent extracted from the model's reply.
type Action struct {
	Action   string  `json:"action"`
	Content  string  `json:"content,omitempty"`
	ID       int64   `json:"id,omitempty"`
	Amount   float64 `json:"amount,omitempty"`
	Account  string  `json:"account,omitempty"`
	Merchant string  `json:"merchant,omitempty"`
	Category string  `json:"category,omitempty"`
	Balance  float64 `json:"balance,omitempty"`
	Name     string  `json:"name,omitempty"`
	Day      int     `json:"day,omitempty"`
	DueDate  string  `json:"due_date,omitempty"` // "MM-DD"
}

// ParseAction extracts an action from the model's reply.
// Handles our preferred format and llama3.2's native {"name":...} format.
func ParseAction(reply string) *Action {
	reply = strings.TrimSpace(reply)
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start == -1 || end <= start {
		return nil
	}
	blob := reply[start : end+1]

	// 1. Our clean format: {"action":"add_note","content":"..."}
	var a Action
	if err := json.Unmarshal([]byte(blob), &a); err == nil && a.Action != "" {
		return &a
	}

	// 2. llama3.2 native: {"name":"notes","parameters":{...}}
	var native struct {
		Name       string          `json:"name"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(blob), &native); err == nil && native.Name != "" {
		return mapNative(native.Name, native.Parameters)
	}

	return nil
}

var nameMap = map[string]string{
	"add_note":       "add_note",
	"note":           "add_note",
	"save_note":      "add_note",
	"notes":          "list_notes",
	"list_notes":     "list_notes",
	"get_notes":      "list_notes",
	"delete_note":    "delete_note",
	"remove_note":    "delete_note",
	"add_task":       "add_task",
	"task":           "add_task",
	"create_task":    "add_task",
	"tasks":          "list_tasks",
	"list_tasks":     "list_tasks",
	"get_tasks":      "list_tasks",
	"complete_task":  "complete_task",
	"done_task":      "complete_task",
	"done":           "complete_task",
	"delete_task":    "delete_task",
	"remove_task":    "delete_task",
	"complete_all":   "complete_all_tasks",
	// Finance
	"set_account":         "set_account",
	"add_account":         "set_account",
	"list_accounts":       "list_accounts",
	"accounts":            "list_accounts",
	"my_accounts":         "list_accounts",
	"add_expense":         "add_expense",
	"expense":             "add_expense",
	"spent":               "add_expense",
	"paid":                "add_expense",
	"get_summary":         "get_summary",
	"summary":             "get_summary",
	"balance":             "get_summary",
	"spending":            "get_summary",
	"add_sip":             "add_sip",
	"sip":                 "add_sip",
	"list_sips":           "list_sips",
	"add_yearly_expense":  "add_yearly_expense",
	"yearly":              "add_yearly_expense",
	"insurance":           "add_yearly_expense",
	"list_yearly_expenses": "list_yearly_expenses",
	"yearly_expenses":     "list_yearly_expenses",
	// Time logging
	"log_time":        "log_time",
	"time_log":        "log_time",
	"logged":          "log_time",
	"list_time_logs":  "list_time_logs",
	"time_logs":       "list_time_logs",
	"my_logs":         "list_time_logs",
}

func mapNative(name string, rawParams json.RawMessage) *Action {
	action, ok := nameMap[name]
	if !ok {
		return nil
	}
	a := &Action{Action: action}
	if len(rawParams) == 0 {
		return a
	}
	var flat map[string]interface{}
	if err := json.Unmarshal(rawParams, &flat); err != nil {
		return a
	}
	if content := deepString(flat, "content"); content != "" {
		a.Content = content
	}
	if id := deepInt(flat, "id"); id != 0 {
		a.ID = id
	}
	return a
}

func deepString(m map[string]interface{}, key string) string {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			if s, ok := v.(string); ok && s != "" &&
				!strings.Contains(s, `"type"`) &&
				!strings.Contains(s, `"description"`) {
				return s
			}
		}
		if sub, ok := v.(map[string]interface{}); ok {
			if found := deepString(sub, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func deepInt(m map[string]interface{}, key string) int64 {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			if n, ok := v.(float64); ok {
				return int64(n)
			}
		}
		if sub, ok := v.(map[string]interface{}); ok {
			if found := deepInt(sub, key); found != 0 {
				return found
			}
		}
	}
	return 0
}

// ExecuteAction runs an action and returns a user-ready reply string.
func ExecuteAction(database *db.DB, lw *ledger.Writer, userID int64, a *Action) string {
	switch a.Action {

	case "add_note":
		if a.Content == "" {
			return "What would you like me to note down?"
		}
		id, err := database.AddNote(userID, "", a.Content)
		if err != nil {
			return fmt.Sprintf("Couldn't save the note: %v", err)
		}
		return fmt.Sprintf("Got it! Note saved (#%d).", id)

	case "list_notes":
		notes, err := database.ListNotes(userID, 10)
		if err != nil {
			return "Couldn't fetch notes right now."
		}
		if len(notes) == 0 {
			return "You have no notes saved."
		}
		var sb strings.Builder
		sb.WriteString("Your notes:\n")
		for _, n := range notes {
			sb.WriteString(fmt.Sprintf("  #%d — %s\n", n.ID, n.Content))
		}
		return strings.TrimSpace(sb.String())

	case "delete_note":
		if a.ID == 0 {
			return "Which note should I delete? Say something like \"delete note 3\"."
		}
		if err := database.DeleteNote(userID, a.ID); err != nil {
			return fmt.Sprintf("Couldn't delete note #%d — does it exist?", a.ID)
		}
		return fmt.Sprintf("Note #%d deleted.", a.ID)

	case "add_task":
		if a.Content == "" {
			return "What's the task?"
		}
		id, err := database.AddTask(userID, a.Content)
		if err != nil {
			return fmt.Sprintf("Couldn't save the task: %v", err)
		}
		return fmt.Sprintf("Task added (#%d): %s", id, a.Content)

	case "list_tasks":
		tasks, err := database.ListTasks(userID)
		if err != nil {
			return "Couldn't fetch tasks right now."
		}
		if len(tasks) == 0 {
			return "No pending tasks — you're all clear!"
		}
		var sb strings.Builder
		sb.WriteString("Your pending tasks:\n")
		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("  #%d — %s\n", t.ID, t.Content))
		}
		return strings.TrimSpace(sb.String())

	case "complete_task":
		if a.ID == 0 {
			return "Which task is done? Say \"complete task 3\" or list tasks first."
		}
		if err := database.CompleteTask(userID, a.ID); err != nil {
			return fmt.Sprintf("Couldn't complete task #%d — does it exist?", a.ID)
		}
		return fmt.Sprintf("Task #%d marked complete!", a.ID)

	case "delete_task":
		if a.ID == 0 {
			return "Which task should I delete? Say \"delete task 3\"."
		}
		if err := database.DeleteTask(userID, a.ID); err != nil {
			return fmt.Sprintf("Couldn't delete task #%d — does it exist?", a.ID)
		}
		return fmt.Sprintf("Task #%d deleted.", a.ID)

	case "complete_all_tasks":
		n, err := database.CompleteAllTasks(userID)
		if err != nil {
			return "Couldn't complete all tasks."
		}
		if n == 0 {
			return "No pending tasks to complete."
		}
		return fmt.Sprintf("Marked all %d tasks as complete!", n)

	// --- Finance actions ---

	case "set_account":
		name := a.Name
		if name == "" {
			name = a.Account
		}
		if name == "" {
			return "Which account? e.g. \"Set HDFC balance to 50000\""
		}
		_, err := database.UpsertAccount(userID, name, a.Balance)
		if err != nil {
			return fmt.Sprintf("Couldn't save account: %v", err)
		}
		return fmt.Sprintf("Account %s set with starting balance ₹%.2f.", name, a.Balance)

	case "list_accounts":
		accounts, err := database.ListAccounts(userID)
		if err != nil {
			return "Couldn't fetch accounts right now."
		}
		if len(accounts) == 0 {
			return "No accounts set up yet. Try \"Set HDFC starting balance to 50000\"."
		}
		var sb strings.Builder
		sb.WriteString("Your accounts:\n")
		for _, acc := range accounts {
			start, spent, err := database.GetAccountBalance(userID, acc.Name)
			if err != nil {
				sb.WriteString(fmt.Sprintf("  %s — ₹%.2f (starting)\n", acc.Name, acc.StartingBalance))
				continue
			}
			current := start - spent
			sb.WriteString(fmt.Sprintf("  %s — ₹%.2f (spent ₹%.2f this month)\n", acc.Name, current, spent))
		}
		return strings.TrimSpace(sb.String())

	case "add_expense":
		if a.Amount <= 0 {
			return "How much was spent?"
		}
		merchant := a.Merchant
		if merchant == "" {
			merchant = a.Content
		}
		if merchant == "" {
			merchant = "Unknown"
		}
		category := a.Category
		if category == "" {
			category = "Expenses:General"
		}

		var accountID int64
		if a.Account != "" {
			acc, err := database.GetAccountByName(userID, a.Account)
			if err == nil {
				accountID = acc.ID
			}
		}

		_, err := database.AddTransaction(userID, accountID, a.Amount, merchant, category)
		if err != nil {
			return fmt.Sprintf("Couldn't save expense: %v", err)
		}
		if lw != nil {
			_ = lw.WriteExpense(time.Now(), merchant, category, a.Amount)
		}
		reply := fmt.Sprintf("Logged ₹%.2f at %s", a.Amount, merchant)
		if a.Account != "" {
			reply += fmt.Sprintf(" from %s", a.Account)
		}
		return reply + "."

	case "get_summary":
		if a.Account != "" {
			start, spent, err := database.GetAccountBalance(userID, a.Account)
			if err != nil {
				return fmt.Sprintf("Couldn't find account %q.", a.Account)
			}
			current := start - spent
			return fmt.Sprintf("%s balance: ₹%.2f (starting ₹%.2f, spent ₹%.2f this month).", a.Account, current, start, spent)
		}
		now := time.Now()
		total, byCategory, err := database.GetMonthSummary(userID, now.Year(), int(now.Month()))
		if err != nil {
			return "Couldn't fetch monthly summary."
		}
		if total == 0 {
			return "No expenses recorded this month."
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("This month's spending: ₹%.2f\n", total))
		for cat, amt := range byCategory {
			sb.WriteString(fmt.Sprintf("  %s: ₹%.2f\n", cat, amt))
		}
		return strings.TrimSpace(sb.String())

	case "add_sip":
		name := a.Name
		if name == "" {
			return "What's the SIP name?"
		}
		if a.Amount <= 0 {
			return "What's the SIP amount?"
		}
		if a.Day == 0 {
			return "Which day of the month does it debit?"
		}
		var accountID int64
		if a.Account != "" {
			acc, err := database.GetAccountByName(userID, a.Account)
			if err == nil {
				accountID = acc.ID
			}
		}
		id, err := database.AddSIP(userID, accountID, name, a.Amount, a.Day)
		if err != nil {
			return fmt.Sprintf("Couldn't save SIP: %v", err)
		}
		return fmt.Sprintf("SIP added (#%d): %s — ₹%.2f on the %s of every month.", id, name, a.Amount, ordinal(a.Day))

	case "list_sips":
		sips, err := database.ListSIPs(userID)
		if err != nil {
			return "Couldn't fetch SIPs."
		}
		if len(sips) == 0 {
			return "No SIPs set up yet."
		}
		var sb strings.Builder
		sb.WriteString("Your SIPs:\n")
		for _, s := range sips {
			line := fmt.Sprintf("  #%d %s — ₹%.2f on %s", s.ID, s.Name, s.Amount, ordinal(s.DebitDay))
			if s.AccountName != "" {
				line += fmt.Sprintf(" from %s", s.AccountName)
			}
			sb.WriteString(line + "\n")
		}
		return strings.TrimSpace(sb.String())

	case "add_yearly_expense":
		name := a.Name
		if name == "" {
			return "What's the name of this yearly expense?"
		}
		if a.Amount <= 0 {
			return "What's the amount?"
		}
		if a.DueDate == "" {
			return "When is it due? e.g. due_date: \"03-15\" for March 15."
		}
		var dueMonth, dueDay int
		if _, err := fmt.Sscanf(a.DueDate, "%d-%d", &dueMonth, &dueDay); err != nil {
			return fmt.Sprintf("Couldn't parse due_date %q — use MM-DD format.", a.DueDate)
		}
		var accountID int64
		if a.Account != "" {
			acc, err := database.GetAccountByName(userID, a.Account)
			if err == nil {
				accountID = acc.ID
			}
		}
		id, err := database.AddYearlyExpense(userID, accountID, name, a.Amount, dueMonth, dueDay)
		if err != nil {
			return fmt.Sprintf("Couldn't save yearly expense: %v", err)
		}
		return fmt.Sprintf("Yearly expense added (#%d): %s — ₹%.2f due %s/%d.", id, name, a.Amount, monthName(dueMonth), dueDay)

	case "list_yearly_expenses":
		yes, err := database.ListYearlyExpenses(userID)
		if err != nil {
			return "Couldn't fetch yearly expenses."
		}
		if len(yes) == 0 {
			return "No yearly expenses set up yet."
		}
		var sb strings.Builder
		sb.WriteString("Your yearly expenses:\n")
		for _, ye := range yes {
			line := fmt.Sprintf("  #%d %s — ₹%.2f due %s/%d", ye.ID, ye.Name, ye.Amount, monthName(ye.DueMonth), ye.DueDay)
			if ye.AccountName != "" {
				line += fmt.Sprintf(" from %s", ye.AccountName)
			}
			sb.WriteString(line + "\n")
		}
		return strings.TrimSpace(sb.String())

	// --- Time log actions ---

	case "log_time":
		if a.Content == "" {
			return "What did you work on?"
		}
		_, err := database.AddTimeLog(userID, a.Content)
		if err != nil {
			return fmt.Sprintf("Couldn't save time log: %v", err)
		}
		return "Logged ✓"

	case "list_time_logs":
		logs, err := database.ListTimeLogs(userID, 10)
		if err != nil {
			return "Couldn't fetch time logs right now."
		}
		if len(logs) == 0 {
			return "No time logs recorded yet."
		}
		var sb strings.Builder
		sb.WriteString("Your recent time logs:\n")
		for _, tl := range logs {
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", tl.LoggedAt.Format("Jan 2 15:04"), tl.Content))
		}
		return strings.TrimSpace(sb.String())

	default:
		return ""
	}
}

func ordinal(n int) string {
	switch n {
	case 1, 21, 31:
		return fmt.Sprintf("%dst", n)
	case 2, 22:
		return fmt.Sprintf("%dnd", n)
	case 3, 23:
		return fmt.Sprintf("%drd", n)
	default:
		return fmt.Sprintf("%dth", n)
	}
}

func monthName(m int) string {
	if m < 1 || m > 12 {
		return fmt.Sprintf("%d", m)
	}
	return time.Month(m).String()
}
