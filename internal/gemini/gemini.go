package gemini

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

const baseURL = "https://generativelanguage.googleapis.com/v1beta/models"

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

// --- request types ---

type part struct {
	Text string `json:"text"`
}

type content struct {
	Role  string `json:"role"`
	Parts []part `json:"parts"`
}

type systemInstruction struct {
	Parts []part `json:"parts"`
}

type genRequest struct {
	SystemInstruction systemInstruction `json:"system_instruction"`
	Contents          []content         `json:"contents"`
}

// --- response types ---

type candidate struct {
	Content content `json:"content"`
}

type apiError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type genResponse struct {
	Candidates []candidate `json:"candidates"`
	Error      *apiError   `json:"error,omitempty"`
}

// --- public types (matches ollama.Response shape used in main.go) ---

type Message struct {
	Content string
}

type Response struct {
	Message Message
}

// Chat sends a conversation turn to Gemini and returns the response.
func (c *Client) Chat(systemPrompt string, history []db.Message, userMsg string) (*Response, error) {
	var contents []content
	for _, m := range history {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, content{
			Role:  role,
			Parts: []part{{Text: m.Content}},
		})
	}
	contents = append(contents, content{
		Role:  "user",
		Parts: []part{{Text: userMsg}},
	})

	body, err := json.Marshal(genRequest{
		SystemInstruction: systemInstruction{Parts: []part{{Text: systemPrompt}}},
		Contents:          contents,
	})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, c.model, c.apiKey)
	resp, err := c.http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result genResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("gemini error %d: %s", result.Error.Code, result.Error.Message)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from gemini")
	}

	return &Response{
		Message: Message{Content: result.Candidates[0].Content.Parts[0].Text},
	}, nil
}

// BuildSystemPrompt builds the system prompt injecting active goals.
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
