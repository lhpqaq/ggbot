package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"log/slog"

	"github.com/lhpqaq/ggbot/config"
)

// ToolExecutor handles AI tool calling loops
type ToolExecutor struct {
	manager *MCPManager
	logger  *slog.Logger
}

// NewToolExecutor creates a new tool executor
func NewToolExecutor(manager *MCPManager, logger *slog.Logger) *ToolExecutor {
	return &ToolExecutor{
		manager: manager,
		logger:  logger,
	}
}

// ExecuteWithTools executes an AI conversation with tool support
// Returns the final response content or an error
// platformPrompt is applied only to the final response (not during tool calls)
func (e *ToolExecutor) ExecuteWithTools(
	ctx context.Context,
	aiCfg config.AIConfig,
	initialMessages []ChatMessage,
	maxIterations int,
	platformPrompt string,
) (string, error) {
	if maxIterations <= 0 {
		maxIterations = 5
	}

	messages := make([]ChatMessage, len(initialMessages))
	copy(messages, initialMessages)

	tools := e.manager.GetTools()

	for i := 0; i < maxIterations; i++ {
		e.logger.Debug("AI generation iteration", "iteration", i)

		// Generate response
		respMsg, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages, tools)
		if err != nil {
			return "", fmt.Errorf("generation error at iteration %d: %w", i, err)
		}

		messages = append(messages, *respMsg)

		// Check for tool calls
		if len(respMsg.ToolCalls) == 0 {
			// Final response - apply platform-specific prompt if provided
			finalContent := respMsg.Content

			if platformPrompt != "" && finalContent != "" {
				e.logger.Debug("Applying platform prompt for final response")

				// Create a new message with platform-specific instructions
				finalMessages := []ChatMessage{
					{Role: "user", Content: fmt.Sprintf("%s\n\n请按照以下要求重新组织你的回复：%s", finalContent, platformPrompt)},
				}

				// Generate final polished response
				finalResp, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, finalMessages, nil)
				if err != nil {
					e.logger.Warn("Failed to apply platform prompt, using original response", "error", err)
					return finalContent, nil
				}

				return finalResp.Content, nil
			}

			return finalContent, nil
		}

		// Execute tool calls
		if err := e.executeToolCalls(ctx, respMsg.ToolCalls, &messages); err != nil {
			e.logger.Error("Tool execution failed", "error", err)
			return "", err
		}
	}

	// Exceeded max iterations - force final response based on current information
	e.logger.Warn("Exceeded maximum iterations, generating final response based on current information", "max_iterations", maxIterations)

	// Add a message asking AI to summarize based on what it has so far
	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: "请基于目前已经获得的信息，给出你的最终回复。即使信息不完整，也请尽可能给出有用的答案。",
	})

	// Generate final response without tools
	finalResp, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate final response after max iterations: %w", err)
	}

	finalContent := finalResp.Content

	// Apply platform-specific prompt if provided
	if platformPrompt != "" && finalContent != "" {
		e.logger.Debug("Applying platform prompt for final response")

		finalMessages := []ChatMessage{
			{Role: "user", Content: fmt.Sprintf("%s\n\n请按照以下要求重新组织你的回复：%s", finalContent, platformPrompt)},
		}

		polishedResp, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, finalMessages, nil)
		if err != nil {
			e.logger.Warn("Failed to apply platform prompt, using original response", "error", err)
			return finalContent, nil
		}

		return polishedResp.Content, nil
	}

	return finalContent, nil
}

// executeToolCalls executes all tool calls and appends results to messages
func (e *ToolExecutor) executeToolCalls(
	ctx context.Context,
	toolCalls []ToolCall,
	messages *[]ChatMessage,
) error {
	for _, call := range toolCalls {
		// Parse arguments
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			*messages = append(*messages, ChatMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("Error parsing arguments: %v", err),
			})
			continue
		}

		e.logger.Info("Executing tool", "tool", call.Function.Name, "id", call.ID)

		// Execute tool
		contentStr, err := e.manager.CallTool(ctx, call.Function.Name, args)
		if err != nil {
			contentStr = fmt.Sprintf("Error executing tool: %v", err)
			e.logger.Error("Tool execution error", "tool", call.Function.Name, "error", err)
		}

		e.logger.Debug("Tool execution result", "tool", call.Function.Name, "length", len(contentStr))

		// Append result
		*messages = append(*messages, ChatMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    contentStr,
		})
	}

	return nil
}

// marshalSchema safely marshals a tool schema
func marshalSchema(schema interface{}) (json.RawMessage, error) {
	if schema == nil {
		// Return empty object schema if nil
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	return json.RawMessage(schemaBytes), nil
}
