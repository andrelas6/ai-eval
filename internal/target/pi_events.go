package target

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type piEvent struct {
	Type       string       `json:"type"`
	Message    piMessage    `json:"message"`
	ToolCallID string       `json:"toolCallId"`
	ToolName   string       `json:"toolName"`
	Result     piToolResult `json:"result"`
	IsError    bool         `json:"isError"`
}

type piMessage struct {
	Role         string      `json:"role"`
	Content      []piContent `json:"content"`
	Usage        piUsage     `json:"usage"`
	StopReason   string      `json:"stopReason"`
	ErrorMessage string      `json:"errorMessage"`
}

type piContent struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type piUsage struct {
	TotalTokens int `json:"totalTokens"`
	Cost        struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

type piToolResult struct {
	Content []piContent `json:"content"`
}

func parsePiEvents(stream []byte) (Output, error) {
	var out Output
	var last piMessage
	sawAssistant, sawEnd := false, false
	type position struct{ turn, call int }
	where := map[string]position{}

	for _, line := range bytes.Split(stream, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev piEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return out, fmt.Errorf("pi printed something that is not JSON: %w", err)
		}
		switch ev.Type {
		case "message_end":
			if ev.Message.Role != "assistant" {
				continue
			}
			sawAssistant = true
			last = ev.Message
			out.Trace.Tokens += ev.Message.Usage.TotalTokens
			out.Trace.Cost += ev.Message.Usage.Cost.Total
			var turn Turn
			for _, c := range ev.Message.Content {
				if c.Type == "toolCall" {
					where[c.ID] = position{len(out.Trace.Turns), len(turn.ToolCalls)}
					turn.ToolCalls = append(turn.ToolCalls, ToolCall{Name: c.Name, Args: c.Arguments})
				}
			}
			out.Trace.Turns = append(out.Trace.Turns, turn)
		case "tool_execution_end":
			text := joinText(ev.Result.Content)
			var found ToolCall
			if ev.IsError {
				found = ToolCall{Name: ev.ToolName, Err: text}
			} else {
				found = ToolCall{Name: ev.ToolName, Result: text}
			}
			pos, ok := where[ev.ToolCallID]
			if ok {
				call := &out.Trace.Turns[pos.turn].ToolCalls[pos.call]
				call.Result, call.Err = found.Result, found.Err
			} else if n := len(out.Trace.Turns); n > 0 {
				out.Trace.Turns[n-1].ToolCalls = append(out.Trace.Turns[n-1].ToolCalls, found)
			} else {
				out.Trace.Turns = append(out.Trace.Turns, Turn{ToolCalls: []ToolCall{found}})
			}
		case "agent_end":
			sawEnd = true
		}
	}

	if !sawEnd {
		return out, errors.New("pi stopped before agent_end, the run is incomplete")
	}
	if !sawAssistant {
		return out, errors.New("pi finished without an assistant message")
	}
	if last.StopReason == "error" {
		return out, fmt.Errorf("pi run failed: %s", last.ErrorMessage)
	}
	if last.StopReason != "stop" && last.StopReason != "toolUse" {
		return out, fmt.Errorf("pi run stopped: %s", last.StopReason)
	}
	out.Answer = joinText(last.Content)
	return out, nil
}

func joinText(content []piContent) string {
	var parts []string
	for _, c := range content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "")
}
