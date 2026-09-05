package proxy

import "encoding/json"

// 終止事件沿用已送出的 response ID，並保持序號遞增。
type responseFailureState struct {
	id       string
	sequence float64
}

func (s *responseFailureState) observe(event string) {
	for _, payload := range responseEventPayloads(event) {
		if sequence, ok := payload["sequence_number"].(float64); ok && sequence > s.sequence {
			s.sequence = sequence
		}
		if response, ok := payload["response"].(map[string]interface{}); ok {
			if id, ok := response["id"].(string); ok && id != "" {
				s.id = id
			}
		}
		if id, ok := payload["response_id"].(string); ok && id != "" {
			s.id = id
		}
	}
}

func (s *responseFailureState) decorate(event []byte) []byte {
	for _, payload := range responseEventPayloads(string(event)) {
		if payload["type"] != "response.failed" {
			continue
		}
		if response, ok := payload["response"].(map[string]interface{}); ok && s.id != "" {
			response["id"] = s.id
		}
		payload["sequence_number"] = s.sequence + 1
		data, err := json.Marshal(payload)
		if err == nil {
			return []byte("event: response.failed\ndata: " + string(data) + "\n\n")
		}
	}
	return event
}
