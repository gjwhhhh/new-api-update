package types

type ChannelError struct {
	ChannelId     int    `json:"channel_id"`
	ChannelType   int    `json:"channel_type"`
	ChannelName   string `json:"channel_name"`
	IsMultiKey    bool   `json:"is_multi_key"`
	AutoBan       bool   `json:"auto_ban"`
	UsingKey      string `json:"using_key"`
	MultiKeyIndex int    `json:"multi_key_index"`
}

func NewChannelError(channelId int, channelType int, channelName string, isMultiKey bool, usingKey string, autoBan bool, multiKeyIndex ...int) *ChannelError {
	keyIndex := -1
	if len(multiKeyIndex) > 0 {
		keyIndex = multiKeyIndex[0]
	}
	return &ChannelError{
		ChannelId:     channelId,
		ChannelType:   channelType,
		ChannelName:   channelName,
		IsMultiKey:    isMultiKey,
		AutoBan:       autoBan,
		UsingKey:      usingKey,
		MultiKeyIndex: keyIndex,
	}
}
