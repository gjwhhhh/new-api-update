package conversationaudit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const maxSSEFrameBytes = 1024 * 1024

type conversationProtocol int

const (
	protocolUnknown conversationProtocol = iota
	protocolOpenAIChat
	protocolOpenAIResponses
	protocolClaude
	protocolGemini
	protocolLegacyCompletion
)

type requestCapture struct {
	body         string
	truncated    bool
	captureError string
}

type capturePayload struct {
	SchemaVersion int    `json:"schema_version"`
	CaptureMode   string `json:"capture_mode"`
	UserInput     string `json:"user_input,omitempty"`
	AssistantText string `json:"assistant_text,omitempty"`
}

// CaptureMiddleware is mounted after relay authentication. It reuses the
// upstream BodyStorage, so reading an audit payload never consumes the body
// needed by distributor retries or provider adaptors.
func CaptureMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		protocol := protocolForRequest(c.Request)
		if !isEnabled() || protocol == protocolUnknown {
			c.Next()
			return
		}

		cfg := currentConfig()
		collector := newResponseCollector(protocol, cfg.MaxContentBytes, cfg.MaxParseBytes)
		writer := &captureWriter{ResponseWriter: c.Writer, collector: collector}
		c.Writer = writer

		request := captureRequest(c, protocol, cfg.MaxParseBytes, cfg.MaxContentBytes)
		c.Next()

		responseBody, responseTruncated, errorCode, responseCaptureError := collector.Finalize()
		status := "completed"
		if c.Writer.Status() >= http.StatusBadRequest {
			status = "failed"
		}
		if request.truncated || responseTruncated {
			status = "truncated"
		}
		persist(
			c,
			request.body,
			responseBody,
			errorCode,
			joinCaptureErrors(request.captureError, responseCaptureError),
			request.truncated,
			responseTruncated,
			status,
			c.Writer.Status(),
		)
	}
}

func protocolForRequest(request *http.Request) conversationProtocol {
	if request.Method != http.MethodPost || !strings.HasPrefix(request.Header.Get("Content-Type"), "application/json") {
		return protocolUnknown
	}
	path := request.URL.Path
	switch path {
	case "/v1/chat/completions", "/pg/chat/completions":
		return protocolOpenAIChat
	case "/v1/responses":
		return protocolOpenAIResponses
	case "/v1/messages":
		return protocolClaude
	case "/v1/completions":
		return protocolLegacyCompletion
	}
	if strings.HasPrefix(path, "/v1beta/models/") && (strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent")) {
		return protocolGemini
	}
	return protocolUnknown
}

func captureRequest(c *gin.Context, protocol conversationProtocol, maxParseBytes, maxContentBytes int) requestCapture {
	if protocol == protocolLegacyCompletion {
		return requestCapture{captureError: "unsupported_legacy_prompt"}
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return requestCapture{captureError: "request_body_unavailable"}
	}
	defer func() {
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr == nil {
			c.Request.Body = io.NopCloser(storage)
		}
	}()

	if storage.Size() > int64(maxParseBytes) {
		return requestCapture{captureError: "request_parse_limit_exceeded"}
	}
	if _, err = storage.Seek(0, io.SeekStart); err != nil {
		return requestCapture{captureError: "request_body_unavailable"}
	}
	raw, _, err := readLimited(storage, maxParseBytes)
	if err != nil {
		return requestCapture{captureError: "request_body_unavailable"}
	}

	userText, captureError := extractLatestUserText(protocol, raw)
	if captureError != "" {
		return requestCapture{captureError: captureError}
	}
	body, truncated, err := marshalCapturePayload(capturePayload{
		SchemaVersion: 2,
		CaptureMode:   "latest_user_text",
		UserInput:     userText,
	}, maxContentBytes)
	if err != nil {
		return requestCapture{captureError: "request_encode_failed"}
	}
	return requestCapture{body: body, truncated: truncated}
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

func extractLatestUserText(protocol conversationProtocol, raw []byte) (string, string) {
	switch protocol {
	case protocolOpenAIChat, protocolClaude:
		return extractLastMessageText(raw)
	case protocolOpenAIResponses:
		return extractResponsesInputText(raw)
	case protocolGemini:
		return extractGeminiInputText(raw)
	default:
		return "", "unsupported_request_protocol"
	}
}

func extractLastMessageText(raw []byte) (string, string) {
	var envelope struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if common.Unmarshal(raw, &envelope) != nil {
		return "", "request_parse_failed"
	}
	if len(envelope.Messages) == 0 {
		return "", "no_new_user_text"
	}
	var message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if common.Unmarshal(envelope.Messages[len(envelope.Messages)-1], &message) != nil {
		return "", "request_parse_failed"
	}
	if message.Role != "user" {
		return "", "no_new_user_text"
	}
	text := extractTextContent(message.Content, "text", "input_text")
	if strings.TrimSpace(text) == "" {
		return "", "no_new_user_text"
	}
	return text, ""
}

func extractResponsesInputText(raw []byte) (string, string) {
	var envelope struct {
		Input json.RawMessage `json:"input"`
	}
	if common.Unmarshal(raw, &envelope) != nil || len(envelope.Input) == 0 {
		return "", "request_parse_failed"
	}
	switch common.GetJsonType(envelope.Input) {
	case "string":
		var input string
		if common.Unmarshal(envelope.Input, &input) != nil || !isAuditableText(input) {
			return "", "no_new_user_text"
		}
		return input, ""
	case "array":
		var items []json.RawMessage
		if common.Unmarshal(envelope.Input, &items) != nil {
			return "", "request_parse_failed"
		}
		if len(items) == 0 {
			return "", "no_new_user_text"
		}
		last := items[len(items)-1]
		if common.GetJsonType(last) == "string" {
			var input string
			if common.Unmarshal(last, &input) == nil && isAuditableText(input) {
				return input, ""
			}
			return "", "no_new_user_text"
		}
		var item struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Text    string          `json:"text"`
			Content json.RawMessage `json:"content"`
		}
		if common.Unmarshal(last, &item) != nil {
			return "", "request_parse_failed"
		}
		if item.Type == "input_text" && isAuditableText(item.Text) {
			return item.Text, ""
		}
		if item.Role != "user" {
			return "", "no_new_user_text"
		}
		text := extractTextContent(item.Content, "input_text", "text")
		if strings.TrimSpace(text) == "" {
			return "", "no_new_user_text"
		}
		return text, ""
	default:
		return "", "no_new_user_text"
	}
}

func extractGeminiInputText(raw []byte) (string, string) {
	var envelope struct {
		Contents []json.RawMessage `json:"contents"`
	}
	if common.Unmarshal(raw, &envelope) != nil {
		return "", "request_parse_failed"
	}
	if len(envelope.Contents) == 0 {
		return "", "no_new_user_text"
	}
	var content struct {
		Role  string `json:"role"`
		Parts []struct {
			Text             string          `json:"text"`
			InlineData       json.RawMessage `json:"inlineData"`
			InlineDataSnake  json.RawMessage `json:"inline_data"`
			FileData         json.RawMessage `json:"fileData"`
			FunctionResponse json.RawMessage `json:"functionResponse"`
		} `json:"parts"`
	}
	if common.Unmarshal(envelope.Contents[len(envelope.Contents)-1], &content) != nil {
		return "", "request_parse_failed"
	}
	if content.Role != "" && content.Role != "user" {
		return "", "no_new_user_text"
	}
	var texts []string
	for _, part := range content.Parts {
		if isAuditableText(part.Text) {
			texts = append(texts, part.Text)
		}
	}
	if len(texts) == 0 {
		return "", "no_new_user_text"
	}
	return strings.Join(texts, "\n"), ""
}

func extractTextContent(raw json.RawMessage, allowedTypes ...string) string {
	return extractTypedTextContent(raw, false, allowedTypes...)
}

func extractResponseTextContent(raw json.RawMessage, allowedTypes ...string) string {
	return extractTypedTextContent(raw, true, allowedTypes...)
}

func extractTypedTextContent(raw json.RawMessage, preserveWhitespace bool, allowedTypes ...string) string {
	if len(raw) == 0 {
		return ""
	}
	if common.GetJsonType(raw) == "string" {
		var text string
		if common.Unmarshal(raw, &text) == nil && ((preserveWhitespace && text != "") || isAuditableText(text)) {
			return text
		}
		return ""
	}
	if common.GetJsonType(raw) != "array" {
		return ""
	}
	allowed := make(map[string]struct{}, len(allowedTypes))
	for _, itemType := range allowedTypes {
		allowed[itemType] = struct{}{}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if common.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, block := range blocks {
		if _, ok := allowed[block.Type]; !ok || (!preserveWhitespace && !isAuditableText(block.Text)) || (preserveWhitespace && block.Text == "") {
			continue
		}
		texts = append(texts, block.Text)
	}
	return strings.Join(texts, "\n")
}

func isAuditableText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed != "" && !strings.HasPrefix(strings.ToLower(trimmed), "data:")
}

type captureWriter struct {
	gin.ResponseWriter
	collector *responseCollector
}

func (w *captureWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if n > 0 {
		w.collector.Write(data[:n], strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream"))
	}
	return n, err
}

func (w *captureWriter) WriteString(value string) (int, error) {
	n, err := w.ResponseWriter.WriteString(value)
	if n > 0 {
		w.collector.Write([]byte(value[:n]), strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream"))
	}
	return n, err
}

type responsePartKey struct {
	output  int
	content int
}

type responseCollector struct {
	protocol      conversationProtocol
	maxContent    int
	raw           *limitedBuffer
	decoder       *sseDecoder
	streaming     bool
	parts         map[responsePartKey]*strings.Builder
	textBytes     int
	textTruncated bool
	errorCode     string
	captureError  string
	finalized     bool
}

func newResponseCollector(protocol conversationProtocol, maxContentBytes, maxParseBytes int) *responseCollector {
	collector := &responseCollector{
		protocol:   protocol,
		maxContent: maxContentBytes,
		raw:        newLimitedBuffer(maxParseBytes),
		parts:      make(map[responsePartKey]*strings.Builder),
	}
	collector.decoder = newSSEDecoder(maxSSEFrameBytes, collector.consumeSSEPayload)
	return collector
}

func (c *responseCollector) Write(data []byte, streaming bool) {
	if c.finalized || len(data) == 0 {
		return
	}
	if streaming && !c.streaming {
		c.streaming = true
		if c.raw.buffer.Len() > 0 {
			c.decoder.Write(c.raw.Bytes())
			c.raw = newLimitedBuffer(c.raw.maxBytes)
		}
	}
	if c.streaming {
		c.decoder.Write(data)
		return
	}
	c.raw.Write(data)
}

func (c *responseCollector) Finalize() (string, bool, string, string) {
	if c.finalized {
		return "", c.textTruncated, c.errorCode, c.captureError
	}
	c.finalized = true
	if c.streaming {
		c.decoder.Flush()
		if c.decoder.Oversized() {
			c.captureError = joinCaptureErrors(c.captureError, "response_frame_too_large")
		}
	} else if c.raw.Truncated() {
		c.captureError = joinCaptureErrors(c.captureError, "response_parse_limit_exceeded")
	} else {
		c.consumeNonStreaming(c.raw.Bytes())
	}

	text := c.joinParts()
	if text == "" {
		return "", c.textTruncated, c.errorCode, c.captureError
	}
	body, truncated, err := marshalCapturePayload(capturePayload{
		SchemaVersion: 2,
		CaptureMode:   "assistant_text",
		AssistantText: text,
	}, c.maxContent)
	if err != nil {
		return "", c.textTruncated, c.errorCode, joinCaptureErrors(c.captureError, "response_encode_failed")
	}
	return body, c.textTruncated || truncated, c.errorCode, c.captureError
}

func (c *responseCollector) appendText(key responsePartKey, text string) {
	if text == "" || c.textTruncated {
		return
	}
	remaining := c.maxContent - c.textBytes
	if remaining <= 0 {
		c.textTruncated = true
		return
	}
	part := truncateUTF8(text, remaining)
	if part != text {
		c.textTruncated = true
	}
	if part == "" {
		return
	}
	builder := c.parts[key]
	if builder == nil {
		builder = &strings.Builder{}
		c.parts[key] = builder
	}
	builder.WriteString(part)
	c.textBytes += len(part)
}

func (c *responseCollector) joinParts() string {
	keys := make([]responsePartKey, 0, len(c.parts))
	for key := range c.parts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].output == keys[j].output {
			return keys[i].content < keys[j].content
		}
		return keys[i].output < keys[j].output
	})
	texts := make([]string, 0, len(keys))
	for _, key := range keys {
		if text := c.parts[key].String(); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func (c *responseCollector) consumeSSEPayload(payload []byte) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return
	}
	if code := extractSafeErrorCode(payload); code != "" {
		c.errorCode = code
	}
	switch c.protocol {
	case protocolOpenAIChat:
		var event struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content json.RawMessage `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if common.Unmarshal(payload, &event) != nil {
			return
		}
		for _, choice := range event.Choices {
			if choice.Index == 0 {
				c.appendText(responsePartKey{}, extractResponseTextContent(choice.Delta.Content, "text", "output_text"))
			}
		}
	case protocolOpenAIResponses:
		var event struct {
			Type         string `json:"type"`
			Delta        string `json:"delta"`
			OutputIndex  int    `json:"output_index"`
			ContentIndex int    `json:"content_index"`
		}
		if common.Unmarshal(payload, &event) == nil && event.Type == "response.output_text.delta" {
			c.appendText(responsePartKey{output: event.OutputIndex, content: event.ContentIndex}, event.Delta)
		}
	case protocolClaude:
		var event struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if common.Unmarshal(payload, &event) == nil && event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			c.appendText(responsePartKey{output: event.Index}, event.Delta.Text)
		}
	case protocolGemini:
		c.consumeGeminiResponse(payload)
	}
}

func (c *responseCollector) consumeNonStreaming(raw []byte) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	if code := extractSafeErrorCode(raw); code != "" {
		c.errorCode = code
	}
	switch c.protocol {
	case protocolOpenAIChat:
		var response struct {
			Choices []struct {
				Index   int `json:"index"`
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if common.Unmarshal(raw, &response) != nil {
			c.captureError = joinCaptureErrors(c.captureError, "response_parse_failed")
			return
		}
		for _, choice := range response.Choices {
			if choice.Index == 0 {
				c.appendText(responsePartKey{}, extractResponseTextContent(choice.Message.Content, "text", "output_text"))
			}
		}
	case protocolOpenAIResponses:
		var response struct {
			Output []struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		}
		if common.Unmarshal(raw, &response) != nil {
			c.captureError = joinCaptureErrors(c.captureError, "response_parse_failed")
			return
		}
		for outputIndex, output := range response.Output {
			if output.Type != "message" || (output.Role != "" && output.Role != "assistant") {
				continue
			}
			for contentIndex, content := range output.Content {
				if content.Type == "output_text" {
					c.appendText(responsePartKey{output: outputIndex, content: contentIndex}, content.Text)
				}
			}
		}
	case protocolClaude:
		var response struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if common.Unmarshal(raw, &response) != nil {
			c.captureError = joinCaptureErrors(c.captureError, "response_parse_failed")
			return
		}
		for index, content := range response.Content {
			if content.Type == "text" {
				c.appendText(responsePartKey{output: index}, content.Text)
			}
		}
	case protocolGemini:
		c.consumeGeminiResponse(raw)
	case protocolLegacyCompletion:
		// Legacy completion prompts have no reliable user/system boundary, so
		// neither side is persisted as conversation content.
	}
}

func (c *responseCollector) consumeGeminiResponse(raw []byte) {
	var response struct {
		Candidates []struct {
			Index   int `json:"index"`
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if common.Unmarshal(raw, &response) != nil {
		c.captureError = joinCaptureErrors(c.captureError, "response_parse_failed")
		return
	}
	if len(response.Candidates) == 0 {
		return
	}
	for partIndex, part := range response.Candidates[0].Content.Parts {
		if !part.Thought {
			c.appendText(responsePartKey{content: partIndex}, part.Text)
		}
	}
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

type sseDecoder struct {
	maxFrame      int
	line          []byte
	lineOverflow  bool
	frame         []byte
	frameOverflow bool
	oversized     bool
	handle        func([]byte)
}

func newSSEDecoder(maxFrame int, handle func([]byte)) *sseDecoder {
	return &sseDecoder{maxFrame: maxFrame, handle: handle}
}

func (d *sseDecoder) Write(data []byte) {
	for _, value := range data {
		if value == '\n' {
			d.finishLine()
			continue
		}
		if d.lineOverflow {
			continue
		}
		if len(d.line) >= d.maxFrame {
			d.line = d.line[:0]
			d.lineOverflow = true
			d.frameOverflow = true
			d.oversized = true
			continue
		}
		d.line = append(d.line, value)
	}
}

func (d *sseDecoder) Flush() {
	if len(d.line) > 0 || d.lineOverflow {
		d.finishLine()
	}
	d.finishFrame()
}

func (d *sseDecoder) Oversized() bool {
	return d.oversized
}

func (d *sseDecoder) finishLine() {
	if d.lineOverflow {
		d.lineOverflow = false
		d.line = d.line[:0]
		return
	}
	line := bytes.TrimSuffix(d.line, []byte{'\r'})
	if len(line) == 0 {
		d.finishFrame()
		d.line = d.line[:0]
		return
	}
	if bytes.HasPrefix(line, []byte("data:")) && !d.frameOverflow {
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		extra := len(payload)
		if len(d.frame) > 0 {
			extra++
		}
		if len(d.frame)+extra > d.maxFrame {
			d.frame = d.frame[:0]
			d.frameOverflow = true
			d.oversized = true
		} else {
			if len(d.frame) > 0 {
				d.frame = append(d.frame, '\n')
			}
			d.frame = append(d.frame, payload...)
		}
	}
	d.line = d.line[:0]
}

func (d *sseDecoder) finishFrame() {
	if !d.frameOverflow && len(d.frame) > 0 {
		d.handle(d.frame)
	}
	d.frame = d.frame[:0]
	d.frameOverflow = false
}

func marshalCapturePayload(payload capturePayload, maxBytes int) (string, bool, error) {
	encoded, err := common.Marshal(payload)
	if err != nil {
		return "", false, err
	}
	if len(encoded) <= maxBytes {
		return string(encoded), false, nil
	}

	text := payload.UserInput
	if text == "" {
		text = payload.AssistantText
	}
	low, high := 0, len(text)
	var best []byte
	for low <= high {
		mid := low + (high-low)/2
		candidateText := truncateUTF8(text, mid)
		candidate := payload
		if payload.UserInput != "" {
			candidate.UserInput = candidateText
		} else {
			candidate.AssistantText = candidateText
		}
		candidateEncoded, marshalErr := common.Marshal(candidate)
		if marshalErr != nil {
			return "", false, marshalErr
		}
		if len(candidateEncoded) <= maxBytes {
			best = candidateEncoded
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if best == nil {
		return "", false, io.ErrShortBuffer
	}
	return string(best), true, nil
}

func truncateUTF8(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end]
}

func joinCaptureErrors(errors ...string) string {
	seen := make(map[string]struct{}, len(errors))
	var result []string
	for _, value := range errors {
		for _, code := range strings.Split(value, ",") {
			code = strings.TrimSpace(code)
			if code == "" {
				continue
			}
			if _, exists := seen[code]; exists {
				continue
			}
			seen[code] = struct{}{}
			result = append(result, code)
		}
	}
	return strings.Join(result, ",")
}
