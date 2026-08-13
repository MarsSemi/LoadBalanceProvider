package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"

	_ "golang.org/x/image/webp"

	"LoadBalanceProvider/src/config"
	"LoadBalanceProvider/src/domain"
)

const mcpProtocolVersion = "2025-11-25"

const mcpImagePreviewTotalMaxBytes = 512 * 1024

var mcpSupportedProtocolVersions = map[string]bool{
	"2025-11-25": true,
	"2025-06-18": true,
	"2025-03-26": true,
}

// -------------------------------------------------------------------------------------
type MCPSettingsUpdateRequest struct {
	Enabled        *bool     `json:"enabled"`
	ReadOnly       *bool     `json:"readOnly"`
	AllowedOrigins *[]string `json:"allowedOrigins"`
}

// -------------------------------------------------------------------------------------
type mcpJSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// -------------------------------------------------------------------------------------
type mcpToolDefinition struct {
	Name         string                 `json:"name"`
	Title        string                 `json:"title,omitempty"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"inputSchema"`
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty"`
	Annotations  map[string]interface{} `json:"annotations,omitempty"`
}

// -------------------------------------------------------------------------------------
type mcpToolRoute struct {
	Method                string
	Path                  string
	PathArguments         map[string]string
	QueryArguments        map[string]string
	BodyArgument          string
	RawBodyBase64Argument string
	ContentType           string
	ContentTypeArgument   string
	DynamicPathArgument   string
	AllowedDynamicPaths   map[string]string
	BodyFromArguments     bool
	BodyDefaults          map[string]interface{}
	BodyForcedValues      map[string]interface{}
	RichContent           bool
}

// -------------------------------------------------------------------------------------
type mcpToolSpec struct {
	Definition mcpToolDefinition
	Route      mcpToolRoute
	ReadOnly   bool
}

// -------------------------------------------------------------------------------------
type mcpInternalRequestContextKey struct{}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) handleGetMCPSettings(_w http.ResponseWriter, _r *http.Request) {
	_settings, _err := config.LoadMCPSettingsConfig(_h.mcpSettingsConfigPath())
	if _err != nil {
		_h.writeJSON(_w, http.StatusInternalServerError, domain.ErrorResponse("load_failed", _err.Error()))
		return
	}
	_h.writeJSON(_w, http.StatusOK, map[string]interface{}{
		"mcp": _h.mcpSettingsForm(_settings, _r),
	})
}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) handleSaveMCPSettings(_w http.ResponseWriter, _r *http.Request, _body []byte) {
	var _request MCPSettingsUpdateRequest
	if _err := json.Unmarshal(_body, &_request); _err != nil {
		_h.writeJSON(_w, http.StatusBadRequest, domain.ErrorResponse("invalid_request_error", "MCP settings payload is not valid"))
		return
	}

	_saved, _err := config.LoadMCPSettingsConfig(_h.mcpSettingsConfigPath())
	if _err != nil {
		_h.writeJSON(_w, http.StatusInternalServerError, domain.ErrorResponse("load_failed", _err.Error()))
		return
	}
	if _request.Enabled != nil {
		_saved.Enabled = *_request.Enabled
	}
	if _request.ReadOnly != nil {
		_saved.ReadOnly = *_request.ReadOnly
	}
	if _request.AllowedOrigins != nil {
		_saved.AllowedOrigins = *_request.AllowedOrigins
	}
	if _err := config.SaveMCPSettingsConfig(_h.mcpSettingsConfigPath(), _saved); _err != nil {
		_h.writeJSON(_w, http.StatusBadRequest, domain.ErrorResponse("save_failed", _err.Error()))
		return
	}
	_saved, _ = config.LoadMCPSettingsConfig(_h.mcpSettingsConfigPath())
	_h.writeJSON(_w, http.StatusOK, map[string]interface{}{
		"mcp": _h.mcpSettingsForm(_saved, _r),
	})
}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) mcpSettingsForm(_settings domain.MCPSettingsConfig, _r *http.Request) map[string]interface{} {
	_specs := availableMCPToolSpecs(_settings.ReadOnly)
	_tools := make([]map[string]interface{}, 0, len(_specs))
	for _, _spec := range _specs {
		_tools = append(_tools, map[string]interface{}{
			"name":        _spec.Definition.Name,
			"title":       _spec.Definition.Title,
			"description": _spec.Definition.Description,
			"readOnly":    _spec.ReadOnly,
		})
	}
	return map[string]interface{}{
		"enabled":         _settings.Enabled,
		"readOnly":        _settings.ReadOnly,
		"allowedOrigins":  _settings.AllowedOrigins,
		"endpoint":        mcpEndpointForRequest(_r),
		"protocolVersion": mcpProtocolVersion,
		"transport":       "Streamable HTTP",
		"authentication":  "Bearer or X-API-Key",
		"toolCount":       len(_tools),
		"tools":           _tools,
	}
}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) handleMCP(_w http.ResponseWriter, _r *http.Request, _body []byte) {
	_settings, _err := config.LoadMCPSettingsConfig(_h.mcpSettingsConfigPath())
	if _err != nil {
		writeMCPHTTPError(_w, http.StatusInternalServerError, -32603, "MCP settings could not be loaded", nil)
		return
	}
	if !_settings.Enabled {
		writeMCPHTTPError(_w, http.StatusServiceUnavailable, -32000, "MCP service is disabled", nil)
		return
	}

	switch _r.Method {
	case http.MethodGet:
		_w.Header().Set("Allow", "POST")
		_w.WriteHeader(http.StatusMethodNotAllowed)
		return
	case http.MethodDelete:
		_w.Header().Set("Allow", "POST")
		_w.WriteHeader(http.StatusMethodNotAllowed)
		return
	case http.MethodPost:
	default:
		_w.Header().Set("Allow", "GET, POST")
		_w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	_mediaType, _, _mediaErr := mime.ParseMediaType(_r.Header.Get("Content-Type"))
	if _mediaErr != nil || _mediaType != "application/json" {
		writeMCPHTTPError(_w, http.StatusUnsupportedMediaType, -32600, "Content-Type must be application/json", nil)
		return
	}
	if len(_body) == 0 {
		writeMCPHTTPError(_w, http.StatusBadRequest, -32700, "empty JSON-RPC message", nil)
		return
	}
	if bytes.HasPrefix(bytes.TrimSpace(_body), []byte("[")) {
		writeMCPHTTPError(_w, http.StatusBadRequest, -32600, "JSON-RPC batching is not supported by Streamable HTTP", nil)
		return
	}

	var _request mcpJSONRPCRequest
	if _err := json.Unmarshal(_body, &_request); _err != nil {
		writeMCPHTTPError(_w, http.StatusBadRequest, -32700, "invalid JSON-RPC message", nil)
		return
	}
	if _request.JSONRPC != "2.0" {
		writeMCPHTTPError(_w, http.StatusBadRequest, -32600, "invalid JSON-RPC request", rawMCPID(_request.ID))
		return
	}
	if strings.TrimSpace(_request.Method) == "" {
		if len(_request.Result) > 0 || len(_request.Error) > 0 {
			_w.WriteHeader(http.StatusAccepted)
			return
		}
		writeMCPHTTPError(_w, http.StatusBadRequest, -32600, "invalid JSON-RPC request", rawMCPID(_request.ID))
		return
	}

	if _request.Method != "initialize" {
		_protocolVersion := strings.TrimSpace(_r.Header.Get("MCP-Protocol-Version"))
		if _protocolVersion == "" {
			_protocolVersion = "2025-03-26"
		}
		if !mcpSupportedProtocolVersions[_protocolVersion] {
			writeMCPHTTPError(_w, http.StatusBadRequest, -32602, "unsupported MCP protocol version", rawMCPID(_request.ID))
			return
		}
	}

	if len(_request.ID) == 0 {
		// MCP notifications are accepted without producing a JSON-RPC body.
		_w.WriteHeader(http.StatusAccepted)
		return
	}

	switch _request.Method {
	case "initialize":
		_h.handleMCPInitialize(_w, _request)
	case "ping":
		writeMCPResult(_w, _request.ID, map[string]interface{}{})
	case "tools/list":
		_specs := availableMCPToolSpecs(_settings.ReadOnly)
		_tools := make([]mcpToolDefinition, 0, len(_specs))
		for _, _spec := range _specs {
			_tools = append(_tools, _spec.Definition)
		}
		writeMCPResult(_w, _request.ID, map[string]interface{}{"tools": _tools})
	case "tools/call":
		_h.handleMCPToolCall(_w, _r, _request, _settings)
	default:
		writeMCPError(_w, _request.ID, -32601, "method not found", nil)
	}
}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) handleMCPInitialize(_w http.ResponseWriter, _request mcpJSONRPCRequest) {
	var _params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if _err := json.Unmarshal(_request.Params, &_params); _err != nil || strings.TrimSpace(_params.ProtocolVersion) == "" {
		writeMCPError(_w, _request.ID, -32602, "initialize params are not valid", nil)
		return
	}
	_negotiated := mcpProtocolVersion
	if mcpSupportedProtocolVersions[_params.ProtocolVersion] {
		_negotiated = _params.ProtocolVersion
	}
	writeMCPResult(_w, _request.ID, map[string]interface{}{
		"protocolVersion": _negotiated,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{"listChanged": false},
		},
		"serverInfo": map[string]interface{}{
			"name":        "load-balance-provider",
			"title":       "Load Balance Provider",
			"version":     serviceVersion(),
			"description": "LLM Proxy operations through standard MCP; API key management is intentionally excluded.",
		},
		"instructions": "Use tools to inspect and operate Load Balance Provider. API key management is not exposed through MCP.",
	})
}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) handleMCPToolCall(_w http.ResponseWriter, _source *http.Request, _request mcpJSONRPCRequest, _settings domain.MCPSettingsConfig) {
	var _params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if _err := json.Unmarshal(_request.Params, &_params); _err != nil || strings.TrimSpace(_params.Name) == "" {
		writeMCPError(_w, _request.ID, -32602, "tool call params are not valid", nil)
		return
	}
	if _params.Arguments == nil {
		_params.Arguments = map[string]interface{}{}
	}

	var _selected *mcpToolSpec
	for _, _spec := range availableMCPToolSpecs(_settings.ReadOnly) {
		if _spec.Definition.Name == _params.Name {
			_copy := _spec
			_selected = &_copy
			break
		}
	}
	if _selected == nil {
		writeMCPError(_w, _request.ID, -32602, "unknown or unavailable tool: "+_params.Name, nil)
		return
	}

	_result, _err := _h.invokeMCPTool(_source, *_selected, _params.Arguments)
	if _err != nil {
		writeMCPError(_w, _request.ID, -32602, _err.Error(), nil)
		return
	}
	writeMCPResult(_w, _request.ID, _result)
}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) invokeMCPTool(_source *http.Request, _spec mcpToolSpec, _arguments map[string]interface{}) (map[string]interface{}, error) {
	_route := _spec.Route
	_requestPath := _route.Path
	if _route.DynamicPathArgument != "" {
		_value := mcpArgumentString(_arguments, _route.DynamicPathArgument)
		_allowedPath, _ok := _route.AllowedDynamicPaths[_value]
		if !_ok {
			return nil, fmt.Errorf("argument %s is not supported", _route.DynamicPathArgument)
		}
		_requestPath = _allowedPath
	}
	for _argument, _placeholder := range _route.PathArguments {
		_value := mcpArgumentString(_arguments, _argument)
		if _value == "" {
			return nil, fmt.Errorf("argument %s is required", _argument)
		}
		_requestPath = strings.ReplaceAll(_requestPath, _placeholder, url.PathEscape(_value))
	}

	_query := url.Values{}
	for _argument, _parameter := range _route.QueryArguments {
		if _value, _ok := _arguments[_argument]; _ok && _value != nil && strings.TrimSpace(fmt.Sprint(_value)) != "" {
			_query.Set(_parameter, fmt.Sprint(_value))
		}
	}
	if _encoded := _query.Encode(); _encoded != "" {
		_requestPath += "?" + _encoded
	}

	var _body []byte
	_contentType := _route.ContentType
	if _contentType == "" {
		_contentType = "application/json"
	}
	if _route.ContentTypeArgument != "" {
		if _value := mcpArgumentString(_arguments, _route.ContentTypeArgument); _value != "" {
			_contentType = _value
		}
	}
	if _route.RawBodyBase64Argument != "" && mcpArgumentString(_arguments, _route.RawBodyBase64Argument) != "" {
		_decoded, _err := base64.StdEncoding.DecodeString(mcpArgumentString(_arguments, _route.RawBodyBase64Argument))
		if _err != nil {
			return nil, fmt.Errorf("argument %s must be valid base64", _route.RawBodyBase64Argument)
		}
		_body = _decoded
	} else if _route.BodyFromArguments {
		_payload := make(map[string]interface{}, len(_route.BodyDefaults)+len(_arguments)+len(_route.BodyForcedValues))
		for _key, _value := range _route.BodyDefaults {
			_payload[_key] = _value
		}
		for _key, _value := range _arguments {
			_payload[_key] = _value
		}
		for _key, _value := range _route.BodyForcedValues {
			_payload[_key] = _value
		}
		_encoded, _err := json.Marshal(_payload)
		if _err != nil {
			return nil, fmt.Errorf("tool arguments cannot be encoded")
		}
		_body = _encoded
	} else if _route.BodyArgument != "" {
		_value, _ok := _arguments[_route.BodyArgument]
		if !_ok {
			return nil, fmt.Errorf("argument %s is required", _route.BodyArgument)
		}
		_encoded, _err := json.Marshal(_value)
		if _err != nil {
			return nil, fmt.Errorf("argument %s cannot be encoded", _route.BodyArgument)
		}
		_body = _encoded
	}

	_internalRequest := httptest.NewRequest(_route.Method, _requestPath, bytes.NewReader(_body))
	_internalRequest.Header.Set("Content-Type", _contentType)
	if _source != nil {
		_internalRequest = _internalRequest.WithContext(_source.Context())
	}
	_internalRequest = _internalRequest.WithContext(
		contextWithMCPInternalRequest(_internalRequest),
	)
	_recorder := httptest.NewRecorder()
	_h.Process(_recorder, _internalRequest, nil, nil, nil, string(_body))
	_response := _recorder.Result()
	defer _response.Body.Close()
	return mcpToolResultFromHTTP(_response.StatusCode, _response.Header, _recorder.Body.Bytes()), nil
}

// -------------------------------------------------------------------------------------
func contextWithMCPInternalRequest(_request *http.Request) context.Context {
	return context.WithValue(_request.Context(), mcpInternalRequestContextKey{}, true)
}

// -------------------------------------------------------------------------------------
func mcpToolResultFromHTTP(_status int, _headers http.Header, _body []byte) map[string]interface{} {
	_contentType := strings.TrimSpace(strings.Split(_headers.Get("Content-Type"), ";")[0])
	_selectedHeaders := map[string]interface{}{}
	for _, _name := range []string{"Content-Type", "ETag", "Location", "Retry-After", "X-Proxy-Provider", "X-Proxy-Model", "X-Proxy-Task-Type", "X-Proxy-Strategy"} {
		if _value := _headers.Get(_name); _value != "" {
			_selectedHeaders[_name] = _value
		}
	}

	_structuredBody := interface{}(string(_body))
	_content := make([]map[string]interface{}, 0, 2)
	if strings.HasPrefix(_contentType, "image/") {
		_content = append(_content, mcpImageContentBlock(base64.StdEncoding.EncodeToString(_body), _contentType))
		_structuredBody = map[string]interface{}{"mimeType": _contentType, "size": len(_body), "encoding": "base64"}
	} else if strings.HasPrefix(_contentType, "audio/") {
		_content = append(_content, map[string]interface{}{"type": "audio", "data": base64.StdEncoding.EncodeToString(_body), "mimeType": _contentType})
		_structuredBody = map[string]interface{}{"mimeType": _contentType, "size": len(_body), "encoding": "base64"}
	} else if json.Valid(_body) {
		var _decoded interface{}
		_ = json.Unmarshal(_body, &_decoded)
		if _imageContent, _sanitized, _ok := mcpGeneratedImageContent(_decoded); _ok {
			_structuredBody = _sanitized
			_content = append(_content, _imageContent...)
		} else {
			_structuredBody = _decoded
			_pretty, _ := json.MarshalIndent(_decoded, "", "  ")
			_content = append(_content, map[string]interface{}{"type": "text", "text": string(_pretty)})
		}
	} else if strings.HasPrefix(_contentType, "text/") || strings.Contains(_contentType, "xml") || strings.Contains(_contentType, "javascript") {
		_text := string(_body)
		if _text == "" {
			_text = http.StatusText(_status)
		}
		_content = append(_content, map[string]interface{}{"type": "text", "text": _text})
	} else {
		_encoded := base64.StdEncoding.EncodeToString(_body)
		_structuredBody = map[string]interface{}{"mimeType": _contentType, "size": len(_body), "encoding": "base64", "data": _encoded}
		_summary, _ := json.Marshal(_structuredBody)
		_content = append(_content, map[string]interface{}{"type": "text", "text": string(_summary)})
	}
	if len(_content) == 0 {
		_content = append(_content, map[string]interface{}{"type": "text", "text": fmt.Sprintf("HTTP %d (%d bytes)", _status, len(_body))})
	}

	_result := map[string]interface{}{
		"content": _content,
		"isError": _status < 200 || _status >= 300,
	}
	// Codex 會優先把 structuredContent 轉成一般 function output；若同一份結果
	// 同時包含 image/audio block，rich content 會因此被捨棄，前端只看得到中繼
	// 資料。媒體結果只回傳 content，讓客戶端依 MCP content block 渲染。
	if !mcpContentHasRichMedia(_content) {
		_result["structuredContent"] = map[string]interface{}{
			"status":  _status,
			"headers": _selectedHeaders,
			"body":    _structuredBody,
		}
	}
	return _result
}

// -------------------------------------------------------------------------------------
func mcpContentHasRichMedia(_content []map[string]interface{}) bool {
	for _, _item := range _content {
		_type, _ := _item["type"].(string)
		switch strings.ToLower(strings.TrimSpace(_type)) {
		case "image", "audio":
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func mcpImageContentBlock(_data string, _mimeType string) map[string]interface{} {
	return map[string]interface{}{
		"type":     "image",
		"data":     _data,
		"mimeType": firstNonEmptyMCPImageMIME(_mimeType),
		"_meta": map[string]interface{}{
			"codex/imageDetail": "original",
		},
	}
}

// -------------------------------------------------------------------------------------
// OpenAI-compatible image APIs return JSON even when the actual image is base64.
// Convert b64_json into MCP image blocks so clients can render and persist the result
// directly, while keeping a compact structured result without duplicating base64 data.
func mcpGeneratedImageContent(_decoded interface{}) ([]map[string]interface{}, interface{}, bool) {
	_payload, _ok := _decoded.(map[string]interface{})
	if !_ok {
		return nil, _decoded, false
	}
	_items, _ok := _payload["data"].([]interface{})
	if !_ok || len(_items) == 0 {
		return nil, _decoded, false
	}

	_content := make([]map[string]interface{}, 0, len(_items)+1)
	_sanitizedItems := make([]interface{}, 0, len(_items))
	_hasImageResult := false
	_previewBudget := mcpImagePreviewTotalMaxBytes / len(_items)
	for _idx, _rawItem := range _items {
		_item, _itemOK := _rawItem.(map[string]interface{})
		if !_itemOK {
			_sanitizedItems = append(_sanitizedItems, _rawItem)
			continue
		}
		_sanitizedItem := make(map[string]interface{}, len(_item))
		for _key, _value := range _item {
			_sanitizedItem[_key] = _value
		}

		if _encoded, _ := _item["b64_json"].(string); strings.TrimSpace(_encoded) != "" {
			_imageBytes, _err := base64.StdEncoding.DecodeString(strings.TrimSpace(_encoded))
			if _err == nil && len(_imageBytes) > 0 {
				_originalMIMEType := http.DetectContentType(_imageBytes)
				_previewBytes, _mimeType, _isPreview := compactMCPImage(_imageBytes, _originalMIMEType, _previewBudget)
				_content = append(_content, mcpImageContentBlock(base64.StdEncoding.EncodeToString(_previewBytes), _mimeType))
				delete(_sanitizedItem, "b64_json")
				_sanitizedItem["encoding"] = "base64"
				_sanitizedItem["mime_type"] = _mimeType
				_sanitizedItem["size"] = len(_previewBytes)
				if _isPreview {
					_sanitizedItem["preview"] = true
					_sanitizedItem["original_mime_type"] = _originalMIMEType
					_sanitizedItem["original_size"] = len(_imageBytes)
				}
				_hasImageResult = true
			}
		}
		if _imageURL, _ := _item["url"].(string); strings.TrimSpace(_imageURL) != "" {
			_content = append(_content, map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("Generated image %d: %s", _idx+1, strings.TrimSpace(_imageURL)),
			})
			_hasImageResult = true
		}
		_sanitizedItems = append(_sanitizedItems, _sanitizedItem)
	}
	if !_hasImageResult {
		return nil, _decoded, false
	}

	_sanitizedPayload := make(map[string]interface{}, len(_payload))
	for _key, _value := range _payload {
		_sanitizedPayload[_key] = _value
	}
	_sanitizedPayload["data"] = _sanitizedItems
	return _content, _sanitizedPayload, true
}

// -------------------------------------------------------------------------------------
// Codex 對單一 MCP 工具結果有大小保護。大型 PNG 的 base64 超出限制時，
// 客戶端會把整份結果降級成截斷文字，導致圖片無法顯示。這裡只壓縮 MCP
// 預覽；OpenAI 相容影像 API 的原始回應不受影響。
// isRenderableMCPImageMIME 回報客戶端普遍能直接渲染的格式。
// WebP 等格式雖是合法的 MCP image content，但 Codex 之類的客戶端顯示不出來，
// 會退化成把整包 base64 JSON 印給使用者。
func isRenderableMCPImageMIME(_mimeType string) bool {
	switch strings.ToLower(strings.TrimSpace(_mimeType)) {
	case "image/png", "image/jpeg", "image/jpg", "image/gif":
		return true
	default:
		return false
	}
}

// -------------------------------------------------------------------------------------
func compactMCPImage(_imageBytes []byte, _mimeType string, _maxBytes int) ([]byte, string, bool) {
	if _maxBytes <= 0 {
		_maxBytes = mcpImagePreviewTotalMaxBytes
	}
	_mimeType = firstNonEmptyMCPImageMIME(_mimeType)
	// 格式由上游決定，代理只負責轉成客戶端渲染得出來的格式。
	// 例如 WebP：MCP 客戶端（Codex）顯示不出來，會退化成印出整包 base64 JSON。
	_renderable := isRenderableMCPImageMIME(_mimeType)
	if _renderable && len(_imageBytes) <= _maxBytes {
		return _imageBytes, _mimeType, false
	}

	_source, _, _err := image.Decode(bytes.NewReader(_imageBytes))
	if _err != nil {
		// 解不開就原樣送出，至少不會比丟棄更糟。
		return _imageBytes, _mimeType, false
	}

	// 尺寸沒問題、只是格式不能渲染：直接無損轉 PNG，不動解析度。
	if !_renderable && len(_imageBytes) <= _maxBytes {
		var _buffer bytes.Buffer
		if _err := png.Encode(&_buffer, _source); _err == nil && _buffer.Len() <= _maxBytes {
			return _buffer.Bytes(), "image/png", false
		}
	}

	// 照片類影像用 PNG 幾乎壓不動：相同預算下改用 JPEG 才能保住解析度，
	// 硬要維持 PNG 會讓 1024x1024 被迫降到約一半尺寸。
	_current := _source
	for _scaleStep := 0; _scaleStep < 6; _scaleStep++ {
		for _, _quality := range []int{82, 72, 62, 52, 42} {
			var _buffer bytes.Buffer
			if _err := jpeg.Encode(&_buffer, _current, &jpeg.Options{Quality: _quality}); _err == nil && _buffer.Len() <= _maxBytes {
				return _buffer.Bytes(), "image/jpeg", true
			}
		}
		if !scaleDownMCPImage(&_current) {
			break
		}
	}

	return _imageBytes, firstNonEmptyMCPImageMIME(_mimeType), false
}

// -------------------------------------------------------------------------------------
// scaleDownMCPImage 把影像縮到 3/4；已達下限則回傳 false。
func scaleDownMCPImage(_current *image.Image) bool {
	_bounds := (*_current).Bounds()
	_width := (_bounds.Dx() * 3) / 4
	_height := (_bounds.Dy() * 3) / 4
	if _width < 128 || _height < 128 {
		return false
	}
	*_current = resizeMCPImageNearest(*_current, _width, _height)
	return true
}

// -------------------------------------------------------------------------------------
func resizeMCPImageNearest(_source image.Image, _width int, _height int) image.Image {
	_bounds := _source.Bounds()
	_resized := image.NewRGBA(image.Rect(0, 0, _width, _height))
	for _y := 0; _y < _height; _y++ {
		_sourceY := _bounds.Min.Y + (_y*_bounds.Dy())/_height
		for _x := 0; _x < _width; _x++ {
			_sourceX := _bounds.Min.X + (_x*_bounds.Dx())/_width
			_resized.Set(_x, _y, _source.At(_sourceX, _sourceY))
		}
	}
	return _resized
}

// -------------------------------------------------------------------------------------
func firstNonEmptyMCPImageMIME(_mimeType string) string {
	if strings.HasPrefix(strings.TrimSpace(_mimeType), "image/") {
		return strings.TrimSpace(_mimeType)
	}
	return "image/png"
}

// -------------------------------------------------------------------------------------
func availableMCPToolSpecs(_readOnly bool) []mcpToolSpec {
	_all := mcpToolSpecs()
	_result := make([]mcpToolSpec, 0, len(_all))
	for _, _spec := range _all {
		if _readOnly && !_spec.ReadOnly {
			continue
		}
		_result = append(_result, _spec)
	}
	sort.SliceStable(_result, func(_left int, _right int) bool {
		return _result[_left].Definition.Name < _result[_right].Definition.Name
	})
	return _result
}

// -------------------------------------------------------------------------------------
func mcpToolSpecs() []mcpToolSpec {
	_noArgs := mcpObjectSchema(map[string]interface{}{})
	_string := func(_description string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": _description}
	}
	_object := func(_description string) map[string]interface{} {
		return map[string]interface{}{"type": "object", "description": _description, "additionalProperties": true}
	}
	_arrayOfStrings := func(_description string) map[string]interface{} {
		return map[string]interface{}{"type": "array", "description": _description, "items": map[string]interface{}{"type": "string"}}
	}
	_tool := func(_name string, _title string, _description string, _schema map[string]interface{}, _route mcpToolRoute, _readOnly bool, _destructive bool, _openWorld bool) mcpToolSpec {
		var _outputSchema map[string]interface{}
		if !_route.RichContent {
			_outputSchema = mcpHTTPOutputSchema()
		}
		return mcpToolSpec{
			Definition: mcpToolDefinition{
				Name:         _name,
				Title:        _title,
				Description:  _description,
				InputSchema:  _schema,
				OutputSchema: _outputSchema,
				Annotations: map[string]interface{}{
					"readOnlyHint":    _readOnly,
					"destructiveHint": _destructive,
					"idempotentHint":  _readOnly,
					"openWorldHint":   _openWorld,
				},
			},
			Route:    _route,
			ReadOnly: _readOnly,
		}
	}
	_imageGenerationSchema := mcpObjectSchema(map[string]interface{}{
		"prompt":             _string("Required description of the image to generate"),
		"model":              _string("Optional model name; omit or use AUTO for load balancing"),
		"provider_id":        _string("Optional Provider ID; omit for load balancing"),
		"n":                  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 10, "description": "Number of images to generate"},
		"size":               _string("Optional output size supported by the selected model"),
		"quality":            _string("Optional quality level supported by the selected model"),
		"background":         _string("Optional background mode, such as transparent, opaque, or auto"),
		"output_format":      _string("Optional output format, such as png, jpeg, or webp"),
		"output_compression": map[string]interface{}{"type": "integer", "minimum": 0, "maximum": 100, "description": "Optional output compression percentage"},
		"response_format":    map[string]interface{}{"type": "string", "enum": []string{"b64_json", "url"}, "description": "Optional OpenAI-compatible response format"},
		"moderation":         _string("Optional moderation setting supported by the selected model"),
		"user":               _string("Optional end-user identifier forwarded to the Provider"),
	}, "prompt")
	_imageGenerationRoute := mcpToolRoute{
		Method:            http.MethodPost,
		Path:              "/v1/images/generations",
		BodyFromArguments: true,
		BodyDefaults:      map[string]interface{}{"response_format": "b64_json"},
		BodyForcedValues:  map[string]interface{}{"response_format": "b64_json"},
		RichContent:       true,
	}

	return []mcpToolSpec{
		_tool("service_health", "Service health", "Return service health and Provider runtime status.", _noArgs, mcpRoute(http.MethodGet, "/api/health"), true, false, false),
		_tool("service_version", "Service version", "Return service name, version, and start time.", _noArgs, mcpRoute(http.MethodGet, "/api/version"), true, false, false),
		_tool("models_list", "List models", "List models exposed by the load-balancing strategy.", _noArgs, mcpRoute(http.MethodGet, "/v1/models"), true, false, false),
		_tool("models_get", "Get model", "Retrieve one exposed model by ID.", mcpObjectSchema(map[string]interface{}{"model_id": _string("Model ID")}, "model_id"), mcpToolRoute{Method: http.MethodGet, Path: "/v1/models/{model_id}", PathArguments: map[string]string{"model_id": "{model_id}"}}, true, false, false),
		_tool("providers_status", "Provider status", "Return runtime status and metrics for all Providers.", _noArgs, mcpRoute(http.MethodGet, "/api/providers"), true, false, false),
		_tool("providers_list", "List Provider settings", "Return Provider configurations with secrets masked.", _noArgs, mcpRoute(http.MethodGet, "/api/provider-configs"), true, false, false),
		_tool("providers_create", "Create Provider", "Create a Provider configuration. API keys may be supplied as part of Provider configuration; client API-key management remains excluded.", mcpObjectSchema(map[string]interface{}{"provider": _object("ProviderForm object")}, "provider"), mcpToolRoute{Method: http.MethodPost, Path: "/api/provider-configs", BodyArgument: "provider"}, false, false, true),
		_tool("providers_update", "Update Provider", "Update an existing Provider configuration.", mcpObjectSchema(map[string]interface{}{"provider_id": _string("Provider ID"), "provider": _object("ProviderForm fields to update")}, "provider_id", "provider"), mcpToolRoute{Method: http.MethodPut, Path: "/api/provider-configs/{provider_id}", PathArguments: map[string]string{"provider_id": "{provider_id}"}, BodyArgument: "provider"}, false, false, true),
		_tool("providers_delete", "Delete Provider", "Delete a Provider configuration.", mcpObjectSchema(map[string]interface{}{"provider_id": _string("Provider ID")}, "provider_id"), mcpToolRoute{Method: http.MethodDelete, Path: "/api/provider-configs/{provider_id}", PathArguments: map[string]string{"provider_id": "{provider_id}"}}, false, true, true),
		_tool("providers_reorder", "Reorder Providers", "Set Provider priority order by ID.", mcpObjectSchema(map[string]interface{}{"order": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"ids": _arrayOfStrings("Provider IDs in desired order")}, "required": []string{"ids"}, "additionalProperties": false}}, "order"), mcpToolRoute{Method: http.MethodPost, Path: "/api/provider-configs/reorder", BodyArgument: "order"}, false, false, false),
		_tool("providers_refresh_models", "Refresh Provider models", "Fetch and synchronize models from one Provider.", mcpObjectSchema(map[string]interface{}{"provider_id": _string("Provider ID")}, "provider_id"), mcpToolRoute{Method: http.MethodGet, Path: "/api/provider-configs/{provider_id}/models", PathArguments: map[string]string{"provider_id": "{provider_id}"}}, false, false, true),
		_tool("providers_test", "Test Provider", "Run a minimal chat request and model discovery against one Provider.", mcpObjectSchema(map[string]interface{}{"provider_id": _string("Provider ID")}, "provider_id"), mcpToolRoute{Method: http.MethodPost, Path: "/api/provider-configs/{provider_id}/test", PathArguments: map[string]string{"provider_id": "{provider_id}"}}, false, false, true),
		_tool("providers_oauth_start", "Start Provider OAuth", "Start the OAuth flow for a supported Provider.", mcpObjectSchema(map[string]interface{}{"request": _object("Object containing id, flow_preference, and launch_browser")}, "request"), mcpToolRoute{Method: http.MethodPost, Path: "/api/provider/oauth/start", BodyArgument: "request"}, false, false, true),
		_tool("providers_oauth_status", "Provider OAuth status", "Read the OAuth flow status for a Provider.", mcpObjectSchema(map[string]interface{}{"provider_id": _string("Provider ID")}, "provider_id"), mcpToolRoute{Method: http.MethodGet, Path: "/api/provider/oauth/status", QueryArguments: map[string]string{"provider_id": "id"}}, true, false, true),
		_tool("providers_oauth_complete", "Complete Provider OAuth", "Complete a Provider OAuth flow with a redirect URL or authorization code.", mcpObjectSchema(map[string]interface{}{"request": _object("Object containing id and input")}, "request"), mcpToolRoute{Method: http.MethodPost, Path: "/api/provider/oauth/complete", BodyArgument: "request"}, false, false, true),
		_tool("dashboard_baselines_get", "Dashboard baselines", "Return dashboard metric baselines.", _noArgs, mcpRoute(http.MethodGet, "/api/dashboard-metric-baselines"), true, false, false),
		_tool("dashboard_baselines_reset", "Reset dashboard baselines", "Update dashboard metric baselines for supplied Providers.", mcpObjectSchema(map[string]interface{}{"baselines": _object("Object containing a providers baseline map")}, "baselines"), mcpToolRoute{Method: http.MethodPost, Path: "/api/dashboard-metric-baselines/reset", BodyArgument: "baselines"}, false, true, false),
		_tool("provider_usage_get", "Provider usage", "Return daily Provider usage for a month in YYYY-MM format.", mcpObjectSchema(map[string]interface{}{"month": _string("Optional month in YYYY-MM format")}), mcpToolRoute{Method: http.MethodGet, Path: "/api/provider-usage", QueryArguments: map[string]string{"month": "month"}}, true, false, false),
		_tool("settings_general_get", "General settings", "Return general application settings.", _noArgs, mcpRoute(http.MethodGet, "/api/settings/general"), true, false, false),
		_tool("settings_general_update", "Update general settings", "Update general application settings.", mcpObjectSchema(map[string]interface{}{"settings": _object("General settings object")}, "settings"), mcpToolRoute{Method: http.MethodPut, Path: "/api/settings/general", BodyArgument: "settings"}, false, false, false),
		_tool("settings_advanced_get", "Advanced settings", "Return advanced routing settings.", _noArgs, mcpRoute(http.MethodGet, "/api/settings/advanced"), true, false, false),
		_tool("settings_advanced_update", "Update advanced settings", "Partially update advanced routing settings.", mcpObjectSchema(map[string]interface{}{"settings": _object("Advanced settings fields")}, "settings"), mcpToolRoute{Method: http.MethodPut, Path: "/api/settings/advanced", BodyArgument: "settings"}, false, false, false),
		_tool("settings_notification_get", "Notification settings", "Return the notification target with its secret masked.", _noArgs, mcpRoute(http.MethodGet, "/api/settings/notification"), true, false, false),
		_tool("settings_notification_update", "Update notification settings", "Update the notification target.", mcpObjectSchema(map[string]interface{}{"target": _object("Notification target form")}, "target"), mcpToolRoute{Method: http.MethodPut, Path: "/api/settings/notification", BodyArgument: "target"}, false, false, true),
		_tool("settings_notification_test", "Test notification", "Send a test message to the supplied notification target.", mcpObjectSchema(map[string]interface{}{"target": _object("Notification target form")}, "target"), mcpToolRoute{Method: http.MethodPost, Path: "/api/settings/notification/test", BodyArgument: "target"}, false, false, true),
		_tool("settings_mcp_get", "MCP settings", "Return MCP endpoint settings and available tools.", _noArgs, mcpRoute(http.MethodGet, "/api/settings/mcp"), true, false, false),
		_tool("settings_mcp_update", "Update MCP settings", "Update MCP enablement, read-only mode, and allowed browser origins.", mcpObjectSchema(map[string]interface{}{"settings": _object("MCP settings object")}, "settings"), mcpToolRoute{Method: http.MethodPut, Path: "/api/settings/mcp", BodyArgument: "settings"}, false, false, false),
		_tool("benchmarks_catalog", "Benchmark catalog", "List available intelligence benchmark datasets.", _noArgs, mcpRoute(http.MethodGet, "/api/benchmarks/intelligence/catalog"), true, false, false),
		_tool("benchmarks_start", "Start benchmark", "Start an intelligence benchmark job.", mcpObjectSchema(map[string]interface{}{"request": _object("Benchmark start request")}, "request"), mcpToolRoute{Method: http.MethodPost, Path: "/api/benchmarks/intelligence", BodyArgument: "request"}, false, false, true),
		_tool("benchmarks_get", "Get benchmark", "Return benchmark job status and results.", mcpObjectSchema(map[string]interface{}{"job_id": _string("Benchmark job ID")}, "job_id"), mcpToolRoute{Method: http.MethodGet, Path: "/api/benchmarks/intelligence/{job_id}", PathArguments: map[string]string{"job_id": "{job_id}"}}, true, false, false),
		_tool("benchmarks_cancel", "Cancel benchmark", "Cancel a running benchmark job.", mcpObjectSchema(map[string]interface{}{"job_id": _string("Benchmark job ID")}, "job_id"), mcpToolRoute{Method: http.MethodPost, Path: "/api/benchmarks/intelligence/{job_id}/cancel", PathArguments: map[string]string{"job_id": "{job_id}"}}, false, true, false),
		_tool("system_resources_usage", "System resource usage", "Query sampled CPU, memory, disk, network, and GPU usage.", mcpObjectSchema(map[string]interface{}{"date": _string("Date or period anchor"), "mode": map[string]interface{}{"type": "string", "enum": []string{"day", "week", "month"}}}), mcpToolRoute{Method: http.MethodGet, Path: "/api/system/resources/usage", QueryArguments: map[string]string{"date": "date", "mode": "mode"}}, true, false, false),
		_tool("system_resources_details", "System resource details", "Return current host and runtime details.", _noArgs, mcpRoute(http.MethodGet, "/api/system/resources/details"), true, false, false),
		_tool("system_update_status", "System update status", "Return the current system-update status.", _noArgs, mcpRoute(http.MethodGet, "/api/system/update/status"), true, false, false),
		_tool("system_update_create_session", "Create update upload session", "Create a chunked ZIP update upload session.", mcpObjectSchema(map[string]interface{}{"upload": _object("Object containing file_name and total_size")}, "upload"), mcpToolRoute{Method: http.MethodPost, Path: "/api/system/update/session", BodyArgument: "upload"}, false, false, false),
		_tool("system_update_upload_chunk", "Upload update chunk", "Append one base64-encoded binary chunk to an update upload session.", mcpObjectSchema(map[string]interface{}{"session_id": _string("Upload session ID"), "index": map[string]interface{}{"type": "integer", "minimum": 0}, "offset": map[string]interface{}{"type": "integer", "minimum": 0}, "data_base64": _string("Base64-encoded chunk data")}, "session_id", "index", "offset", "data_base64"), mcpToolRoute{Method: http.MethodPost, Path: "/api/system/update/chunk", QueryArguments: map[string]string{"session_id": "session_id", "index": "index", "offset": "offset"}, RawBodyBase64Argument: "data_base64", ContentType: "application/octet-stream"}, false, false, false),
		_tool("system_update_upload", "Upload system update", "Upload and launch one complete multipart ZIP update. Chunked upload is preferred for large files.", mcpObjectSchema(map[string]interface{}{"body_base64": _string("Base64-encoded complete multipart request body"), "content_type": _string("Multipart Content-Type including boundary")}, "body_base64", "content_type"), mcpToolRoute{Method: http.MethodPost, Path: "/api/system/update", RawBodyBase64Argument: "body_base64", ContentTypeArgument: "content_type"}, false, true, false),
		_tool("system_update_commit", "Commit system update", "Validate the uploaded ZIP and launch the system update.", mcpObjectSchema(map[string]interface{}{"upload": _object("Object containing session_id and file_name")}, "upload"), mcpToolRoute{Method: http.MethodPost, Path: "/api/system/update/commit", BodyArgument: "upload"}, false, true, false),
		_tool("chat_completions_create", "Create chat completion", "Send an OpenAI-compatible Chat Completions request through load balancing. Set stream=false for a JSON MCP result.", mcpObjectSchema(map[string]interface{}{"request": _object("OpenAI-compatible chat completion request")}, "request"), mcpToolRoute{Method: http.MethodPost, Path: "/v1/chat/completions", BodyArgument: "request"}, false, false, true),
		_tool("responses_create", "Create response", "Send an OpenAI-compatible Responses request through load balancing. Set stream=false for a JSON MCP result.", mcpObjectSchema(map[string]interface{}{"request": _object("OpenAI-compatible Responses request")}, "request"), mcpToolRoute{Method: http.MethodPost, Path: "/v1/responses", BodyArgument: "request"}, false, false, true),
		_tool("responses_input_tokens", "Count response input tokens", "Count input tokens for an OpenAI-compatible Responses payload.", mcpObjectSchema(map[string]interface{}{"request": _object("Responses input-token request")}, "request"), mcpToolRoute{Method: http.MethodPost, Path: "/v1/responses/input_tokens", BodyArgument: "request"}, true, false, false),
		_tool("responses_get", "Get response", "Retrieve a stored upstream response.", mcpObjectSchema(map[string]interface{}{"response_id": _string("Response ID"), "include": _string("Optional comma-separated fields to include")}, "response_id"), mcpToolRoute{Method: http.MethodGet, Path: "/v1/responses/{response_id}", PathArguments: map[string]string{"response_id": "{response_id}"}, QueryArguments: map[string]string{"include": "include"}}, true, false, true),
		_tool("responses_delete", "Delete response", "Delete a stored upstream response.", mcpObjectSchema(map[string]interface{}{"response_id": _string("Response ID")}, "response_id"), mcpToolRoute{Method: http.MethodDelete, Path: "/v1/responses/{response_id}", PathArguments: map[string]string{"response_id": "{response_id}"}}, false, true, true),
		_tool("responses_cancel", "Cancel response", "Cancel a running upstream response.", mcpObjectSchema(map[string]interface{}{"response_id": _string("Response ID")}, "response_id"), mcpToolRoute{Method: http.MethodPost, Path: "/v1/responses/{response_id}/cancel", PathArguments: map[string]string{"response_id": "{response_id}"}}, false, true, true),
		_tool("responses_compact", "Compact response context", "Compact a Responses input payload.", mcpObjectSchema(map[string]interface{}{"request": _object("Compaction request")}, "request"), mcpToolRoute{Method: http.MethodPost, Path: "/v1/responses/compact", BodyArgument: "request"}, false, false, true),
		_tool("responses_compact_stored", "Compact stored response", "Compact the context of an existing stored response.", mcpObjectSchema(map[string]interface{}{"response_id": _string("Response ID"), "request": _object("Compaction request")}, "response_id", "request"), mcpToolRoute{Method: http.MethodPost, Path: "/v1/responses/{response_id}/compact", PathArguments: map[string]string{"response_id": "{response_id}"}, BodyArgument: "request"}, false, false, true),
		_tool("responses_input_items", "List response input items", "List input items for a stored response.", mcpObjectSchema(map[string]interface{}{"response_id": _string("Response ID"), "after": _string("Optional pagination cursor"), "limit": map[string]interface{}{"type": "integer", "minimum": 1}, "order": _string("Optional sort order")}, "response_id"), mcpToolRoute{Method: http.MethodGet, Path: "/v1/responses/{response_id}/input_items", PathArguments: map[string]string{"response_id": "{response_id}"}, QueryArguments: map[string]string{"after": "after", "limit": "limit", "order": "order"}}, true, false, true),
		_tool("responses_output_items", "List response output items", "List output items for a stored response.", mcpObjectSchema(map[string]interface{}{"response_id": _string("Response ID"), "after": _string("Optional pagination cursor"), "limit": map[string]interface{}{"type": "integer", "minimum": 1}, "order": _string("Optional sort order")}, "response_id"), mcpToolRoute{Method: http.MethodGet, Path: "/v1/responses/{response_id}/output_items", PathArguments: map[string]string{"response_id": "{response_id}"}, QueryArguments: map[string]string{"after": "after", "limit": "limit", "order": "order"}}, true, false, true),
		_tool("image_gen", "Generate image", "Generate one or more images through an enabled image-generation Provider. The result is returned as MCP image content when the Provider supplies base64 image data.", _imageGenerationSchema, _imageGenerationRoute, false, false, true),
		_tool("image_generate", "Generate image (legacy alias)", "Compatibility alias for image_gen.", _imageGenerationSchema, _imageGenerationRoute, false, false, true),
		_tool("multimodal_request", "Multimodal API request", "Call a supported image, audio, or video endpoint. Supply payload for JSON or body_base64 plus content_type for multipart/binary bodies.", mcpObjectSchema(map[string]interface{}{"endpoint": map[string]interface{}{"type": "string", "enum": []string{"images_generate", "images_edit", "images_variation", "audio_transcribe", "audio_translate", "audio_speech", "video_analyze", "video_generate"}}, "payload": _object("JSON request payload"), "body_base64": _string("Optional base64-encoded raw request body"), "content_type": _string("Request media type when body_base64 is supplied")}, "endpoint"), mcpToolRoute{Method: http.MethodPost, DynamicPathArgument: "endpoint", AllowedDynamicPaths: map[string]string{"images_generate": "/v1/images/generations", "images_edit": "/v1/images/edits", "images_variation": "/v1/images/variations", "audio_transcribe": "/v1/audio/transcriptions", "audio_translate": "/v1/audio/translations", "audio_speech": "/v1/audio/speech", "video_analyze": "/v1/videos/analysis", "video_generate": "/v1/videos/generations"}, BodyArgument: "payload", RawBodyBase64Argument: "body_base64", ContentTypeArgument: "content_type", RichContent: true}, false, false, true),
	}
}

// -------------------------------------------------------------------------------------
func mcpRoute(_method string, _path string) mcpToolRoute {
	return mcpToolRoute{Method: _method, Path: _path}
}

// -------------------------------------------------------------------------------------
func mcpObjectSchema(_properties map[string]interface{}, _required ...string) map[string]interface{} {
	_schema := map[string]interface{}{
		"type":                 "object",
		"properties":           _properties,
		"additionalProperties": false,
	}
	if len(_required) > 0 {
		_schema["required"] = _required
	}
	return _schema
}

// -------------------------------------------------------------------------------------
func mcpHTTPOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status":  map[string]interface{}{"type": "integer"},
			"headers": map[string]interface{}{"type": "object", "additionalProperties": true},
			"body":    map[string]interface{}{},
		},
		"required":             []string{"status", "headers", "body"},
		"additionalProperties": false,
	}
}

// -------------------------------------------------------------------------------------
func mcpArgumentString(_arguments map[string]interface{}, _name string) string {
	_value, _ok := _arguments[_name]
	if !_ok || _value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(_value))
}

// -------------------------------------------------------------------------------------
func mcpEndpointForRequest(_r *http.Request) string {
	_prefix := ""
	if _r != nil && _r.URL != nil {
		_path := strings.TrimSpace(_r.URL.Path)
		for _, _marker := range []string{"/api/", "/v1/"} {
			if _idx := strings.LastIndex(_path, _marker); _idx >= 0 {
				_prefix = strings.TrimRight(_path[:_idx], "/")
				break
			}
		}
	}
	return _prefix + "/mcp/"
}

// -------------------------------------------------------------------------------------
func isMCPRoute(_route string) bool {
	_route = strings.TrimRight(strings.TrimSpace(_route), "/")
	return _route == "/mcp" || strings.HasSuffix(_route, "/mcp")
}

// -------------------------------------------------------------------------------------
func isMCPInternalRequest(_r *http.Request) bool {
	if _r == nil {
		return false
	}
	_value, _ := _r.Context().Value(mcpInternalRequestContextKey{}).(bool)
	return _value
}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) mcpOriginAllowed(_r *http.Request) (bool, error) {
	if _r == nil {
		return false, nil
	}
	_origin := strings.TrimRight(strings.TrimSpace(_r.Header.Get("Origin")), "/")
	if _origin == "" {
		return true, nil
	}
	_settings, _err := config.LoadMCPSettingsConfig(_h.mcpSettingsConfigPath())
	if _err != nil {
		return false, _err
	}
	_parsed, _err := url.Parse(_origin)
	if _err != nil || _parsed.Host == "" {
		return false, nil
	}
	_requestScheme := "http"
	if _r.TLS != nil {
		_requestScheme = "https"
	}
	if _forwarded := strings.ToLower(strings.TrimSpace(strings.Split(_r.Header.Get("X-Forwarded-Proto"), ",")[0])); _forwarded == "http" || _forwarded == "https" {
		_requestScheme = _forwarded
	}
	if strings.EqualFold(_parsed.Host, _r.Host) && strings.EqualFold(_parsed.Scheme, _requestScheme) {
		return true, nil
	}
	for _, _allowed := range _settings.AllowedOrigins {
		if strings.EqualFold(strings.TrimRight(_allowed, "/"), _origin) {
			return true, nil
		}
	}
	return false, nil
}

// -------------------------------------------------------------------------------------
func (_h *HTTPAPI) mcpSettingsConfigPath() string {
	if _h != nil && strings.TrimSpace(_h.MCPSettingsConfigPath) != "" {
		return _h.MCPSettingsConfigPath
	}
	return "data/mcp_settings.json"
}

// -------------------------------------------------------------------------------------
func rawMCPID(_id json.RawMessage) interface{} {
	if len(_id) == 0 {
		return nil
	}
	var _decoded interface{}
	if _err := json.Unmarshal(_id, &_decoded); _err != nil {
		return nil
	}
	return _decoded
}

// -------------------------------------------------------------------------------------
func writeMCPResult(_w http.ResponseWriter, _id json.RawMessage, _result interface{}) {
	writeMCPJSON(_w, http.StatusOK, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      rawMCPID(_id),
		"result":  _result,
	})
}

// -------------------------------------------------------------------------------------
func writeMCPError(_w http.ResponseWriter, _id json.RawMessage, _code int, _message string, _data interface{}) {
	_payload := map[string]interface{}{"code": _code, "message": _message}
	if _data != nil {
		_payload["data"] = _data
	}
	writeMCPJSON(_w, http.StatusOK, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      rawMCPID(_id),
		"error":   _payload,
	})
}

// -------------------------------------------------------------------------------------
func writeMCPHTTPError(_w http.ResponseWriter, _status int, _code int, _message string, _id interface{}) {
	writeMCPJSON(_w, _status, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      _id,
		"error": map[string]interface{}{
			"code":    _code,
			"message": _message,
		},
	})
}

// -------------------------------------------------------------------------------------
func writeMCPJSON(_w http.ResponseWriter, _status int, _payload interface{}) {
	_w.Header().Set("Content-Type", "application/json")
	_w.Header().Set("MCP-Protocol-Version", mcpProtocolVersion)
	_w.WriteHeader(_status)
	_ = json.NewEncoder(_w).Encode(_payload)
}
