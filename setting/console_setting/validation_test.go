package console_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAnnouncementsPopupOrder(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		valid    bool
	}{
		{
			name:     "accepts legacy popup without an explicit order",
			settings: `[{"content":"Legacy","publishDate":"2026-09-06T00:00:00Z","popup":true}]`,
			valid:    true,
		},
		{
			name:     "accepts distinct popup orders",
			settings: `[{"content":"First","publishDate":"2026-09-06T00:00:00Z","popup":true,"popupOrder":1},{"content":"Second","publishDate":"2026-09-06T00:00:00Z","popup":true,"popupOrder":2}]`,
			valid:    true,
		},
		{
			name:     "rejects a non-positive popup order",
			settings: `[{"content":"Invalid","publishDate":"2026-09-06T00:00:00Z","popup":true,"popupOrder":0}]`,
		},
		{
			name:     "rejects a fractional popup order",
			settings: `[{"content":"Invalid","publishDate":"2026-09-06T00:00:00Z","popup":true,"popupOrder":1.5}]`,
		},
		{
			name:     "rejects duplicate popup orders",
			settings: `[{"content":"First","publishDate":"2026-09-06T00:00:00Z","popup":true,"popupOrder":1},{"content":"Second","publishDate":"2026-09-06T00:00:00Z","popup":true,"popupOrder":1}]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAnnouncements(test.settings)
			if test.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}
