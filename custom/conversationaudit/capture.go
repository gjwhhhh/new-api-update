package conversationaudit

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// CaptureMiddleware is mounted after relay authentication. It reuses the
// upstream BodyStorage, so reading an audit payload never consumes the body
// needed by distributor retries or provider adaptors.
func CaptureMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isEnabled() || !supportsConversationPath(c.Request) {
			c.Next()
			return
		}

		requestBody, requestTruncated := captureRequest(c)
		if requestBody == "" {
			c.Next()
			return
		}

		writer := &captureWriter{
			ResponseWriter: c.Writer,
			buffer:         newLimitedBuffer(currentConfig().MaxContentBytes),
		}
		c.Writer = writer
		c.Next()

		status := "completed"
		if c.Writer.Status() >= http.StatusBadRequest {
			status = "failed"
		}
		if requestTruncated || writer.buffer.Truncated() {
			status = "truncated"
		}
		responseRaw := writer.buffer.Bytes()
		persist(c, requestBody, extractResponse(responseRaw), extractSafeErrorCode(responseRaw), requestTruncated, writer.buffer.Truncated(), status, c.Writer.Status())
	}
}

func supportsConversationPath(request *http.Request) bool {
	if request.Method != http.MethodPost || !strings.HasPrefix(request.Header.Get("Content-Type"), "application/json") {
		return false
	}
	path := request.URL.Path
	if path == "/v1/chat/completions" || path == "/v1/completions" || path == "/v1/responses" || path == "/v1/responses/compact" || path == "/v1/messages" || path == "/pg/chat/completions" {
		return true
	}
	return strings.HasPrefix(path, "/v1beta/models/") && (strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent"))
}

func captureRequest(c *gin.Context) (string, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return "", false
	}
	if _, err = storage.Seek(0, io.SeekStart); err != nil {
		return "", false
	}
	raw, truncated, err := readLimited(storage, currentConfig().MaxContentBytes)
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr == nil {
		c.Request.Body = io.NopCloser(storage)
	}
	if err != nil {
		return "", false
	}
	content := extractRequest(raw)
	if content == "" && truncated {
		return `{"capture":"request payload exceeded audit limit before semantic extraction"}`, true
	}
	return content, truncated
}

type captureWriter struct {
	gin.ResponseWriter
	buffer *limitedBuffer
}

func (w *captureWriter) Write(data []byte) (int, error) {
	w.buffer.Write(data)
	return w.ResponseWriter.Write(data)
}

func (w *captureWriter) WriteString(value string) (int, error) {
	w.buffer.Write([]byte(value))
	return w.ResponseWriter.WriteString(value)
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	maxBytes  int
	truncated bool
}

func newLimitedBuffer(maxBytes int) *limitedBuffer {
	return &limitedBuffer{maxBytes: maxBytes}
}

func (b *limitedBuffer) Write(data []byte) {
	if b.truncated || len(data) == 0 {
		return
	}
	remaining := b.maxBytes - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return
	}
	if len(data) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		b.truncated = true
		return
	}
	_, _ = b.buffer.Write(data)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}

func readLimited(reader io.Reader, maxBytes int) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes)+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

func extractRequest(raw []byte) string {
	return extractJSON(raw, false)
}

func extractResponse(raw []byte) string {
	if content := extractJSON(raw, true); content != "" {
		return content
	}

	var events []any
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var value any
		if common.Unmarshal([]byte(payload), &value) == nil {
			events = append(events, sanitizeValue(value, ""))
		}
	}
	if len(events) == 0 {
		return ""
	}
	encoded, err := common.Marshal(map[string]any{"stream_events": events})
	if err != nil {
		return ""
	}
	return string(encoded)
}

func extractSafeErrorCode(raw []byte) string {
	var value map[string]any
	if common.Unmarshal(raw, &value) != nil {
		return ""
	}
	errorValue, ok := value["error"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"code", "type"} {
		if code, ok := errorValue[key].(string); ok {
			return code
		}
	}
	return ""
}

func extractJSON(raw []byte, response bool) string {
	var value map[string]any
	if common.Unmarshal(raw, &value) != nil {
		return ""
	}
	keys := []string{"messages", "input", "instructions", "system", "system_instruction", "contents", "tools"}
	if response {
		keys = []string{"choices", "output", "content", "candidates", "message", "tool_calls"}
	}
	content := make(map[string]any)
	for _, key := range keys {
		if field, exists := value[key]; exists {
			content[key] = sanitizeValue(field, key)
		}
	}
	if len(content) == 0 {
		return ""
	}
	encoded, err := common.Marshal(content)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func sanitizeValue(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any)
		for childKey, childValue := range typed {
			lower := strings.ToLower(childKey)
			if isSensitiveKey(lower) || isBinaryKey(lower) {
				continue
			}
			result[childKey] = sanitizeValue(childValue, lower)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, sanitizeValue(item, key))
		}
		return result
	case string:
		if strings.HasPrefix(typed, "data:") {
			return "[omitted binary data URI]"
		}
		return typed
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	return strings.Contains(key, "authorization") || strings.Contains(key, "api_key") || strings.Contains(key, "apikey") || strings.Contains(key, "password") || strings.Contains(key, "secret")
}

func isBinaryKey(key string) bool {
	return key == "image_url" || strings.Contains(key, "input_audio") || strings.Contains(key, "file_data") || strings.Contains(key, "binary")
}
