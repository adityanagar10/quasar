package ollama

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

type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

func New(baseURL, model string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// --- Message types ---

type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// --- Tool definition types ---

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// --- Request / Response ---

type request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

type Response struct {
	Message Message `json:"message"`
	Error   string  `json:"error,omitempty"`
}

// --- Public API ---

// Chat starts a new turn. Converts DB history to ollama messages and calls the model.
func (c *Client) Chat(systemPrompt string, history []db.Message, userMsg string, tools []Tool) (*Response, error) {
	messages := []Message{{Role: "system", Content: systemPrompt}}
	for _, m := range history {
		messages = append(messages, Message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, Message{Role: "user", Content: userMsg})
	return c.do(messages, tools)
}

// Continue sends an already-built message list back to the model (used after tool execution).
func (c *Client) Continue(messages []Message, tools []Tool) (*Response, error) {
	return c.do(messages, tools)
}

func (c *Client) do(messages []Message, tools []Tool) (*Response, error) {
	body, err := json.Marshal(request{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Post(c.baseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama status %d: %s", resp.StatusCode, string(data))
	}

	var result Response
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("ollama error: %s", result.Error)
	}
	return &result, nil
}

// IsReachable does a quick health check against Ollama.
func (c *Client) IsReachable() bool {
	resp, err := c.http.Get(c.baseURL + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// BuildSystemPrompt builds the system prompt, injecting active goals.
func BuildSystemPrompt(goals []db.Goal) string {
	var sb strings.Builder
	sb.WriteString(`You are a personal AI assistant. Be concise and helpful.

When the user wants to perform one of these actions, respond with ONLY a raw JSON object and nothing else:

Save a note:          {"action":"add_note","content":"<text>"}
List notes:           {"action":"list_notes"}
Delete a note:        {"action":"delete_note","id":<number>}
Add a task:           {"action":"add_task","content":"<text>"}
List tasks:           {"action":"list_tasks"}
Complete a task:      {"action":"complete_task","id":<number>}
Delete a task:        {"action":"delete_task","id":<number>}
Complete all tasks:   {"action":"complete_all_tasks"}

Set account balance:  {"action":"set_account","name":"HDFC","balance":50000}
List accounts:        {"action":"list_accounts"}
Log expense:          {"action":"add_expense","amount":500,"merchant":"XYZ","account":"HDFC","category":"Food"}
Get account balance:  {"action":"get_summary","account":"HDFC"}
Get monthly summary:  {"action":"get_summary","content":"this month"}
Add SIP:              {"action":"add_sip","name":"HDFC MF","amount":5000,"account":"HDFC","day":5}
List SIPs:            {"action":"list_sips"}
Add yearly expense:   {"action":"add_yearly_expense","name":"Health Insurance","amount":15000,"account":"HDFC","due_date":"03-15"}
List yearly expenses: {"action":"list_yearly_expenses"}

Rules:
- Output ONLY the JSON with no extra text when performing an action.
- For regular conversation, respond normally in plain text.
- Never describe or explain JSON — just output it.
- For expenses, extract amount, merchant/shop name, and account name from the user's message.
`)
	if len(goals) > 0 {
		sb.WriteString("\nUser's active goals:\n")
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
