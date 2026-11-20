package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptTextFilterExemptions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tokenID int
		channel int
	}{
		{name: "matching token", tokenID: 42, channel: 4},
		{name: "retry from exempt to non-exempt channel", tokenID: 7, channel: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			common.SetContextKey(c, constant.ContextKeyTokenId, tc.tokenID)
			filter := &PromptTextFilter{
				policy: &model.PromptTextFilterPolicy{
					Enabled: true, Keywords: []string{"blocked"},
					Target: model.PromptTextFilterTarget{Scope: "all"},
					Exempt: model.PromptTextFilterExempt{ChannelIDs: []int{3}, TokenIDs: []int{42}},
				},
				request: &dto.OpenAIResponsesRequest{Input: []byte(`"blocked word here"`)},
			}
			assert.Nil(t, filter.Check(c, tc.channel))
			if tc.tokenID == 42 {
				// A different token gets a new request filter, with the same policy.
				filter = &PromptTextFilter{policy: filter.policy, request: filter.request}
				common.SetContextKey(c, constant.ContextKeyTokenId, 7)
			}
			require.NotNil(t, filter.Check(c, 4))
		})
	}
}
