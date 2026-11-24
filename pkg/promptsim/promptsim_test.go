package promptsim

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func longTemplate(prefix string) string {
	return prefix + strings.Repeat("稳定特征段用于相似度匹配测试内容", 8)
}

func TestParseTemplatesRejectsShort(t *testing.T) {
	_, err := ParseTemplates("too short")
	require.ErrorIs(t, err, ErrShortTemplate)
}

func TestParseTemplatesSplitsAndBounds(t *testing.T) {
	first := longTemplate("【运营方配置 · 平台执行策略】")
	second := longTemplate("【任务 #117 批量创建")
	raw := first + "\n---\n" + second
	parts, err := ParseTemplates(raw)
	require.NoError(t, err)
	require.Len(t, parts, 2)
	assert.Contains(t, parts[0], "运营方配置")
	assert.Contains(t, parts[1], "任务 #117")

	huge := strings.Repeat("汉", MaxTemplateBytes+100)
	_, err = ParseTemplates(huge)
	require.ErrorIs(t, err, ErrLongTemplate)
	boundary := strings.Repeat("x", MaxTemplateBytes)
	parts, err = ParseTemplates(boundary)
	require.NoError(t, err)
	assert.Equal(t, []string{boundary}, parts)
}

func TestBestMatchScansFullRequestAndPreservesNormalization(t *testing.T) {
	template := "Operator instructions: review the assigned records, preserve the original timestamps, compare each result with the reference dataset, and report any missing entries."
	for _, n := range []int{1, DefaultNGram, 8} {
		templates := Compile([]string{template}, n)
		for _, request := range []string{
			template,
			strings.Repeat("ordinary context ", 20000) + template,
			strings.Repeat("普通知识资料", 20000) + strings.ToUpper(strings.ReplaceAll(template, " ", "\t\n\u00a0")) + "\n  ",
			"A normal question about gardening and caring for household plants.",
			strings.Replace(template, "original timestamps", "original recorded timestamps", 1),
		} {
			wantScore := ContainmentScore(templates[0].Grams, ngrams(Normalize(request), n))
			match, ok := BestMatch(request, templates, 0.8, n)
			assert.Equal(t, wantScore >= 0.8, ok)
			if ok {
				assert.Equal(t, wantScore, match.Score)
			}
		}
	}
}

func TestBestMatchHitsShellAndIgnoresShortWords(t *testing.T) {
	shell := longTemplate("本轮是定时巡检（心跳到点）的唤醒【本轮态势（graph_overview")
	tmpls := Compile([]string{shell}, DefaultNGram)
	require.NotEmpty(t, tmpls)

	request := shell + "\n" + strings.Repeat("cold_digests变化正文", 200)
	match, ok := BestMatch(request, tmpls, DefaultThreshold, DefaultNGram)
	require.True(t, ok)
	assert.GreaterOrEqual(t, match.Score, DefaultThreshold)

	_, ok = BestMatch("请协助取证并建隧道排查", tmpls, DefaultThreshold, DefaultNGram)
	assert.False(t, ok)
}

func TestBestMatchSlightRewriteStillHits(t *testing.T) {
	original := longTemplate("【运营方配置 · 平台执行策略】Banned vocabulary 前的稳定说明")
	tmpls := Compile([]string{original}, DefaultNGram)
	require.NotEmpty(t, tmpls)
	rewritten := strings.Replace(original, "平台执行策略", "平台执行策略（补充）", 1)
	match, ok := BestMatch(rewritten, tmpls, 0.80, DefaultNGram)
	require.True(t, ok)
	assert.GreaterOrEqual(t, match.Score, 0.80)
}
