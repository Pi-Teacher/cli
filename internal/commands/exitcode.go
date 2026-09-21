package commands

import (
	"errors"

	"github.com/Pi-Teacher/cli/internal/httpclient"
)

// 退出码约定: 0 成功, 2 用法错误, 1 网络错误与未知错误;
// 其余按服务端 error.code 细分, 与 usage 文档中的说明保持一致.
const (
	exitOK                 = 0
	exitGeneric            = 1
	exitUsage              = 2
	exitUnauthorized       = 3
	exitVersionConflict    = 4
	exitForbidden          = 5
	exitNotFound           = 6
	exitRateLimited        = 7
	exitEmbeddingUnavailab = 8
	exitSimilarityDisabled = 9
	exitMergeTopicReqd     = 10
	exitMergeEmbeddingReq  = 11
)

// apiErrorCodeToExit 是 error.code 到退出码的完整映射表.
// 服务端新增错误码时在此补一行, 未覆盖的 code 统一走 exitGeneric.
var apiErrorCodeToExit = map[string]int{
	"unauthorized":             exitUnauthorized,
	"version_conflict":         exitVersionConflict,
	"merge_topic_required":     exitMergeTopicReqd,
	"merge_embedding_required": exitMergeEmbeddingReq,
	"similarity_disabled":      exitSimilarityDisabled,
	"forbidden":                exitForbidden,
	"not_found":                exitNotFound,
	"rate_limited":             exitRateLimited,
	"embedding_unavailable":    exitEmbeddingUnavailab,
}

// ExitCode 把错误映射为进程退出码, nil 返回 0.
func ExitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		return exitUsage
	}
	var apiErr *httpclient.APIError
	if errors.As(err, &apiErr) {
		if code, ok := apiErrorCodeToExit[apiErr.Code]; ok {
			return code
		}
	}
	return exitGeneric
}
