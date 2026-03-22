package groq

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"adityanagar.com/ad-bot/internal/db"
)

const baseURL = "https://api.groq.com/openai/v1/chat/completions"

type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

func New(apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
}

// ---- request structs ----

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type fnParam struct {
	Type        string             `json:"type,omitempty"`
	Description string             `json:"description,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	Properties  map[string]fnParam `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	AnyOf       []fnParam          `json:"anyOf,omitempty"`
}

type fnDef struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Parameters  fnParam `json:"parameters"`
}

type toolDef struct {
	Type     string `json:"type"`
	Function fnDef  `json:"function"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []toolDef     `json:"tools,omitempty"`
}

// ---- response structs ----

type tcFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolCallResp struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Function tcFunction `json:"function"`
}

type respMessage struct {
	Role      string         `json:"role"`
	Content   *string        `json:"content"`
	ToolCalls []toolCallResp `json:"tool_calls"`
}

type respChoice struct {
	Message      respMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type apiError struct {
	Message string `json:"message"`
}

type apiResponse struct {
	Choices []respChoice `json:"choices"`
	Error   *apiError    `json:"error,omitempty"`
}

// ---- public types ----

type Message struct {
	Content string
}

type ToolCall struct {
	Name      string
	Arguments map[string]interface{}
}

type Response struct {
	Message  Message
	ToolCall *ToolCall
}

func (c *Client) Chat(systemPrompt string, history []db.Message, userMsg string) (*Response, error) {
	msgs := []chatMessage{{Role: "system", Content: systemPrompt}}
	for _, m := range history {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: userMsg})

	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: msgs,
		Tools:    allTools(),
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("groq request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result apiResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("groq error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response from groq")
	}

	msg := result.Choices[0].Message

	if len(msg.ToolCalls) > 0 {
		tc := msg.ToolCalls[0]
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return nil, fmt.Errorf("parse tool args: %w", err)
		}
		return &Response{ToolCall: &ToolCall{Name: tc.Function.Name, Arguments: args}}, nil
	}

	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}
	return &Response{Message: Message{Content: content}}, nil
}

func BuildSystemPrompt(goals []db.Goal) string {
	var sb strings.Builder
	sb.WriteString("You are a personal AI assistant. Be concise and helpful. Use the provided tools to perform actions — never describe what you would do, just call the tool directly.")
	if len(goals) > 0 {
		sb.WriteString("\n\nUser's active goals:\n")
		for _, g := range goals {
			sb.WriteString(fmt.Sprintf("- [%s] %s", g.Category, g.Name))
			if g.Target.Valid && g.Target.String != "" {
				sb.WriteString(fmt.Sprintf(" (target: %s)", g.Target.String))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func allTools() []toolDef {
	obj := func(props map[string]fnParam, required ...string) fnParam {
		return fnParam{Type: "object", Properties: props, Required: required}
	}
	str  := func(desc string) fnParam { return fnParam{Type: "string", Description: desc} }
	num  := func(desc string) fnParam {
		return fnParam{Description: desc, AnyOf: []fnParam{{Type: "number"}, {Type: "string"}}}
	}
	intP := func(desc string) fnParam {
		return fnParam{Description: desc, AnyOf: []fnParam{{Type: "integer"}, {Type: "string"}}}
	}
	fn := func(name, desc string, params fnParam) toolDef {
		return toolDef{Type: "function", Function: fnDef{Name: name, Description: desc, Parameters: params}}
	}
	noParams := fnParam{Type: "object", Properties: map[string]fnParam{}}

	return []toolDef{
		// Notes
		fn("add_note", "Save a note",
			obj(map[string]fnParam{"content": str("The note text")}, "content")),
		fn("list_notes", "List recent notes", noParams),
		fn("delete_note", "Delete a note by ID",
			obj(map[string]fnParam{"id": intP("Note ID to delete")}, "id")),

		// Tasks
		fn("add_task", "Add a to-do task",
			obj(map[string]fnParam{"content": str("Task description")}, "content")),
		fn("list_tasks", "List pending tasks", noParams),
		fn("complete_task", "Mark a task as complete",
			obj(map[string]fnParam{"id": intP("Task ID")}, "id")),
		fn("delete_task", "Delete a task",
			obj(map[string]fnParam{"id": intP("Task ID")}, "id")),
		fn("complete_all_tasks", "Mark all pending tasks as complete", noParams),

		// Finance — accounts
		fn("set_account", "Set an account's starting balance — ONLY when user explicitly sets up or resets a starting balance, never just because they mention a number",
			obj(map[string]fnParam{
				"name":    str("Account name e.g. HDFC, SBI"),
				"balance": num("Starting balance amount in rupees"),
			}, "name", "balance")),
		fn("list_accounts", "List all accounts with current balances", noParams),

		// Finance — expenses
		fn("add_expense", "Record money the user spent or paid — use this when user says 'add X on Y', 'spent X', 'paid X', 'X on food', etc. Never use list_transactions for this.",
			obj(map[string]fnParam{
				"amount":   num("Amount spent in rupees"),
				"merchant": str("Shop or vendor name; if no specific merchant just use the category word like 'food' or 'petrol'"),
				"account":  str("Account name used for the payment"),
				"category": str("Spending category e.g. Food, Transport, Entertainment, Medical"),
			}, "amount")),
		fn("get_summary", "Get monthly spending summary, or balance of a specific account",
			obj(map[string]fnParam{
				"account": str("Account name for balance — omit for overall monthly summary"),
			})),
		fn("list_transactions", "Show history of past expenses — use ONLY when user explicitly asks to see, show, or list transactions or history. Never call this to record a new expense.",
			noParams),

		// Finance — SIPs
		fn("add_sip", "Add a monthly SIP or recurring investment",
			obj(map[string]fnParam{
				"name":    str("SIP or fund name"),
				"amount":  num("Monthly debit amount in rupees"),
				"account": str("Debit account name"),
				"day":     intP("Day of month the debit happens"),
			}, "name", "amount", "day")),
		fn("list_sips", "List all SIPs", noParams),

		// Finance — yearly expenses
		fn("add_yearly_expense", "Add a yearly recurring expense like insurance or subscription",
			obj(map[string]fnParam{
				"name":     str("Expense name e.g. Health Insurance"),
				"amount":   num("Annual amount in rupees"),
				"account":  str("Payment account name"),
				"due_date": str("Due date in MM-DD format e.g. 03-15 for March 15"),
			}, "name", "amount", "due_date")),
		fn("list_yearly_expenses", "List all yearly expenses", noParams),

		// Time logging
		fn("log_time", "Log what was worked on in the last hour",
			obj(map[string]fnParam{"content": str("Description of work done")}, "content")),
		fn("list_time_logs", "List recent time log entries", noParams),
	}
}
