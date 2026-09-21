package commands

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"text/tabwriter"
	"time"

	"github.com/Pi-Teacher/cli/internal/httpclient"
)

// RunReview 实现 review 子命令组: due / submit.
// 复习提交直接生效, 不挂审批.
func RunReview(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: due / submit <card_id>")
	}
	switch args[0] {
	case "due":
		return reviewDue(args[1:], out)
	case "submit":
		return reviewSubmit(args[1:], out)
	default:
		return NewUsageError("未知子命令 %q, 可用: due / submit <card_id>", args[0])
	}
}

// dueItem 对应服务端 dueItemResponse: 到期卡与调度摘要.
type dueItem struct {
	CardID          int64     `json:"card_id"`
	Front           string    `json:"front"`
	Back            string    `json:"back"`
	TopicID         *int64    `json:"topic_id"`
	CardVersion     int64     `json:"card_version"`
	Due             time.Time `json:"due"`
	State           string    `json:"state"`
	ScheduleVersion int64     `json:"schedule_version"`
	Reps            int64     `json:"reps"`
	Lapses          int64     `json:"lapses"`
}

// dueListResult 对应服务端 dueListResponse (无分页, 只有 total).
type dueListResult struct {
	Items []dueItem `json:"items"`
	Total int64     `json:"total"`
}

// --- due ---

func reviewDue(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("review due", flag.ContinueOnError)
	topicID := fs.Int64("topic-id", 0, "按 Topic 筛选, 0 表示无 Topic 的卡, 省略不过滤")
	limit := fs.Int("limit", 0, "返回条数上限, 省略用服务端默认")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("review due: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("review due 不接受额外参数: %s", fs.Arg(0))
	}

	// topic-id 三态: 未设置不过滤, 设置 0 查无 Topic 卡.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	// limit 显式设置时必须正整数, 未设置走服务端默认.
	if setFlags["limit"] && *limit < 1 {
		return NewUsageError("--limit 必须是正整数")
	}
	if setFlags["topic-id"] && *topicID < 0 {
		return NewUsageError("--topic-id 不能为负数 (0 表示无 Topic)")
	}

	query := url.Values{}
	if setFlags["topic-id"] {
		query.Set("topic_id", fmt.Sprint(*topicID))
	}
	if setFlags["limit"] {
		query.Set("limit", fmt.Sprint(*limit))
	}
	path := "/api/cli/review/due"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var result dueListResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   path,
	}, &result)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, result)
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "CARD_ID\tFRONT\tSTATE\tDUE\tREPS\tCV\tSV")
	for _, it := range result.Items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%d\t%d\n",
			it.CardID, truncateRunes(it.Front, 30), it.State,
			it.Due.Local().Format("2006-01-02 15:04"), it.Reps,
			it.CardVersion, it.ScheduleVersion)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "共 %d 张到期 (CV=card_version, SV=schedule_version)\n", result.Total)
	return nil
}

// --- submit ---

// submitReviewResult 对应服务端 submitReviewResponse.
type submitReviewResult struct {
	CardID      int64           `json:"card_id"`
	Rating      string          `json:"rating"`
	Schedule    *scheduleResult `json:"schedule"`
	ReviewLogID int64           `json:"review_log_id"`
}

func reviewSubmit(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("review submit", flag.ContinueOnError)
	rating := fs.String("rating", "", "评分: again / hard / good / easy (必填)")
	expectedCardVersion := fs.Int64("expected-card-version", 0, "卡乐观锁版本, 取自 review due 的 CV (必填)")
	expectedScheduleVersion := fs.Int64("expected-schedule-version", 0, "调度乐观锁版本, 取自 review due 的 SV (必填)")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("review submit: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: review submit <card_id> --rating <again|hard|good|easy> --expected-card-version <v> --expected-schedule-version <v>")
	}
	cardID, err := parseID(positional[0])
	if err != nil {
		return err
	}
	switch *rating {
	case "again", "hard", "good", "easy":
	default:
		return NewUsageError("--rating 取值必须是 again / hard / good / easy")
	}
	if *expectedCardVersion < 1 {
		return NewUsageError("--expected-card-version 必填且为正整数")
	}
	if *expectedScheduleVersion < 1 {
		return NewUsageError("--expected-schedule-version 必填且为正整数")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	// 复习提交直接生效, 永不进审批, 但仍带 Idempotency-Key.
	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/api/cli/review/%d/submit", cardID),
		Body: map[string]any{
			"rating":                    *rating,
			"expected_card_version":     *expectedCardVersion,
			"expected_schedule_version": *expectedScheduleVersion,
		},
	})
	if err != nil {
		return wrapReviewConflict(err)
	}
	var result submitReviewResult
	if err := json.Unmarshal(raw.Body, &result); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if *jsonOut {
		return writeJSON(out, result)
	}
	fmt.Fprintf(out, "card_id:       %d\n", result.CardID)
	fmt.Fprintf(out, "rating:        %s\n", result.Rating)
	fmt.Fprintf(out, "review_log_id: %d\n", result.ReviewLogID)
	if s := result.Schedule; s != nil {
		fmt.Fprintf(out, "schedule:\n")
		fmt.Fprintf(out, "  due:            %s\n", s.Due.Local().Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "  state:          %s\n", s.State)
		fmt.Fprintf(out, "  stability:      %.4f\n", s.Stability)
		fmt.Fprintf(out, "  difficulty:     %.4f\n", s.Difficulty)
		fmt.Fprintf(out, "  scheduled_days: %d\n", s.ScheduledDays)
		fmt.Fprintf(out, "  reps:           %d\n", s.Reps)
		fmt.Fprintf(out, "  lapses:         %d\n", s.Lapses)
		fmt.Fprintf(out, "  version:        %d\n", s.Version)
	}
	return nil
}

// wrapReviewConflict 给复习提交的版本冲突附加重试指引.
// 复习提交带卡与调度双乐观锁, 任一不一致都会 409,
// 提示用户重新拉取 due 队列获取最新 CV/SV.
func wrapReviewConflict(err error) error {
	var apiErr *httpclient.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "version_conflict" {
		return fmt.Errorf("%w\n提示: 卡或调度版本已变化 (可能刚被复习或修改), 请重新执行 review due 获取最新 CV/SV 后重试", err)
	}
	return err
}

// RunUserProfile 实现 user-profile 子命令组: get / set.
// 画像更新直接生效, 不挂审批.
func RunUserProfile(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: get / set")
	}
	switch args[0] {
	case "get":
		return userProfileGet(args[1:], out)
	case "set":
		return userProfileSet(args[1:], out)
	default:
		return NewUsageError("未知子命令 %q, 可用: get / set", args[0])
	}
}

// userProfileResult 对应服务端 userProfileResponse.
type userProfileResult struct {
	Profile string `json:"profile"`
	Version int64  `json:"version"`
}

func userProfileGet(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("user-profile get", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("user-profile get: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("user-profile get 不接受额外参数: %s", fs.Arg(0))
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var profile userProfileResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   "/api/cli/user-profile",
	}, &profile)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, profile)
	}
	fmt.Fprintf(out, "profile:  %s\n", displayValue(profile.Profile))
	fmt.Fprintf(out, "version: %d\n", profile.Version)
	return nil
}

func userProfileSet(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("user-profile set", flag.ContinueOnError)
	profile := fs.String("profile", "", "画像内容, 允许空串表示清空 (必填)")
	expectedVersion := fs.Int64("expected-version", 0, "乐观锁版本号, 取自 user-profile get (必填, 首次写入用 0)")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("user-profile set: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("user-profile set 不接受额外参数: %s", fs.Arg(0))
	}

	// --profile 必须显式设置: 空串是合法值 (清空画像),
	// 未设置与空串必须区分, 否则会把清空误当缺省.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if !setFlags["profile"] {
		return NewUsageError("--profile 必填 (空串表示清空画像)")
	}
	if !setFlags["expected-version"] {
		return NewUsageError("--expected-version 必填 (首次写入用 0)")
	}
	if *expectedVersion < 0 {
		return NewUsageError("--expected-version 不能为负数 (首次写入用 0)")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPut,
		Path:   "/api/cli/user-profile",
		Body: map[string]any{
			"profile":          *profile,
			"expected_version": *expectedVersion,
		},
	})
	if err != nil {
		return wrapVersionConflict(err)
	}
	var result userProfileResult
	if err := json.Unmarshal(raw.Body, &result); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if *jsonOut {
		return writeJSON(out, map[string]any{"outcome": "applied", "profile": result})
	}
	fmt.Fprintf(out, "已保存画像 (version: %d)\n", result.Version)
	fmt.Fprintf(out, "profile: %s\n", displayValue(result.Profile))
	return nil
}
