package constant

var StreamingTimeout int
var DifyDebug bool
var MaxFileDownloadMB int
var StreamScannerMaxBufferMB int
var ForceStreamOption bool
var CountToken bool
var GetMediaToken bool
var GetMediaTokenNotStream bool
var UpdateTask bool
var MaxRequestBodyMB int
var AnonymousRequestBodyLimitKB int

// ResponsesStreamPreCommit* control the private buffer used before an OpenAI
// Responses stream has emitted client-visible output. They are deliberately
// separate from incoming request-body limits.
var ResponsesStreamPreCommitMemoryKB int
var ResponsesStreamPreCommitMaxKB int
var ResponsesStreamPreCommitDiskBudgetMB int
var ResponsesStreamPreCommitMaxEvents int
var ResponsesStreamPreCommitFileTTLMinutes int
var AzureDefaultAPIVersion string
var NotifyLimitCount int
var NotificationLimitDurationMinute int
var GenerateDefaultToken bool
var ErrorLogEnabled bool
var TaskQueryLimit int
var TaskTimeoutMinutes int

// temporary variable for sora patch, will be removed in future
var TaskPricePatches []string

// TrustedRedirectDomains is a list of trusted domains for redirect URL validation.
// Domains support subdomain matching (e.g., "example.com" matches "sub.example.com").
var TrustedRedirectDomains []string
