package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneOpenAIResponsesRequestSharesInputAndIsolatesPointers(t *testing.T) {
	maxTokens := uint(128)
	stream := true
	src := &OpenAIResponsesRequest{
		Model:           "gpt-5",
		Input:           json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]`),
		MaxOutputTokens: &maxTokens,
		Stream:          &stream,
		Reasoning:       &Reasoning{Effort: "medium", Summary: "auto"},
		StreamOptions:   &StreamOptions{IncludeUsage: true},
	}

	cloned := CloneOpenAIResponsesRequest(src)
	require.NotNil(t, cloned)
	require.NotSame(t, src, cloned)
	require.Greater(t, len(src.Input), 0)
	assert.Equal(t, &src.Input[0], &cloned.Input[0])

	cloned.Reasoning.Effort = "high"
	assert.Equal(t, "medium", src.Reasoning.Effort)

	cloned.StreamOptions.IncludeObfuscation = true
	assert.False(t, src.StreamOptions.IncludeObfuscation)

	*cloned.MaxOutputTokens = 1
	assert.Equal(t, uint(128), *src.MaxOutputTokens)

	*cloned.Stream = false
	assert.True(t, *src.Stream)
}

func TestOpenAIResponsesParseInputSkipsInlineDataURIMedia(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[
			{
				"role":"user",
				"content":[
					{"type":"input_text","text":"describe this"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA"},
					{"type":"input_image","image_url":{"url":"https://example.com/a.png"}},
					{"type":"input_file","file_url":"DATA:application/pdf;base64,BBBB"},
					{"type":"input_file","file_url":{"url":"https://example.com/a.pdf"}}
				]
			}
		]`),
	}

	got := req.ParseInput()
	require.Equal(t, []MediaInput{
		{Type: "input_text", Text: "describe this"},
		{Type: "input_image", ImageUrl: "https://example.com/a.png"},
		{Type: "input_file", FileUrl: "https://example.com/a.pdf"},
	}, got)
}

func TestOpenAIResponsesGetTokenCountMetaSkipsInlineDataURIFiles(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Instructions: json.RawMessage(`"system hint"`),
		Input: json.RawMessage(`[
			{
				"role":"user",
				"content":[
					{"type":"input_text","text":"hello"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA"},
					{"type":"input_image","image_url":"https://example.com/a.png"}
				]
			}
		]`),
	}

	meta := req.GetTokenCountMeta()
	require.NotNil(t, meta)
	assert.Contains(t, meta.CombineText, "hello")
	assert.Contains(t, meta.CombineText, "system hint")
	assert.NotContains(t, meta.CombineText, "data:image/png;base64,AAAA")
	require.Len(t, meta.Files, 1)
	require.NotNil(t, meta.Files[0])
	assert.Equal(t, "https://example.com/a.png", meta.Files[0].GetRawData())
}
