package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"` // Can be null if tool_calls present
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"` // For tool response messages
}

type ToolCall struct {
    ID       string           `json:"id"`
    Type     string           `json:"type"` // "function"
    Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON string
}

type ToolDefinition struct {
    Type     string   `json:"type"` // "function"
    Function Function `json:"function"`
}

type Function struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

type ChatRequest struct {
	Model    string           `json:"model"`
	Messages []ChatMessage    `json:"messages"`
    Tools    []ToolDefinition `json:"tools,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func Generate(baseURL, apiKey, model string, messages []ChatMessage, tools []ToolDefinition) (*ChatMessage, error) {
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(baseURL, "/"))
	
    // Handle cases where baseURL already includes /chat/completions or /v1
    if strings.Contains(baseURL, "/chat/completions") {
        url = baseURL
    }

	reqBody := ChatRequest{
		Model:    model,
		Messages: messages,
        Tools:    tools,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
// ... (rest of the function needs update to return ChatMessage instead of string)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 120 * time.Second} // Increase timeout for tools
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s (status: %d)", string(body), resp.StatusCode)
	}

	var chatResp ChatResponse
	err = json.Unmarshal(body, &chatResp)
	if err != nil {
		return nil, err
	}

	if chatResp.Error != nil {
		return nil, fmt.Errorf("API Error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from AI")
	}

	return &chatResp.Choices[0].Message, nil
}
