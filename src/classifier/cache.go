package classifier

import (
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
	"sync"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
type Cache struct {
	_lock    sync.Mutex
	_limit   int
	_items   map[string]*list.Element
	_lruList *list.List
}

// -------------------------------------------------------------------------------------
type cacheEntry struct {
	Key     string
	Profile domain.RequestProfile
}

// -------------------------------------------------------------------------------------
func NewCache(_limit int) *Cache {
	if _limit <= 0 {
		_limit = 1000
	}
	return &Cache{
		_limit:   _limit,
		_items:   map[string]*list.Element{},
		_lruList: list.New(),
	}
}

// -------------------------------------------------------------------------------------
func (_c *Cache) Get(_key string) (domain.RequestProfile, bool) {
	if _c == nil || _key == "" {
		return domain.RequestProfile{}, false
	}

	_c._lock.Lock()
	defer _c._lock.Unlock()

	_element := _c._items[_key]
	if _element == nil {
		return domain.RequestProfile{}, false
	}

	_c._lruList.MoveToFront(_element)
	_entry := _element.Value.(cacheEntry)
	return _entry.Profile, true
}

// -------------------------------------------------------------------------------------
func (_c *Cache) Set(_key string, _profile domain.RequestProfile) {
	if _c == nil || _key == "" {
		return
	}

	_c._lock.Lock()
	defer _c._lock.Unlock()

	if _element := _c._items[_key]; _element != nil {
		_element.Value = cacheEntry{Key: _key, Profile: _profile}
		_c._lruList.MoveToFront(_element)
		return
	}

	_element := _c._lruList.PushFront(cacheEntry{Key: _key, Profile: _profile})
	_c._items[_key] = _element

	for len(_c._items) > _c._limit {
		_tail := _c._lruList.Back()
		if _tail == nil {
			break
		}
		_entry := _tail.Value.(cacheEntry)
		delete(_c._items, _entry.Key)
		_c._lruList.Remove(_tail)
	}
}

// -------------------------------------------------------------------------------------
func CacheKey(_messages []domain.ChatMessage) string {
	_text := lastUserMessageText(_messages)
	if _text == "" {
		return ""
	}
	_hash := sha1.Sum([]byte(strings.TrimSpace(normalizeCacheText(_text) + "\n" + messageMediaCacheSignature(_messages))))
	return hex.EncodeToString(_hash[:])
}

// -------------------------------------------------------------------------------------
func RequestCacheKey(_req *domain.ChatCompletionRequest) string {
	if _req == nil {
		return ""
	}
	_text := lastUserMessageText(_req.Messages)
	_signature := strings.TrimSpace(messageMediaCacheSignature(_req.Messages) + "\n" + attachmentMediaCacheSignature(_req.Attachments))
	if _text == "" && _signature == "" {
		return ""
	}
	_hash := sha1.Sum([]byte(strings.TrimSpace(normalizeCacheText(_text) + "\n" + _signature)))
	return hex.EncodeToString(_hash[:])
}

// -------------------------------------------------------------------------------------
func lastUserMessageText(_messages []domain.ChatMessage) string {
	for _idx := len(_messages) - 1; _idx >= 0; _idx-- {
		if strings.EqualFold(_messages[_idx].Role, "user") {
			return _messages[_idx].Text()
		}
	}

	if len(_messages) == 0 {
		return ""
	}
	return _messages[len(_messages)-1].Text()
}

// -------------------------------------------------------------------------------------
func normalizeCacheText(_value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(_value)), " "))
}

// -------------------------------------------------------------------------------------
func messageMediaCacheSignature(_messages []domain.ChatMessage) string {
	_parts := make([]string, 0)
	for _, _message := range _messages {
		_parts = append(_parts, contentMediaCacheSignature(_message.Content)...)
	}
	sort.Strings(_parts)
	return strings.Join(_parts, "|")
}

// -------------------------------------------------------------------------------------
func contentMediaCacheSignature(_content interface{}) []string {
	switch _value := _content.(type) {
	case []interface{}:
		_parts := make([]string, 0)
		for _, _item := range _value {
			_parts = append(_parts, contentMediaCacheSignature(_item)...)
		}
		return _parts
	case map[string]interface{}:
		return mapMediaCacheSignature(_value)
	default:
		return nil
	}
}

// -------------------------------------------------------------------------------------
func mapMediaCacheSignature(_value map[string]interface{}) []string {
	_parts := make([]string, 0)
	_type := strings.ToLower(strings.TrimSpace(rawString(_value["type"])))
	if _type != "" && mediaTypeForCache(_type) != "" {
		_parts = append(_parts, "part:"+mediaTypeForCache(_type))
	}
	for _, _key := range sortedMapKeys(_value) {
		_normalizedKey := strings.ToLower(strings.TrimSpace(_key))
		_mediaType := mediaTypeForCache(_normalizedKey)
		if _mediaType == "" {
			continue
		}
		_parts = append(_parts, "key:"+_mediaType+":"+mediaURLKind(_value[_key]))
	}
	return _parts
}

// -------------------------------------------------------------------------------------
func attachmentMediaCacheSignature(_attachments []domain.ChatAttachment) string {
	if len(_attachments) == 0 {
		return ""
	}
	_parts := make([]string, 0, len(_attachments))
	for _, _attachment := range _attachments {
		_mediaType := strings.ToLower(strings.TrimSpace(_attachment.MediaType))
		if _mediaType == "" {
			_mediaType = mediaTypeFromMIMEForCache(_attachment.MIMEType)
		}
		if _mediaType == "" {
			_mediaType = mediaTypeFromNameForCache(_attachment.Name)
		}
		if _mediaType == "" {
			continue
		}
		_parts = append(_parts, "attachment:"+_mediaType+":"+strings.ToLower(strings.TrimSpace(_attachment.MIMEType))+":"+mediaDataPresence(_attachment.Content, _attachment.FileData))
	}
	sort.Strings(_parts)
	return strings.Join(_parts, "|")
}

// -------------------------------------------------------------------------------------
func mediaTypeForCache(_value string) string {
	_value = strings.ToLower(strings.TrimSpace(_value))
	switch {
	case strings.Contains(_value, "image"):
		return "image"
	case strings.Contains(_value, "audio"):
		return "audio"
	case strings.Contains(_value, "video"):
		return "video"
	default:
		return ""
	}
}

// -------------------------------------------------------------------------------------
func mediaTypeFromMIMEForCache(_mimeType string) string {
	_mimeType = strings.ToLower(strings.TrimSpace(_mimeType))
	for _, _mediaType := range []string{"image", "audio", "video"} {
		if strings.HasPrefix(_mimeType, _mediaType+"/") {
			return _mediaType
		}
	}
	return ""
}

// -------------------------------------------------------------------------------------
func mediaTypeFromNameForCache(_name string) string {
	_name = strings.ToLower(strings.TrimSpace(_name))
	switch {
	case strings.HasSuffix(_name, ".png") || strings.HasSuffix(_name, ".jpg") || strings.HasSuffix(_name, ".jpeg") || strings.HasSuffix(_name, ".webp") || strings.HasSuffix(_name, ".gif"):
		return "image"
	case strings.HasSuffix(_name, ".wav") || strings.HasSuffix(_name, ".mp3") || strings.HasSuffix(_name, ".m4a") || strings.HasSuffix(_name, ".webm"):
		return "audio"
	case strings.HasSuffix(_name, ".mp4") || strings.HasSuffix(_name, ".mov"):
		return "video"
	default:
		return ""
	}
}

// -------------------------------------------------------------------------------------
func mediaURLKind(_value interface{}) string {
	switch _typed := _value.(type) {
	case string:
		return mediaURLStringKind(_typed)
	case map[string]interface{}:
		if _url := rawString(_typed["url"]); _url != "" {
			return mediaURLStringKind(_url)
		}
		if _url := rawString(_typed["data"]); _url != "" {
			return mediaURLStringKind(_url)
		}
		return "object"
	default:
		return "present"
	}
}

// -------------------------------------------------------------------------------------
func mediaURLStringKind(_value string) string {
	_value = strings.ToLower(strings.TrimSpace(_value))
	switch {
	case strings.HasPrefix(_value, "data:image/"):
		return "data:image"
	case strings.HasPrefix(_value, "data:audio/"):
		return "data:audio"
	case strings.HasPrefix(_value, "data:video/"):
		return "data:video"
	case strings.HasPrefix(_value, "http://") || strings.HasPrefix(_value, "https://"):
		return "url"
	default:
		return "data"
	}
}

// -------------------------------------------------------------------------------------
func mediaDataPresence(_values ...string) string {
	for _, _value := range _values {
		if strings.TrimSpace(_value) != "" {
			return "data"
		}
	}
	return "metadata"
}

// -------------------------------------------------------------------------------------
func rawString(_value interface{}) string {
	if _text, _ok := _value.(string); _ok {
		return strings.TrimSpace(_text)
	}
	return ""
}

// -------------------------------------------------------------------------------------
func sortedMapKeys(_value map[string]interface{}) []string {
	_keys := make([]string, 0, len(_value))
	for _key := range _value {
		_keys = append(_keys, _key)
	}
	sort.Strings(_keys)
	return _keys
}

// -------------------------------------------------------------------------------------
