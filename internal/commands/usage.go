// Package commands 实现各命令组的执行逻辑.
// 命令函数把结果写入传入的 io.Writer 并返回 error 而不直接退出,
// 由 main 统一映射退出码, 便于测试与复用.
package commands

import "fmt"

// UsageError 表示命令行用法错误, 退出码固定为 2, 与 flag 包约定一致.
type UsageError struct{ msg string }

// NewUsageError 构造用法错误, msg 面向用户, 应包含正确用法示例.
func NewUsageError(format string, args ...any) *UsageError {
	return &UsageError{msg: fmt.Sprintf(format, args...)}
}

func (e *UsageError) Error() string { return e.msg }
