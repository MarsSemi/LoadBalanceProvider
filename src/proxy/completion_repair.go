package proxy

import (
	"encoding/json"
	"sort"
)

// 只用已完成的項目補缺漏；不以尚在生成的 delta 偽造完整工具參數。
type completionRepair struct {
	items    map[int]map[string]interface{}
	bytes    int
	disabled bool
}

func (r *completionRepair) process(event string) string {
	if r.disabled {
		return event
	}
	for _, payload := range responseEventPayloads(event) {
		kind := stringFromAny(payload["type"])
		if kind == "response.output_item.done" {
			item, ok := payload["item"].(map[string]interface{})
			if !ok {
				continue
			}
			index := numberFieldOrNegative(payload, "output_index")
			if index < 0 {
				continue
			}
			data, _ := json.Marshal(item)
			r.bytes += len(data)
			if r.bytes > 8*1024*1024 {
				r.items = nil
				r.disabled = true
				return event
			}
			if r.items == nil {
				r.items = make(map[int]map[string]interface{})
			}
			r.items[index] = item
		}
		if kind != "response.completed" || len(r.items) == 0 {
			continue
		}
		response, ok := payload["response"].(map[string]interface{})
		if !ok {
			continue
		}
		output, exists := response["output"]
		items, valid := output.([]interface{})
		if exists && output != nil && !valid {
			continue
		}
		changed := false
		if len(items) == 0 {
			indices := make([]int, 0, len(r.items))
			for index := range r.items {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			for _, index := range indices {
				items = append(items, r.items[index])
			}
			response["output"] = items
			changed = true
		} else {
			for index, value := range items {
				item, ok := value.(map[string]interface{})
				cached := r.items[index]
				itemType, _ := item["type"].(string)
				cachedType, _ := cached["type"].(string)
				if !ok || cached == nil || itemType == "" || itemType != cachedType {
					continue
				}
				// 若 call_id 不一致，不依位置推定成同一個工具呼叫。
				if item["call_id"] != nil {
					callID, valid := item["call_id"].(string)
					cachedCallID, _ := cached["call_id"].(string)
					if !valid || callID != cachedCallID {
						continue
					}
				}
				if stringFromAny(item["id"]) == "" && stringFromAny(cached["id"]) != "" {
					item["id"] = cached["id"]
					changed = true
				}
			}
		}
		if changed {
			data, err := json.Marshal(payload)
			if err == nil {
				return "event: response.completed\ndata: " + string(data) + "\n\n"
			}
		}
	}
	return event
}
