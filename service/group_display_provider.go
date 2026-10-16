/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"strings"
)

type automaticGroupDisplayProviderRule struct {
	categoryID string
	keywords   []string
}

var automaticGroupDisplayProviderRules = []automaticGroupDisplayProviderRule{
	{categoryID: "provider:openai", keywords: []string{"openai", "gpt", "chatgpt", "codex", "o1", "o3", "o4", "sora", "dall", "whisper", "tts"}},
	{categoryID: "provider:anthropic", keywords: []string{"anthropic", "claude"}},
	{categoryID: "provider:gemini", keywords: []string{"gemini", "gemma", "imagen", "veo", "learnlm"}},
	{categoryID: "provider:xai", keywords: []string{"xai", "grok"}},
	{categoryID: "provider:domestic", keywords: []string{"360gpt", "baidu", "wenxin", "ernie", "baichuan", "deepseek", "doubao", "volcengine", "seedance", "seedream", "iflytek", "sparkdesk", "internlm", "minimax", "abab", "hailuo", "mimo", "xiaomi", "moonshot", "kimi", "qwen", "qwq", "qvq", "tongyi", "stepfun", "tencent", "hunyuan", "yi", "zhipu", "glm", "chatglm"}},
	{categoryID: "provider:mistral", keywords: []string{"mistral", "mixtral", "codestral", "ministral", "pixtral"}},
	{categoryID: "provider:meta", keywords: []string{"meta", "llama"}},
	{categoryID: "provider:perplexity", keywords: []string{"perplexity", "sonar"}},
	{categoryID: "provider:nvidia", keywords: []string{"nvidia", "nemotron"}},
	{categoryID: "provider:microsoft", keywords: []string{"microsoft", "phi"}},
	{categoryID: "provider:amazon", keywords: []string{"amazon", "nova", "titan"}},
	{categoryID: "provider:cohere", keywords: []string{"cohere", "command"}},
	{categoryID: "provider:stability-ai", keywords: []string{"stability", "sdxl"}},
	{categoryID: "provider:midjourney", keywords: []string{"midjourney"}},
	{categoryID: "provider:kling", keywords: []string{"kling"}},
	{categoryID: "provider:vidu", keywords: []string{"vidu"}},
	{categoryID: "provider:suno", keywords: []string{"suno"}},
	{categoryID: "provider:wan", keywords: []string{"wan"}},
}

func ResolveAutomaticGroupDisplayCategoryID(groupName string) string {
	tokens := strings.FieldsFunc(strings.ToLower(groupName), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, rule := range automaticGroupDisplayProviderRules {
		for _, keyword := range rule.keywords {
			for _, token := range tokens {
				if token == keyword || (len(keyword) >= 3 && strings.HasPrefix(token, keyword)) {
					return rule.categoryID
				}
			}
		}
	}
	return "other"
}
