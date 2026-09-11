package common

func IsServerErrorStatus(statusCode int) bool {
	return statusCode >= 500 || statusCode <= 599
}
