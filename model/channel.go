package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"not null"`
	OpenAIOrganization *string `json:"openai_organization"`
	TestModel          *string `json:"test_model"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	Weight             *uint   `json:"weight" gorm:"default:0"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	LastAutoTestTime   int64   `json:"last_auto_test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(4096);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:text"`
	//MaxInputTokens     *int    `json:"max_input_tokens" gorm:"default:0"`
	StatusCodeMapping *string `json:"status_code_mapping" gorm:"type:varchar(1024);default:''"`
	Priority          *int64  `json:"priority" gorm:"bigint;default:0"`
	AutoBan           *int    `json:"auto_ban" gorm:"default:1"`
	OtherInfo         string  `json:"other_info"`
	Tag               *string `json:"tag" gorm:"index"`
	Setting           *string `json:"setting" gorm:"type:text"` // 渠道额外设置
	ParamOverride     *string `json:"param_override" gorm:"type:text"`
	HeaderOverride    *string `json:"header_override" gorm:"type:text"`
	Remark            *string `json:"remark" gorm:"type:varchar(255)" validate:"max=255"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info" gorm:"type:json"`

	OtherSettings string `json:"settings" gorm:"column:settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-" gorm:"-"`
}

type ChannelInfo struct {
	IsMultiKey                   bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize                 int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList           map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason       map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime         map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex         int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyRecoveryPollingIndex int                   `json:"multi_key_recovery_polling_index"`    // 健康检查轮询的自动禁用 Key 索引
	ManuallyDisabled             bool                  `json:"manually_disabled,omitempty"`         // 管理员关闭整个渠道，不等同于单个 Key 手动禁用
	MultiKeyMode                 constant.MultiKeyMode `json:"multi_key_mode"`
}

// ChannelKeySelection describes the exact key selected for a relay or health
// check. The index and original status keep later state changes independent of
// duplicate key values and concurrent edits.
type ChannelKeySelection struct {
	Key            string
	Index          int
	OriginalStatus int
}

// ChannelHealthCheckResult describes the state transition caused by one
// completed multi-key health check.
type ChannelHealthCheckResult struct {
	Changed       bool
	StatusChanged bool
	Enabled       bool
	Disabled      bool
	Stale         bool
}

type ChannelSortOptions struct {
	SortBy    string
	SortOrder string
	IDSort    bool
}

var channelSortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"priority":      "priority",
	"balance":       "balance",
	"response_time": "response_time",
	"test_time":     "test_time",
}

func NewChannelSortOptions(sortBy string, sortOrder string, idSort bool) ChannelSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := channelSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = ""
		normalizedSortOrder = ""
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return ChannelSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
		IDSort:    idSort,
	}
}

func (options ChannelSortOptions) Apply(query *gorm.DB) *gorm.DB {
	if columnName, ok := channelSortColumns[options.SortBy]; ok {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: columnName},
			Desc:   options.SortOrder != "asc",
		})
	}
	if options.IDSort {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: "priority"},
		Desc:   true,
	})
}

func resolveChannelSortOptions(idSort bool, sortOptions []ChannelSortOptions) ChannelSortOptions {
	if len(sortOptions) == 0 {
		return NewChannelSortOptions("", "", idSort)
	}
	options := sortOptions[0]
	options.IDSort = options.IDSort || idSort
	return options
}

func NormalizeChannelGroupFilter(group string) string {
	group = strings.TrimSpace(group)
	if group == "" || strings.EqualFold(group, "all") || strings.EqualFold(group, "null") {
		return ""
	}
	return group
}

func channelGroupFilterCondition() string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return `CONCAT(',', ` + commonGroupCol + `, ',') LIKE ? ESCAPE '!'`
	}
	return `(',' || ` + commonGroupCol + ` || ',') LIKE ? ESCAPE '!'`
}

func channelGroupFilterPattern(group string) string {
	group = strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(group)
	return "%," + group + ",%"
}

func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	group = NormalizeChannelGroupFilter(group)
	if group == "" {
		return query
	}
	return query.Where(channelGroupFilterCondition(), channelGroupFilterPattern(group))
}

// Value implements driver.Valuer interface
func (c ChannelInfo) Value() (driver.Value, error) {
	return common.Marshal(&c)
}

// Scan implements sql.Scanner interface
func (c *ChannelInfo) Scan(value interface{}) error {
	bytesValue, _ := value.([]byte)
	return common.Unmarshal(bytesValue, c)
}

func (channel *Channel) GetKeys() []string {
	if channel.Key == "" {
		return []string{}
	}
	if len(channel.Keys) > 0 {
		return channel.Keys
	}
	trimmed := strings.TrimSpace(channel.Key)
	// If the key starts with '[', try to parse it as a JSON array (e.g., for Vertex AI scenarios)
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
			res := make([]string, len(arr))
			for i, v := range arr {
				res[i] = string(v)
			}
			return res
		}
	}
	// Otherwise, fall back to splitting by newline
	keys := strings.Split(strings.Trim(channel.Key, "\n"), "\n")
	return keys
}

// ReconcileMultiKeyState keeps disabled state with unchanged key values when a
// multi-key list is edited. New keys start enabled and removed keys are pruned.
func (channel *Channel) ReconcileMultiKeyState(previousKeys []string) {
	currentKeys := channel.GetKeys()
	previousIndexes := make(map[string][]int, len(previousKeys))
	for index, key := range previousKeys {
		previousIndexes[key] = append(previousIndexes[key], index)
	}

	statusList := make(map[int]int)
	disabledReasons := make(map[int]string)
	disabledTimes := make(map[int]int64)
	for currentIndex, key := range currentKeys {
		indexes := previousIndexes[key]
		if len(indexes) == 0 {
			continue
		}
		previousIndex := indexes[0]
		previousIndexes[key] = indexes[1:]

		status, disabled := channel.ChannelInfo.MultiKeyStatusList[previousIndex]
		if !disabled || status == common.ChannelStatusEnabled {
			continue
		}
		statusList[currentIndex] = status
		if reason, ok := channel.ChannelInfo.MultiKeyDisabledReason[previousIndex]; ok {
			disabledReasons[currentIndex] = reason
		}
		if disabledTime, ok := channel.ChannelInfo.MultiKeyDisabledTime[previousIndex]; ok {
			disabledTimes[currentIndex] = disabledTime
		}
	}

	channel.ChannelInfo.MultiKeySize = len(currentKeys)
	channel.ChannelInfo.MultiKeyStatusList = statusList
	channel.ChannelInfo.MultiKeyDisabledReason = disabledReasons
	channel.ChannelInfo.MultiKeyDisabledTime = disabledTimes
	channel.normalizeMultiKeyState(len(currentKeys))
}

func (channel *Channel) normalizeMultiKeyState(keyCount int) {
	channel.ChannelInfo.MultiKeySize = keyCount
	for idx := range channel.ChannelInfo.MultiKeyStatusList {
		if idx < 0 || idx >= keyCount {
			delete(channel.ChannelInfo.MultiKeyStatusList, idx)
		}
	}
	for idx := range channel.ChannelInfo.MultiKeyDisabledReason {
		status, disabled := channel.ChannelInfo.MultiKeyStatusList[idx]
		if idx < 0 || idx >= keyCount || !disabled || status == common.ChannelStatusEnabled {
			delete(channel.ChannelInfo.MultiKeyDisabledReason, idx)
		}
	}
	for idx := range channel.ChannelInfo.MultiKeyDisabledTime {
		status, disabled := channel.ChannelInfo.MultiKeyStatusList[idx]
		if idx < 0 || idx >= keyCount || !disabled || status == common.ChannelStatusEnabled {
			delete(channel.ChannelInfo.MultiKeyDisabledTime, idx)
		}
	}
	if channel.ChannelInfo.MultiKeyPollingIndex < 0 || channel.ChannelInfo.MultiKeyPollingIndex >= keyCount {
		channel.ChannelInfo.MultiKeyPollingIndex = 0
	}
	if channel.ChannelInfo.MultiKeyRecoveryPollingIndex < 0 || channel.ChannelInfo.MultiKeyRecoveryPollingIndex >= keyCount {
		channel.ChannelInfo.MultiKeyRecoveryPollingIndex = 0
	}
}

func (channel *Channel) GetNextEnabledKey() (string, int, *types.NewAPIError) {
	selection, err := channel.getNextKey(false, false)
	return selection.Key, selection.Index, err
}

// GetNextTestKey selects a key for a manual single-channel test. It only probes
// an auto-disabled key when there is no enabled key, so the test remains a
// diagnostic action and does not disturb normal key rotation.
func (channel *Channel) GetNextTestKey() (string, int, *types.NewAPIError) {
	selection, err := channel.getNextKey(true, false)
	return selection.Key, selection.Index, err
}

// GetNextHealthCheckKey prioritizes auto-disabled keys. It is intentionally
// separate from production and manual-test selection so a recovered key does
// not starve the remaining keys waiting for recovery.
func (channel *Channel) GetNextHealthCheckKey() (ChannelKeySelection, *types.NewAPIError) {
	return channel.getNextKey(true, true)
}

func (channel *Channel) getNextKey(allowAutoDisabled bool, prioritizeAutoDisabled bool) (ChannelKeySelection, *types.NewAPIError) {
	selection := ChannelKeySelection{Index: -1, OriginalStatus: common.ChannelStatusEnabled}
	// If not in multi-key mode, return the original key string directly.
	if !channel.ChannelInfo.IsMultiKey {
		selection.Key = channel.Key
		selection.Index = 0
		return selection, nil
	}

	// Obtain all keys (split by \n)
	keys := channel.GetKeys()
	if len(keys) == 0 {
		// No keys available, return error, should disable the channel
		return selection, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	statusList := channel.ChannelInfo.MultiKeyStatusList
	// helper to get key status, default to enabled when missing
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}

	selectIndexes := func(status int) []int {
		indexes := make([]int, 0, len(keys))
		for i := range keys {
			if getStatus(i) == status {
				indexes = append(indexes, i)
			}
		}
		return indexes
	}

	selectedStatus := common.ChannelStatusEnabled
	selectedIndexes := selectIndexes(common.ChannelStatusEnabled)
	if prioritizeAutoDisabled {
		if autoDisabledIndexes := selectIndexes(common.ChannelStatusAutoDisabled); len(autoDisabledIndexes) > 0 {
			selectedStatus = common.ChannelStatusAutoDisabled
			selectedIndexes = autoDisabledIndexes
		}
	} else if len(selectedIndexes) == 0 && allowAutoDisabled {
		selectedStatus = common.ChannelStatusAutoDisabled
		selectedIndexes = selectIndexes(common.ChannelStatusAutoDisabled)
	}
	// If no specific status list or none enabled, return an explicit error so caller can
	// properly handle a channel with no available keys (e.g. mark channel disabled).
	// Returning the first key here caused requests to keep using an already-disabled key.
	if len(selectedIndexes) == 0 {
		return selection, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	if prioritizeAutoDisabled && selectedStatus == common.ChannelStatusAutoDisabled {
		start := channel.ChannelInfo.MultiKeyRecoveryPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for i := 0; i < len(keys); i++ {
			index := (start + i) % len(keys)
			if getStatus(index) == common.ChannelStatusAutoDisabled {
				channel.ChannelInfo.MultiKeyRecoveryPollingIndex = (index + 1) % len(keys)
				return ChannelKeySelection{Key: keys[index], Index: index, OriginalStatus: selectedStatus}, nil
			}
		}
	}

	switch channel.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		// Randomly pick one eligible key.
		selectedIdx := selectedIndexes[rand.Intn(len(selectedIndexes))]
		return ChannelKeySelection{Key: keys[selectedIdx], Index: selectedIdx, OriginalStatus: selectedStatus}, nil
	case constant.MultiKeyModePolling:
		// Use channel-specific lock to ensure thread-safe polling

		channelInfo, err := CacheGetChannelInfo(channel.Id)
		if err != nil {
			return selection, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		defer func() {
			if common.DebugEnabled {
				logger.LogDebug(nil, "channel %d polling index: %d", channel.Id, channel.ChannelInfo.MultiKeyPollingIndex)
			}
			if !common.MemoryCacheEnabled {
				_ = channel.SaveChannelInfo()
			} else {
				// CacheUpdateChannel(channel)
			}
		}()
		// Start from the saved polling index and look for the next enabled key
		start := channelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for i := 0; i < len(keys); i++ {
			idx := (start + i) % len(keys)
			if getStatus(idx) == selectedStatus {
				// update polling index for next call (point to the next position)
				channel.ChannelInfo.MultiKeyPollingIndex = (idx + 1) % len(keys)
				return ChannelKeySelection{Key: keys[idx], Index: idx, OriginalStatus: selectedStatus}, nil
			}
		}
		// Fallback – should not happen, but return first enabled key
		return ChannelKeySelection{Key: keys[selectedIndexes[0]], Index: selectedIndexes[0], OriginalStatus: selectedStatus}, nil
	default:
		// Unknown mode, default to first enabled key (or original key string)
		return ChannelKeySelection{Key: keys[selectedIndexes[0]], Index: selectedIndexes[0], OriginalStatus: selectedStatus}, nil
	}
}

func (channel *Channel) SaveChannelInfo() error {
	return DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error
}

func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	return strings.Split(strings.Trim(channel.Models, ","), ",")
}

func (channel *Channel) GetGroups() []string {
	if channel.Group == "" {
		return []string{}
	}
	groups := strings.Split(strings.Trim(channel.Group, ","), ",")
	for i, group := range groups {
		groups[i] = strings.TrimSpace(group)
	}
	return groups
}

func (channel *Channel) GetOtherInfo() map[string]interface{} {
	otherInfo := make(map[string]interface{})
	if channel.OtherInfo != "" {
		err := common.Unmarshal([]byte(channel.OtherInfo), &otherInfo)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		}
	}
	return otherInfo
}

func (channel *Channel) SetOtherInfo(otherInfo map[string]interface{}) {
	otherInfoBytes, err := json.Marshal(otherInfo)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		return
	}
	channel.OtherInfo = string(otherInfoBytes)
}

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

func (channel *Channel) Save() error {
	return DB.Save(channel).Error
}

func (channel *Channel) SaveWithoutKey() error {
	if channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	return DB.Omit("key").Save(channel).Error
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	var err error
	order := resolveChannelSortOptions(idSort, sortOptions)
	if selectAll {
		err = order.Apply(DB).Find(&channels).Error
	} else {
		err = order.Apply(DB).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)
	query := order.Apply(DB.Where("tag = ?", tag))
	if !selectAll {
		query = query.Omit("key")
	}
	err := query.Find(&channels).Error
	return channels, err
}

func SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := resolveChannelSortOptions(idSort, sortOptions)

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	// 执行查询
	err := order.Apply(baseQuery).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}

func GetChannelById(id int, selectAll bool) (*Channel, error) {
	channel := &Channel{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(channel, "id = ?", id).Error
	} else {
		err = DB.Omit("key").First(channel, "id = ?", id).Error
	}
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func BatchInsertChannels(channels []Channel) error {
	if len(channels) == 0 {
		return nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, chunk := range lo.Chunk(channels, 50) {
		if err := tx.Create(&chunk).Error; err != nil {
			tx.Rollback()
			return err
		}
		for _, channel_ := range chunk {
			if err := channel_.AddAbilities(tx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit().Error
}

func BatchDeleteChannels(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	// 使用事务 分批删除channel表和abilities表
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	for _, chunk := range lo.Chunk(ids, 200) {
		if err := tx.Where("id in (?)", chunk).Delete(&Channel{}).Error; err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Where("channel_id in (?)", chunk).Delete(&Ability{}).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetWeight() int {
	if channel.Weight == nil {
		return 0
	}
	return int(*channel.Weight)
}

func (channel *Channel) GetBaseURL() string {
	if channel.BaseURL == nil {
		return ""
	}
	url := *channel.BaseURL
	if url == "" {
		url = constant.ChannelBaseURLs[channel.Type]
	}
	return url
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

func (channel *Channel) GetStatusCodeMapping() string {
	if channel.StatusCodeMapping == nil {
		return ""
	}
	return *channel.StatusCodeMapping
}

func (channel *Channel) Insert() error {
	var err error
	err = DB.Create(channel).Error
	if err != nil {
		return err
	}
	err = channel.AddAbilities(nil)
	return err
}

func (channel *Channel) Update() error {
	// If this is a multi-key channel, recalculate MultiKeySize based on the
	// current key list and remove state for indexes that no longer exist.
	if channel.ChannelInfo.IsMultiKey {
		currentKeySource := channel
		if channel.Key == "" {
			if existing, getErr := GetChannelById(channel.Id, true); getErr == nil {
				currentKeySource = &Channel{Key: existing.Key}
			}
		}
		channel.normalizeMultiKeyState(len(currentKeySource.GetKeys()))
	}
	var err error
	err = DB.Model(channel).Updates(channel).Error
	if err != nil {
		return err
	}
	DB.Model(channel).First(channel, "id = ?", channel.Id)
	err = channel.UpdateAbilities(nil)
	return err
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	err := DB.Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update response time: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) UpdateAutomaticTestTime(responseTime int64) {
	now := common.GetTimestamp()
	err := DB.Model(channel).Select("response_time", "test_time", "last_auto_test_time").Updates(Channel{
		TestTime:         now,
		LastAutoTestTime: now,
		ResponseTime:     int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update automatic test time: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.TestTime = now
	channel.LastAutoTestTime = now
	channel.ResponseTime = int(responseTime)
}

func ResetChannelAutomaticTestTime(channelID int) error {
	return DB.Model(&Channel{}).Where("id = ?", channelID).Update("last_auto_test_time", 0).Error
}

func (channel *Channel) UpdateBalance(balance float64) {
	err := DB.Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: common.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update balance: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) Delete() error {
	var err error
	err = DB.Delete(channel).Error
	if err != nil {
		return err
	}
	err = channel.DeleteAbilities()
	return err
}

var channelStatusLock sync.Mutex

// channelPollingLocks stores locks for each channel.id to ensure thread-safe polling
var channelPollingLocks sync.Map

// GetChannelPollingLock returns or creates a mutex for the given channel ID
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if lock, exists := channelPollingLocks.Load(channelId); exists {
		return lock.(*sync.Mutex)
	}
	// Create new lock for this channel
	newLock := &sync.Mutex{}
	actual, _ := channelPollingLocks.LoadOrStore(channelId, newLock)
	return actual.(*sync.Mutex)
}

// CleanupChannelPollingLocks removes locks for channels that no longer exist
// This is optional and can be called periodically to prevent memory leaks
func CleanupChannelPollingLocks() {
	var activeChannelIds []int
	DB.Model(&Channel{}).Pluck("id", &activeChannelIds)

	activeChannelSet := make(map[int]bool)
	for _, id := range activeChannelIds {
		activeChannelSet[id] = true
	}

	channelPollingLocks.Range(func(key, value interface{}) bool {
		channelId := key.(int)
		if !activeChannelSet[channelId] {
			channelPollingLocks.Delete(channelId)
		}
		return true
	})
}

func (channel *Channel) multiKeyStatus(index int) int {
	if status, ok := channel.ChannelInfo.MultiKeyStatusList[index]; ok {
		return status
	}
	return common.ChannelStatusEnabled
}

// HasAutoDisabledKey reports whether a multi-key channel still has a key that
// should be probed by passive recovery.
func (channel *Channel) HasAutoDisabledKey() bool {
	if channel == nil || !channel.ChannelInfo.IsMultiKey {
		return false
	}
	for index := range channel.GetKeys() {
		if channel.multiKeyStatus(index) == common.ChannelStatusAutoDisabled {
			return true
		}
	}
	return false
}

func (channel *Channel) setChannelStatusReason(reason string) {
	info := channel.GetOtherInfo()
	if reason == "" {
		delete(info, "status_reason")
		delete(info, "status_time")
	} else {
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
	}
	channel.SetOtherInfo(info)
}

// ReconcileMultiKeyChannelStatus derives the channel availability from the
// channel-level manual switch and all current key states.
func (channel *Channel) ReconcileMultiKeyChannelStatus() {
	keys := channel.GetKeys()
	channel.normalizeMultiKeyState(len(keys))
	if channel.ChannelInfo.ManuallyDisabled {
		channel.Status = common.ChannelStatusManuallyDisabled
		return
	}

	hasAutoDisabled := false
	for index := range keys {
		switch channel.multiKeyStatus(index) {
		case common.ChannelStatusEnabled:
			channel.Status = common.ChannelStatusEnabled
			channel.setChannelStatusReason("")
			return
		case common.ChannelStatusAutoDisabled:
			hasAutoDisabled = true
		}
	}
	if hasAutoDisabled {
		channel.Status = common.ChannelStatusAutoDisabled
		channel.setChannelStatusReason("All keys are disabled")
		return
	}
	channel.Status = common.ChannelStatusManuallyDisabled
	channel.setChannelStatusReason("All keys are manually disabled")
}

func handlerMultiKeyUpdate(channel *Channel, usingKey string, status int, reason string) {
	keys := channel.GetKeys()
	if len(keys) == 0 {
		channel.Status = status
		return
	}

	if usingKey == "" {
		switch status {
		case common.ChannelStatusManuallyDisabled:
			channel.ChannelInfo.ManuallyDisabled = true
			channel.Status = common.ChannelStatusManuallyDisabled
			channel.setChannelStatusReason(reason)
		case common.ChannelStatusEnabled:
			channel.ChannelInfo.ManuallyDisabled = false
			channel.ReconcileMultiKeyChannelStatus()
		default:
			channel.Status = status
			channel.setChannelStatusReason(reason)
		}
		return
	}

	for index, key := range keys {
		if key == usingKey {
			handlerMultiKeyUpdateAtIndex(channel, index, status, reason)
			return
		}
	}
	common.SysLog(fmt.Sprintf("failed to update multi-key status: channel_id=%d, using key not found", channel.Id))
}

func handlerMultiKeyUpdateAtIndex(channel *Channel, keyIndex int, status int, reason string) {
	keys := channel.GetKeys()
	if keyIndex < 0 || keyIndex >= len(keys) {
		common.SysLog(fmt.Sprintf("failed to update multi-key status: channel_id=%d, key index=%d out of range", channel.Id, keyIndex))
		return
	}
	if channel.ChannelInfo.MultiKeyStatusList == nil {
		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
	}
	if status == common.ChannelStatusEnabled {
		delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		delete(channel.ChannelInfo.MultiKeyDisabledReason, keyIndex)
		delete(channel.ChannelInfo.MultiKeyDisabledTime, keyIndex)
	} else {
		channel.ChannelInfo.MultiKeyStatusList[keyIndex] = status
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
		channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = common.GetTimestamp()
	}
	channel.ReconcileMultiKeyChannelStatus()
}

func UpdateChannelStatus(channelId int, usingKey string, status int, reason string) bool {
	return updateChannelStatus(channelId, usingKey, -1, status, reason)
}

// UpdateChannelStatusAtKeyIndex updates an exact multi-key slot. Relay errors
// carry the index selected for the request, so duplicate key values cannot
// cause the wrong key to be disabled.
func UpdateChannelStatusAtKeyIndex(channelId, keyIndex, status int, reason string) bool {
	return updateChannelStatus(channelId, "", keyIndex, status, reason)
}

func updateChannelStatus(channelId int, usingKey string, keyIndex, status int, reason string) bool {
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()

		channelCache, _ := CacheGetChannel(channelId)
		if channelCache == nil {
			return false
		}
		if channelCache.ChannelInfo.IsMultiKey {
			// Use per-channel lock to prevent concurrent map read/write with GetNextEnabledKey
			beforeStatus := channelCache.Status
			pollingLock := GetChannelPollingLock(channelId)
			pollingLock.Lock()
			if keyIndex >= 0 {
				handlerMultiKeyUpdateAtIndex(channelCache, keyIndex, status, reason)
			} else {
				handlerMultiKeyUpdate(channelCache, usingKey, status, reason)
			}
			pollingLock.Unlock()
			if beforeStatus != channelCache.Status {
				CacheUpdateChannelStatus(channelId, channelCache.Status)
			}
			//CacheUpdateChannel(channelCache)
			//return true
		} else {
			// 如果缓存渠道存在，且状态已是目标状态，直接返回
			if channelCache.Status == status {
				return false
			}
			CacheUpdateChannelStatus(channelId, status)
		}
	}

	shouldUpdateAbilities := false
	defer func() {
		if shouldUpdateAbilities {
			err := UpdateAbilityStatus(channelId, status == common.ChannelStatusEnabled)
			if err != nil {
				common.SysLog(fmt.Sprintf("failed to update ability status: channel_id=%d, error=%v", channelId, err))
			}
		}
	}()
	channel, err := GetChannelById(channelId, true)
	if err != nil {
		return false
	} else {
		if channel.Status == status && !channel.ChannelInfo.IsMultiKey {
			return false
		}

		if channel.ChannelInfo.IsMultiKey {
			beforeStatus := channel.Status
			// Protect map writes with the same per-channel lock used by readers
			pollingLock := GetChannelPollingLock(channelId)
			pollingLock.Lock()
			if keyIndex >= 0 {
				handlerMultiKeyUpdateAtIndex(channel, keyIndex, status, reason)
			} else {
				handlerMultiKeyUpdate(channel, usingKey, status, reason)
			}
			pollingLock.Unlock()
			if beforeStatus != channel.Status {
				shouldUpdateAbilities = true
			}
		} else {
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			channel.Status = status
			shouldUpdateAbilities = true
		}
		err = channel.SaveWithoutKey()
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, err))
			return false
		}
	}
	return true
}

// ApplyMultiKeyHealthCheckResult applies one completed health check
// synchronously. The selected key must still match the stored key and state;
// otherwise an administrator changed it while the upstream request was in
// flight and the obsolete result is ignored.
func ApplyMultiKeyHealthCheckResult(channelId int, selection ChannelKeySelection, success, shouldDisable, shouldEnable bool, reason string) (ChannelHealthCheckResult, error) {
	result := ChannelHealthCheckResult{}
	if selection.Index < 0 {
		return result, nil
	}

	channelStatusLock.Lock()
	tx := DB.Begin()
	if tx.Error != nil {
		channelStatusLock.Unlock()
		return result, tx.Error
	}
	channel := &Channel{}
	err := lockForUpdate(tx).Where("id = ?", channelId).First(channel).Error
	if err != nil {
		tx.Rollback()
		channelStatusLock.Unlock()
		return result, err
	}
	if !channel.ChannelInfo.IsMultiKey || channel.ChannelInfo.ManuallyDisabled {
		tx.Rollback()
		channelStatusLock.Unlock()
		result.Stale = true
		return result, nil
	}

	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	keys := channel.GetKeys()
	if selection.Index >= len(keys) || keys[selection.Index] != selection.Key || channel.multiKeyStatus(selection.Index) != selection.OriginalStatus {
		pollingLock.Unlock()
		tx.Rollback()
		channelStatusLock.Unlock()
		result.Stale = true
		return result, nil
	}

	previousStatus := channel.Status
	if selection.OriginalStatus == common.ChannelStatusAutoDisabled {
		channel.ChannelInfo.MultiKeyRecoveryPollingIndex = (selection.Index + 1) % len(keys)
		result.Changed = true
	}

	switch {
	case success && selection.OriginalStatus == common.ChannelStatusAutoDisabled && shouldEnable:
		handlerMultiKeyUpdateAtIndex(channel, selection.Index, common.ChannelStatusEnabled, "")
		result.Changed = true
		result.Enabled = true
	case success && selection.OriginalStatus == common.ChannelStatusEnabled && channel.Status == common.ChannelStatusAutoDisabled:
		// A legacy or channel-level auto-disable has no per-key status entry.
		// A successful check of an enabled key is the existing recovery signal.
		channel.ReconcileMultiKeyChannelStatus()
		result.Changed = true
		result.Enabled = channel.Status == common.ChannelStatusEnabled
	case !success && selection.OriginalStatus == common.ChannelStatusEnabled && shouldDisable:
		handlerMultiKeyUpdateAtIndex(channel, selection.Index, common.ChannelStatusAutoDisabled, reason)
		result.Changed = true
		result.Disabled = true
	case !success && selection.OriginalStatus == common.ChannelStatusAutoDisabled && reason != "":
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		channel.ChannelInfo.MultiKeyDisabledReason[selection.Index] = reason
		channel.ChannelInfo.MultiKeyDisabledTime[selection.Index] = common.GetTimestamp()
		result.Changed = true
	}

	if !result.Changed {
		pollingLock.Unlock()
		tx.Rollback()
		channelStatusLock.Unlock()
		return result, nil
	}
	channel.ReconcileMultiKeyChannelStatus()
	result.StatusChanged = previousStatus != channel.Status
	err = tx.Omit("key").Save(channel).Error
	if err == nil && result.StatusChanged {
		err = tx.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", channel.Status == common.ChannelStatusEnabled).Error
	}
	if err == nil {
		err = tx.Commit().Error
	} else {
		tx.Rollback()
	}
	pollingLock.Unlock()
	channelStatusLock.Unlock()
	if err != nil {
		return ChannelHealthCheckResult{}, err
	}
	if result.StatusChanged {
		InitChannelCache()
	} else if common.MemoryCacheEnabled {
		CacheUpdateChannel(channel)
	}
	return result, nil
}

func EnableChannelByTag(tag string) error {
	err := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusEnabled).Error
	if err != nil {
		return err
	}
	err = UpdateAbilityStatusByTag(tag, true)
	return err
}

func DisableChannelByTag(tag string) error {
	err := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusManuallyDisabled).Error
	if err != nil {
		return err
	}
	err = UpdateAbilityStatusByTag(tag, false)
	return err
}

func EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, priority *int64, weight *uint, paramOverride *string, headerOverride *string) error {
	updateData := Channel{}
	shouldReCreateAbilities := false
	updatedTag := tag
	// 如果 newTag 不为空且不等于 tag，则更新 tag
	if newTag != nil && *newTag != tag {
		updateData.Tag = newTag
		updatedTag = *newTag
	}
	if modelMapping != nil {
		updateData.ModelMapping = modelMapping
	}
	if models != nil && *models != "" {
		shouldReCreateAbilities = true
		updateData.Models = *models
	}
	if group != nil && *group != "" {
		shouldReCreateAbilities = true
		updateData.Group = *group
	}
	if priority != nil {
		updateData.Priority = priority
	}
	if weight != nil {
		updateData.Weight = weight
	}
	if paramOverride != nil {
		updateData.ParamOverride = paramOverride
	}
	if headerOverride != nil {
		updateData.HeaderOverride = headerOverride
	}

	err := DB.Model(&Channel{}).Where("tag = ?", tag).Updates(updateData).Error
	if err != nil {
		return err
	}
	if shouldReCreateAbilities {
		channels, err := GetChannelsByTag(updatedTag, false, false)
		if err == nil {
			for _, channel := range channels {
				err = channel.UpdateAbilities(nil)
				if err != nil {
					common.SysLog(fmt.Sprintf("failed to update abilities: channel_id=%d, tag=%s, error=%v", channel.Id, channel.GetTag(), err))
				}
			}
		}
	} else {
		err := UpdateAbilityByTag(tag, newTag, priority, weight)
		if err != nil {
			return err
		}
	}
	return nil
}

func UpdateChannelUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, quota)
		return
	}
	updateChannelUsedQuota(id, quota)
}

func updateChannelUsedQuota(id int, quota int) {
	err := DB.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update channel used quota: channel_id=%d, delta_quota=%d, error=%v", id, quota, err))
	}
}

func DeleteChannelByStatus(status int64) (int64, error) {
	result := DB.Where("status = ?", status).Delete(&Channel{})
	return result.RowsAffected, result.Error
}

func DeleteDisabledChannel() (int64, error) {
	result := DB.Where("status = ? or status = ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled).Delete(&Channel{})
	return result.RowsAffected, result.Error
}

func GetPaginatedTags(offset int, limit int) ([]*string, error) {
	return GetPaginatedChannelTags(DB.Model(&Channel{}), offset, limit)
}

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	var tags []*string
	err := query.
		Select("DISTINCT tag").
		Where("tag is not null AND tag != ''").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tag"}}).
		Offset(offset).
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	var tags []*string
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := "priority desc"
	if idSort {
		order = "id desc"
	}

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	subQuery := baseQuery.
		Select("tag").
		Where("tag != ''").
		Order(order)

	err := DB.Table("(?) as sub", subQuery).
		Select("DISTINCT tag").
		Find(&tags).Error

	if err != nil {
		return nil, err
	}

	return tags, nil
}

func (channel *Channel) ValidateSettings() error {
	channelParams := &dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), channelParams)
		if err != nil {
			return err
		}
	}
	channelOtherSettings := &dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, channelOtherSettings)
		if err != nil {
			return err
		}
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if channelOtherSettings.AdvancedCustom == nil {
			return fmt.Errorf("advanced_custom is required")
		}
	}
	if channelOtherSettings.AdvancedCustom != nil {
		if err := channelOtherSettings.AdvancedCustom.Validate(); err != nil {
			return err
		}
	}
	if err := channelOtherSettings.HealthCheck.Validate(); err != nil {
		return err
	}
	return nil
}

func (channel *Channel) GetSetting() dto.ChannelSettings {
	setting := dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
			channel.Setting = nil // 清空设置以避免后续错误
			_ = channel.Save()    // 保存修改
		}
	}
	return setting
}

func (channel *Channel) SetSetting(setting dto.ChannelSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.Setting = common.GetPointer[string](string(settingBytes))
}

func (channel *Channel) GetOtherSettings() dto.ChannelOtherSettings {
	setting := dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
			channel.OtherSettings = "{}" // 清空设置以避免后续错误
			_ = channel.Save()           // 保存修改
		}
	}
	return setting
}

func (channel *Channel) SetOtherSettings(setting dto.ChannelOtherSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.OtherSettings = string(settingBytes)
}

func (channel *Channel) GetParamOverride() map[string]interface{} {
	paramOverride := make(map[string]interface{})
	if channel.ParamOverride != nil && *channel.ParamOverride != "" {
		err := common.Unmarshal([]byte(*channel.ParamOverride), &paramOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal param override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return paramOverride
}

func (channel *Channel) GetHeaderOverride() map[string]interface{} {
	headerOverride := make(map[string]interface{})
	if channel.HeaderOverride != nil && *channel.HeaderOverride != "" {
		err := common.Unmarshal([]byte(*channel.HeaderOverride), &headerOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal header override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return headerOverride
}

func GetChannelsByIds(ids []int) ([]*Channel, error) {
	var channels []*Channel
	err := DB.Where("id in (?)", ids).Find(&channels).Error
	return channels, err
}

func BatchSetChannelTag(ids []int, tag *string) error {
	// 开启事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// 更新标签
	err := tx.Model(&Channel{}).Where("id in (?)", ids).Update("tag", tag).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	// update ability status
	channels, err := GetChannelsByIds(ids)
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, channel := range channels {
		err = channel.UpdateAbilities(tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 提交事务
	return tx.Commit().Error
}

// CountAllChannels returns total channels in DB
func CountAllChannels() (int64, error) {
	var total int64
	err := DB.Model(&Channel{}).Count(&total).Error
	return total, err
}

// CountAllTags returns number of non-empty distinct tags
func CountAllTags() (int64, error) {
	return CountChannelTags(DB.Model(&Channel{}))
}

func CountChannelTags(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}

// Get channels of specified type with pagination
func GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	var channels []*Channel
	order := "priority desc"
	if idSort {
		order = "id desc"
	}
	err := DB.Where("type = ?", channelType).Order(order).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	return channels, err
}

// Count channels of specific type
func CountChannelsByType(channelType int) (int64, error) {
	var count int64
	err := DB.Model(&Channel{}).Where("type = ?", channelType).Count(&count).Error
	return count, err
}

// Return map[type]count for all channels
func CountChannelsGroupByType() (map[int64]int64, error) {
	type result struct {
		Type  int64 `gorm:"column:type"`
		Count int64 `gorm:"column:count"`
	}
	var results []result
	err := DB.Model(&Channel{}).Select("type, count(*) as count").Group("type").Find(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64)
	for _, r := range results {
		counts[r.Type] = r.Count
	}
	return counts, nil
}
