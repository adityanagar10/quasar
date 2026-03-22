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

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type choice struct {
	Message message `json:"message"`
}

type apiError struct {
	Message string `json:"message"`
}

type response struct {
	Choices []choice  `json:"choices"`
	Error   *apiError `json:"error,omitempty"`
}

type Message struct {
	Content string
}

type Response struct {
	Message Message
}

func (c *Client) Chat(systemPrompt string, history []db.Message, userMsg string) (*Response, error) {
	messages := []message{{Role: "system", Content: systemPrompt}}
	for _, m := range history {
		messages = append(messages, message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, message{Role: "user", Content: userMsg})

	body, err := json.Marshal(request{Model: c.model, Messages: messages})
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

	var result response
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("groq error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response from groq")
	}

	return &Response{Message: Message{Content: result.Choices[0].Message.Content}}, nil
}

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

Log time:             {"action":"log_time","content":"<what you did>"}
List time logs:       {"action":"list_time_logs"}

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
