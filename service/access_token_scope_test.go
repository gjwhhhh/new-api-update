package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAccessTokenScopesAllowsConversationAuditDeletion(t *testing.T) {
	scopes, err := NormalizeAccessTokenScopes(1, common.RoleRootUser, []string{"log:write"})
	require.NoError(t, err)
	require.Equal(t, []string{"log:write"}, scopes)
}
