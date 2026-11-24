package promptsim

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultNGram            = 4
	DefaultThreshold        = 0.80
	MinTemplateRunes        = 80
	DefaultMaxTemplateBytes = 8 * 1024
	MaxTemplateBytes        = 64 * 1024
	TemplateSeparator       = "---"
)

// IsSupportedTemplateLimit reports whether the configured per-template limit
// is one of the bounded values available in request-policy settings.
func IsSupportedTemplateLimit(limit int) bool {
	switch limit {
	case 8 * 1024, 16 * 1024, 32 * 1024, MaxTemplateBytes:
		return true
	default:
		return false
	}
}

// Template is one precomputed dangerous-prompt fragment.
type Template struct {
	Name  string
	Text  string
	Grams map[string]struct{}
}

// Match is the best containment hit against a request.
type Match struct {
	Name  string
	Score float64
	Text  string
}

// Normalize folds case and compresses whitespace for stable n-grams.
func Normalize(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	prevSpace := true
	for _, r := range strings.ToLower(text) {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// TruncateBytes keeps a valid UTF-8 prefix within maxBytes.
func TruncateBytes(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	truncated := text[:maxBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

// ParseTemplates splits on a line that contains only "---".
func ParseTemplates(raw string) ([]string, error) {
	return ParseTemplatesWithLimit(raw, DefaultMaxTemplateBytes)
}

// ParseTemplatesWithLimit splits and validates templates against the selected
// per-template byte limit, which must be one of the supported bounded values.
func ParseTemplatesWithLimit(raw string, maxBytes int) ([]string, error) {
	if !IsSupportedTemplateLimit(maxBytes) {
		return nil, ErrInvalidTemplateLimit
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	var (
		out     []string
		current strings.Builder
	)
	flush := func() error {
		part := strings.TrimSpace(current.String())
		current.Reset()
		if part == "" {
			return nil
		}
		if utf8.RuneCountInString(part) < MinTemplateRunes {
			return errShortTemplate
		}
		if len(part) > maxBytes {
			return ErrLongTemplate
		}
		out = append(out, part)
		return nil
	}
	for line := range strings.SplitSeq(raw, "\n") {
		if strings.TrimSpace(line) == TemplateSeparator {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

type shortTemplateError struct{}

func (shortTemplateError) Error() string {
	return "each prompt similarity template must contain at least 80 characters"
}

// ErrShortTemplate is returned when a template is shorter than MinTemplateRunes.
var ErrShortTemplate = errShortTemplate

// ErrLongTemplate prevents silently saving only a prefix of a template.
var ErrLongTemplate = errors.New("prompt similarity template exceeds the configured byte limit")

// ErrInvalidTemplateLimit rejects unsupported or unbounded settings values.
var ErrInvalidTemplateLimit = errors.New("unsupported prompt similarity template byte limit")

var errShortTemplate = shortTemplateError{}

// Compile builds n-gram sets for templates. Empty input yields an empty slice.
func Compile(texts []string, n int) []Template {
	if n <= 0 {
		n = DefaultNGram
	}
	out := make([]Template, 0, len(texts))
	for i, text := range texts {
		norm := Normalize(text)
		if utf8.RuneCountInString(norm) < n {
			continue
		}
		out = append(out, Template{
			Name:  templateName(i, text),
			Text:  text,
			Grams: ngrams(norm, n),
		})
	}
	return out
}

func templateName(_ int, text string) string {
	preview := TruncateBytes(strings.TrimSpace(text), 48)
	preview = strings.ReplaceAll(preview, "\n", " ")
	return preview
}

func ngrams(text string, n int) map[string]struct{} {
	runes := []rune(text)
	if len(runes) < n {
		return nil
	}
	set := make(map[string]struct{}, len(runes)-n+1)
	for i := 0; i+n <= len(runes); i++ {
		set[string(runes[i:i+n])] = struct{}{}
	}
	return set
}

// ContainmentScore is |grams(template) ∩ grams(request)| / |grams(template)|.
func ContainmentScore(templateGrams, requestGrams map[string]struct{}) float64 {
	if len(templateGrams) == 0 {
		return 0
	}
	hit := 0
	for g := range templateGrams {
		if _, ok := requestGrams[g]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(templateGrams))
}

// BestMatch returns the highest containment score at or above threshold.
func BestMatch(request string, templates []Template, threshold float64, n int) (Match, bool) {
	if len(templates) == 0 {
		return Match{}, false
	}
	if n <= 0 {
		n = DefaultNGram
	}
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	// Scan every rune, retaining only n-grams that occur in a template. Memory
	// depends on the configured templates rather than the request's length.
	candidates := make(map[string]struct{})
	for _, tmpl := range templates {
		for gram := range tmpl.Grams {
			candidates[gram] = struct{}{}
		}
	}
	reqGrams := make(map[string]struct{})
	window := make([]rune, 0, n+1)
	hasText, pendingSpace := false, false
	for _, r := range request {
		if unicode.IsSpace(r) {
			pendingSpace = hasText
			continue
		}
		if pendingSpace {
			window = append(window, ' ')
		}
		window = append(window, unicode.ToLower(r))
		hasText, pendingSpace = true, false
		for len(window) >= n {
			gram := string(window[:n])
			if _, ok := candidates[gram]; ok {
				reqGrams[gram] = struct{}{}
			}
			copy(window, window[1:])
			window = window[:len(window)-1]
		}
	}
	best := Match{}
	found := false
	for _, tmpl := range templates {
		score := ContainmentScore(tmpl.Grams, reqGrams)
		if score < threshold {
			continue
		}
		if !found || score > best.Score {
			best = Match{Name: tmpl.Name, Score: score, Text: tmpl.Text}
			found = true
		}
	}
	return best, found
}
