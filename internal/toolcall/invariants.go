package toolcall

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// ValidateToolResultReferences ensures each tool role message references a known prior tool call id.
func ValidateToolResultReferences(openAIRequestJSON []byte) error {
	callIDs := map[string]struct{}{}
	messages := gjson.GetBytes(openAIRequestJSON, "messages").Array()
	for _, msg := range messages {
		for _, tc := range msg.Get("tool_calls").Array() {
			id := strings.TrimSpace(tc.Get("id").String())
			if id != "" {
				callIDs[id] = struct{}{}
			}
		}
		if strings.EqualFold(msg.Get("role").String(), "tool") {
			id := strings.TrimSpace(msg.Get("tool_call_id").String())
			if id == "" {
				return fmt.Errorf("tool message missing tool_call_id")
			}
			if _, ok := callIDs[id]; !ok {
				return fmt.Errorf("tool message references unknown tool_call_id: %s", id)
			}
		}
	}
	return nil
}

// ValidateOpenAIAdjacency enforces that assistant tool_calls are followed by tool messages before next assistant/user.
func ValidateOpenAIAdjacency(openAIRequestJSON []byte) error {
	messages := gjson.GetBytes(openAIRequestJSON, "messages").Array()
	pending := 0
	for i, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Get("role").String()))
		if role == "assistant" {
			if pending > 0 {
				return fmt.Errorf("message[%d] assistant appears before resolving prior tool calls", i)
			}
			pending = len(msg.Get("tool_calls").Array())
			continue
		}
		if role == "tool" {
			if pending == 0 {
				return fmt.Errorf("message[%d] tool appears without pending assistant tool_calls", i)
			}
			pending--
			continue
		}
		if pending > 0 {
			return fmt.Errorf("message[%d] role=%s appears before tool results complete", i, role)
		}
	}
	if pending > 0 {
		return fmt.Errorf("request ended with %d unresolved tool results", pending)
	}
	return nil
}
