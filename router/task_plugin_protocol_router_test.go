package router

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostProtocolRegistryDrivesProtocolRoutesOnce(t *testing.T) {
	engine := gin.New()
	SetTaskPluginProtocolRouter(engine)

	expected := []string{
		"POST /v1/responses",
		"GET /v1/responses/:response_id",
		"POST /v1/videos",
		"GET /v1/videos/:task_id",
		"GET /v1/videos/:task_id/content",
		"HEAD /v1/videos/:task_id/content",
		"POST /v1/images/generations",
		"POST /v1/images/edits",
	}
	actual := make([]string, 0, len(engine.Routes()))
	for _, route := range engine.Routes() {
		actual = append(actual, fmt.Sprintf("%s %s", route.Method, route.Path))
	}
	sort.Strings(expected)
	sort.Strings(actual)
	assert.Equal(t, expected, actual)
}

func TestOpenAIResponsesTaskRouteIncludesCyberSessionGuard(t *testing.T) {
	handlers, err := taskPluginProtocolHandlers("openai_responses", "create")
	require.NoError(t, err)

	for _, handler := range handlers {
		name := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
		if strings.HasSuffix(name, "/service.CyberSessionGuard") {
			return
		}
	}
	t.Fatal("openai_responses.create route must include service.CyberSessionGuard")
}
