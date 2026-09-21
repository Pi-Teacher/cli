package commands

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Pi-Teacher/cli/internal/httpclient"
)

// RunCard 实现 card 子命令组: list / get / create / update / trash / check / merge.
// 回收站恢复属于 trash 命令组.
func RunCard(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: list / get <id> / create / update <id> / trash <id> / check / merge <id1> <id2>")
	}
	switch args[0] {
	case "list":
		return listCards(args[1:], out)
	case "get":
		return getCard(args[1:], out)
	case "create":
		return createCard(args[1:], out)
	case "update":
		return updateCard(args[1:], out)
	case "trash":
		return trashCard(args[1:], out)
	case "check":
		return checkCard(args[1:], out)
	case "merge":
		return mergeCard(args[1:], out)
	default:
		return NewUsageError("未知子命令 %q, 可用: list / get <id> / create / update <id> / trash <id> / check / merge <id1> <id2>", args[0])
	}
}

// cardResult 对应服务端 cardResponse (列表项, 不含 schedule).
type cardResult struct {
	ID              int64     `json:"id"`
	TopicID         *int64    `json:"topic_id"`
	Front           string    `json:"front"`
	Back            string    `json:"back"`
	EnableEmbedding bool      `json:"enable_embedding"`
	EmbeddingStatus string    `json:"embedding_status"`
	Version         int64     `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// scheduleResult 对应服务端 scheduleResponse (FSRS 调度快照).
type scheduleResult struct {
	Due           time.Time  `json:"due"`
	State         string     `json:"state"`
	Stability     float64    `json:"stability"`
	Difficulty    float64    `json:"difficulty"`
	ScheduledDays int64      `json:"scheduled_days"`
	Reps          int64      `json:"reps"`
	Lapses        int64      `json:"lapses"`
	LastReviewAt  *time.Time `json:"last_review_at"`
	Version       int64      `json:"version"`
}

// cardDetailResult 对应服务端 cardDetailResponse, 在列表项基础上
// 增加 embedding_error 与 schedule; 向量 BLOB 服务端永不返回.
type cardDetailResult struct {
	cardResult
	EmbeddingError *string         `json:"embedding_error"`
	Schedule       *scheduleResult `json:"schedule"`
}

// cardListResult 对应服务端分页响应.
type cardListResult struct {
	Items    []cardResult `json:"items"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

// --- list ---

func listCards(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("card list", flag.ContinueOnError)
	page := fs.Int("page", 1, "页码, 从 1 开始")
	pageSize := fs.Int("page-size", 20, "每页条数, 上限 100")
	q := fs.String("q", "", "按 front/back 模糊搜索")
	topicID := fs.Int64("topic-id", 0, "按 Topic 筛选, 0 表示无 Topic 的卡")
	embeddingStatus := fs.String("embedding-status", "", "按向量状态筛选: pending / processing / ready / failed / disabled")
	sortField := fs.String("sort", "created_at", "排序字段: created_at / updated_at")
	order := fs.String("order", "desc", "排序方向: asc / desc")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("card list: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("card list 不接受额外参数: %s", fs.Arg(0))
	}
	if *page < 1 {
		return NewUsageError("--page 必须从 1 开始")
	}
	if *pageSize < 1 || *pageSize > 100 {
		return NewUsageError("--page-size 必须在 1 到 100 之间")
	}
	if *embeddingStatus != "" {
		switch *embeddingStatus {
		case "pending", "processing", "ready", "failed", "disabled":
		default:
			return NewUsageError("--embedding-status 取值: pending / processing / ready / failed / disabled")
		}
	}
	if *sortField != "created_at" && *sortField != "updated_at" {
		return NewUsageError("--sort 取值: created_at / updated_at")
	}
	if *order != "asc" && *order != "desc" {
		return NewUsageError("--order 取值: asc / desc")
	}

	// topic-id 未显式设置时不应筛选; 显式设置 0 是"无 Topic 卡"的合法查询.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	query := url.Values{}
	query.Set("page", fmt.Sprint(*page))
	query.Set("page_size", fmt.Sprint(*pageSize))
	if *q != "" {
		query.Set("q", *q)
	}
	if setFlags["topic-id"] {
		if *topicID < 0 {
			return NewUsageError("--topic-id 不能为负数")
		}
		query.Set("topic_id", fmt.Sprint(*topicID))
	}
	if *embeddingStatus != "" {
		query.Set("embedding_status", *embeddingStatus)
	}
	if setFlags["sort"] {
		query.Set("sort", *sortField)
	}
	if setFlags["order"] {
		query.Set("order", *order)
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var result cardListResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   "/api/cli/cards?" + query.Encode(),
	}, &result)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, result)
	}
	return printCardList(out, result)
}

// printCardList 以表格输出 Card 列表与分页摘要.
func printCardList(out io.Writer, result cardListResult) error {
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tFRONT\tTOPIC\tEMB\tSTATUS\tVERSION\tUPDATED_AT")
	for _, c := range result.Items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%t\t%s\t%d\t%s\n",
			c.ID, truncateRunes(c.Front, 30), topicLabel(c.TopicID),
			c.EnableEmbedding, c.EmbeddingStatus, c.Version,
			c.UpdatedAt.Local().Format("2006-01-02 15:04"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	totalPages := (result.Total + int64(result.PageSize) - 1) / int64(result.PageSize)
	if totalPages == 0 {
		totalPages = 1
	}
	fmt.Fprintf(out, "共 %d 条, 第 %d/%d 页 (每页 %d)\n",
		result.Total, result.Page, totalPages, result.PageSize)
	return nil
}

// topicLabel 把可空 TopicID 转为展示文本.
func topicLabel(id *int64) string {
	if id == nil {
		return "-"
	}
	return fmt.Sprint(*id)
}

// --- get ---

func getCard(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("card get", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("card get: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: card get <id>")
	}
	id, err := parseID(positional[0])
	if err != nil {
		return err
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var detail cardDetailResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/cli/cards/%d", id),
	}, &detail)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, detail)
	}
	printCardDetail(out, detail)
	return nil
}

// printCardDetail 以键值对输出 Card 详情与 FSRS 调度快照.
func printCardDetail(out io.Writer, d cardDetailResult) {
	fmt.Fprintf(out, "id:               %d\n", d.ID)
	fmt.Fprintf(out, "topic_id:         %s\n", topicLabel(d.TopicID))
	fmt.Fprintf(out, "front:            %s\n", d.Front)
	fmt.Fprintf(out, "back:             %s\n", d.Back)
	fmt.Fprintf(out, "enable_embedding: %t\n", d.EnableEmbedding)
	fmt.Fprintf(out, "embedding_status: %s\n", d.EmbeddingStatus)
	if d.EmbeddingError != nil {
		fmt.Fprintf(out, "embedding_error:  %s\n", *d.EmbeddingError)
	}
	fmt.Fprintf(out, "version:          %d\n", d.Version)
	fmt.Fprintf(out, "created_at:       %s\n", d.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(out, "updated_at:       %s\n", d.UpdatedAt.Local().Format("2006-01-02 15:04:05"))
	if s := d.Schedule; s != nil {
		fmt.Fprintln(out, "schedule:")
		fmt.Fprintf(out, "  due:            %s\n", s.Due.Local().Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "  state:          %s\n", s.State)
		fmt.Fprintf(out, "  stability:      %.4f\n", s.Stability)
		fmt.Fprintf(out, "  difficulty:     %.4f\n", s.Difficulty)
		fmt.Fprintf(out, "  scheduled_days: %d\n", s.ScheduledDays)
		fmt.Fprintf(out, "  reps:           %d\n", s.Reps)
		fmt.Fprintf(out, "  lapses:         %d\n", s.Lapses)
		if s.LastReviewAt != nil {
			fmt.Fprintf(out, "  last_review_at: %s\n", s.LastReviewAt.Local().Format("2006-01-02 15:04:05"))
		}
		fmt.Fprintf(out, "  version:        %d\n", s.Version)
	}
}

// --- create ---

func createCard(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("card create", flag.ContinueOnError)
	front := fs.String("front", "", "卡片正面问题 (必填)")
	back := fs.String("back", "", "卡片背面答案 (必填)")
	topicID := fs.Int64("topic-id", 0, "所属 Topic id, 省略表示无 Topic")
	noEmbedding := fs.Bool("no-embedding", false, "不生成向量, 缺省会生成")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("card create: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("card create 不接受额外参数: %s", fs.Arg(0))
	}
	if *front == "" {
		return NewUsageError("--front 不能为空")
	}
	if *back == "" {
		return NewUsageError("--back 不能为空")
	}

	// topic-id 未设置不传 (服务端存 null); 设置时必须是已存在的 Topic.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	body := map[string]any{"front": *front, "back": *back}
	if setFlags["topic-id"] {
		if *topicID < 1 {
			return NewUsageError("--topic-id 必须是正整数 (无 Topic 请省略该参数)")
		}
		body["topic_id"] = *topicID
	}
	if *noEmbedding {
		body["enable_embedding"] = false
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   "/api/cli/cards",
		Body:   body,
	})
	if err != nil {
		return wrapVersionConflict(err)
	}
	return handleCardWrite(raw, *jsonOut, out)
}

// --- update ---

func updateCard(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("card update", flag.ContinueOnError)
	expectedVersion := fs.Int64("expected-version", 0, "乐观锁版本号, 取自 card get 的 version (必填)")
	front := fs.String("front", "", "新正面, 省略表示不修改")
	back := fs.String("back", "", "新背面, 省略表示不修改")
	topicID := fs.Int64("topic-id", 0, "新 Topic id, 0 表示设为无 Topic, 省略表示不修改")
	enableEmbedding := fs.Bool("enable-embedding", false, "开启向量生成")
	noEmbedding := fs.Bool("no-embedding", false, "关闭向量生成")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("card update: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: card update <id> --expected-version <v> [--front f] [--back b] [--topic-id t] [--enable-embedding|--no-embedding]")
	}
	id, err := parseID(positional[0])
	if err != nil {
		return err
	}
	if *expectedVersion < 1 {
		return NewUsageError("--expected-version 必填且为正整数")
	}
	if *enableEmbedding && *noEmbedding {
		return NewUsageError("--enable-embedding 与 --no-embedding 只能二选一")
	}

	// 三态语义: 只有显式设置的字段进入请求体.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if !setFlags["front"] && !setFlags["back"] && !setFlags["topic-id"] &&
		!setFlags["enable-embedding"] && !setFlags["no-embedding"] {
		return NewUsageError("至少提供一个修改字段: --front / --back / --topic-id / --enable-embedding / --no-embedding")
	}
	body := map[string]any{"expected_version": *expectedVersion}
	if setFlags["front"] {
		body["front"] = *front
	}
	if setFlags["back"] {
		body["back"] = *back
	}
	if setFlags["topic-id"] {
		if *topicID < 0 {
			return NewUsageError("--topic-id 不能为负数 (0 表示设为无 Topic)")
		}
		// 服务端 topic_id 三态: 数字指定, null 设为无 Topic.
		// 0 在 CLI 侧表示"设为无 Topic", 转成 null 传给服务端.
		if *topicID == 0 {
			body["topic_id"] = nil
		} else {
			body["topic_id"] = *topicID
		}
	}
	if setFlags["enable-embedding"] {
		body["enable_embedding"] = *enableEmbedding
	}
	if setFlags["no-embedding"] {
		body["enable_embedding"] = !*noEmbedding
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPatch,
		Path:   fmt.Sprintf("/api/cli/cards/%d", id),
		Body:   body,
	})
	if err != nil {
		return wrapVersionConflict(err)
	}
	return handleCardWrite(raw, *jsonOut, out)
}

// --- trash ---

func trashCard(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("card trash", flag.ContinueOnError)
	expectedVersion := fs.Int64("expected-version", 0, "乐观锁版本号, 取自 card get 的 version (必填)")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("card trash: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: card trash <id> --expected-version <v>")
	}
	id, err := parseID(positional[0])
	if err != nil {
		return err
	}
	if *expectedVersion < 1 {
		return NewUsageError("--expected-version 必填且为正整数")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/api/cli/cards/%d/trash", id),
		Body:   map[string]any{"expected_version": *expectedVersion},
	})
	if err != nil {
		return wrapVersionConflict(err)
	}
	if raw.Status == http.StatusAccepted {
		return printProposalOutcome(raw, *jsonOut, out)
	}
	var trashed struct {
		TrashedCardID int64 `json:"trashed_card_id"`
	}
	if err := json.Unmarshal(raw.Body, &trashed); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if *jsonOut {
		return writeJSON(out, map[string]any{"outcome": "applied", "trashed_card_id": trashed.TrashedCardID})
	}
	fmt.Fprintf(out, "已回收 card %d\n", trashed.TrashedCardID)
	return nil
}

// --- check ---

// checkMatchResult 对应查重候选, semantic 分支带 similarity.
type checkMatchResult struct {
	ID         int64    `json:"id"`
	Front      string   `json:"front"`
	Back       string   `json:"back"`
	TopicID    *int64   `json:"topic_id"`
	Similarity *float64 `json:"similarity"`
}

// checkCoverageResult 对应语义分支的覆盖率信息.
type checkCoverageResult struct {
	TotalEnabled int64   `json:"total_enabled"`
	Ready        int64   `json:"ready"`
	Pending      int64   `json:"pending"`
	Processing   int64   `json:"processing"`
	Failed       int64   `json:"failed"`
	ReadyPercent float64 `json:"ready_percent"`
}

// checkResult 对应查重响应体.
type checkResult struct {
	MatchType string               `json:"match_type"`
	Coverage  *checkCoverageResult `json:"coverage"`
	Matches   []checkMatchResult   `json:"matches"`
}

func checkCard(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("card check", flag.ContinueOnError)
	front := fs.String("front", "", "待查重的卡片正面问题 (必填)")
	noEmbedding := fs.Bool("no-embedding", false, "只做精确查重, 不做语义相似度")
	topK := fs.Int("top-k", 5, "语义查重返回的候选数量, 上限 50")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("card check: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("card check 不接受额外参数: %s", fs.Arg(0))
	}
	if *front == "" {
		return NewUsageError("--front 不能为空")
	}
	if *topK < 1 || *topK > 50 {
		return NewUsageError("--top-k 必须在 1 到 50 之间")
	}

	body := map[string]any{"front": *front, "top_k": *topK}
	if *noEmbedding {
		body["enable_embedding"] = false
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	// check 是只读 dry-run, 永不进审批, 但仍带 Idempotency-Key.
	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   "/api/cli/cards/check",
		Body:   body,
	})
	if err != nil {
		return wrapCheckError(err)
	}
	var result checkResult
	if err := json.Unmarshal(raw.Body, &result); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if *jsonOut {
		return writeJSON(out, result)
	}
	return printCheckResult(out, result)
}

// printCheckResult 输出查重结果: 精确命中直接列出,
// 语义分支附带覆盖率说明, 帮助判断相似度结果的可信度.
func printCheckResult(out io.Writer, result checkResult) error {
	fmt.Fprintf(out, "match_type: %s\n", result.MatchType)
	if result.Coverage != nil {
		fmt.Fprintf(out, "coverage: ready %d/%d (%.1f%%), pending %d, processing %d, failed %d\n",
			result.Coverage.Ready, result.Coverage.TotalEnabled, result.Coverage.ReadyPercent,
			result.Coverage.Pending, result.Coverage.Processing, result.Coverage.Failed)
	}
	if len(result.Matches) == 0 {
		fmt.Fprintln(out, "matches: (无)")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	if result.MatchType == "semantic" {
		fmt.Fprintln(tw, "ID\tSIMILARITY\tFRONT")
		for _, m := range result.Matches {
			fmt.Fprintf(tw, "%d\t%.4f\t%s\n", m.ID, *m.Similarity, truncateRunes(m.Front, 40))
		}
	} else {
		fmt.Fprintln(tw, "ID\tFRONT\tBACK")
		for _, m := range result.Matches {
			fmt.Fprintf(tw, "%d\t%s\t%s\n", m.ID, truncateRunes(m.Front, 40), truncateRunes(m.Back, 40))
		}
	}
	return tw.Flush()
}

// wrapCheckError 给查重的两类可恢复错误附加行动指引.
func wrapCheckError(err error) error {
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Code {
	case "similarity_disabled":
		return fmt.Errorf("%w\n提示: 语义相似度检测已在服务端设置中关闭, 可在 WebUI 设置开启后重试, 或加 --no-embedding 只做精确查重", err)
	case "embedding_unavailable":
		return fmt.Errorf("%w\n提示: embedding 服务暂不可用, 请稍后重试, 或加 --no-embedding 只做精确查重", err)
	}
	return err
}

// --- merge ---

// mergeCard 实现 card merge: 合并两张来源卡为一张新卡, 来源卡进回收站.
// front/back 缺省按 Q1/Q2 规则拼接; topic_id 与 enable_embedding 缺省继承,
// 两张来源卡取值不同时服务端报 409 要求显式指定.
func mergeCard(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("card merge", flag.ContinueOnError)
	front := fs.String("front", "", "新卡正面, 省略按 Q1/Q2 拼接来源卡")
	back := fs.String("back", "", "新卡背面, 省略按 Q1/Q2 拼接来源卡")
	topicID := fs.Int64("topic-id", 0, "新卡 Topic, 0 表示无 Topic, 省略继承 (来源不同时必须显式指定)")
	enableEmbedding := fs.Bool("enable-embedding", false, "新卡开启向量生成")
	noEmbedding := fs.Bool("no-embedding", false, "新卡关闭向量生成")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("card merge: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 2 {
		return NewUsageError("用法: card merge <id1> <id2> [--front f] [--back b] [--topic-id t] [--enable-embedding|--no-embedding]")
	}
	id1, err := parseID(positional[0])
	if err != nil {
		return err
	}
	id2, err := parseID(positional[1])
	if err != nil {
		return err
	}
	if id1 == id2 {
		return NewUsageError("两张来源卡不能相同")
	}
	if *enableEmbedding && *noEmbedding {
		return NewUsageError("--enable-embedding 与 --no-embedding 只能二选一")
	}

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	body := map[string]any{"source_card_ids": []int64{id1, id2}}
	if setFlags["front"] {
		body["front"] = *front
	}
	if setFlags["back"] {
		body["back"] = *back
	}
	if setFlags["topic-id"] {
		if *topicID < 0 {
			return NewUsageError("--topic-id 不能为负数 (0 表示无 Topic)")
		}
		if *topicID == 0 {
			body["topic_id"] = nil
		} else {
			body["topic_id"] = *topicID
		}
	}
	if setFlags["enable-embedding"] {
		body["enable_embedding"] = *enableEmbedding
	}
	if setFlags["no-embedding"] {
		body["enable_embedding"] = !*noEmbedding
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   "/api/cli/cards/merge",
		Body:   body,
	})
	if err != nil {
		return wrapMergeConflict(err)
	}
	return handleCardWrite(raw, *jsonOut, out)
}

// wrapMergeError 给合并的两类 409 冲突附加行动指引.
// 两张来源卡 Topic 或 enable_embedding 取值不同时, 服务端拒绝缺省继承,
// 必须由调用方显式指定, 提示里直接给出对应 flag.
func wrapMergeConflict(err error) error {
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Code {
	case "merge_topic_required":
		return fmt.Errorf("%w\n提示: 两张来源卡的 Topic 不同, 请用 --topic-id <id> 指定新卡 Topic (0 表示无 Topic)", err)
	case "merge_embedding_required":
		return fmt.Errorf("%w\n提示: 两张来源卡的 enable_embedding 不同, 请用 --enable-embedding 或 --no-embedding 指定", err)
	}
	return wrapVersionConflict(err)
}

// --- 写结果公共处理 ---

// handleCardWrite 处理 create/update 的双形态响应:
// 202 为提案 (未生效), 2xx 为直写结果 (Card 详情).
func handleCardWrite(raw *httpclient.RawResponse, jsonOut bool, out io.Writer) error {
	if raw.Status == http.StatusAccepted {
		return printProposalOutcome(raw, jsonOut, out)
	}
	var detail cardDetailResult
	if err := json.Unmarshal(raw.Body, &detail); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if jsonOut {
		return writeJSON(out, map[string]any{"outcome": "applied", "card": detail})
	}
	printCardDetail(out, detail)
	return nil
}

// truncateRunes 按字符数截断文本用于表格展示, 超长加省略号.
// 换行替换为空格, 避免 Q1/Q2 拼接类多行内容破坏表格对齐.
func truncateRunes(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
