package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"
	"time"

	"github.com/Pi-Teacher/cli/internal/httpclient"
)

// RunTrash 实现 trash 子命令组: cards / topics / glossary 三类回收站的
// 列表与单条恢复. 批量恢复与永久删除属于 WebUI, 不在 CLI 范围.
func RunTrash(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: cards|topics|glossary list / cards|topics|glossary restore <id>")
	}
	entity, rest := args[0], args[1:]
	switch entity {
	case "cards", "topics", "glossary":
	default:
		return NewUsageError("未知回收站类型 %q, 可用: cards / topics / glossary", entity)
	}
	if len(rest) == 0 {
		return NewUsageError("缺少子命令, 可用: %s list / %s restore <id>", entity, entity)
	}
	switch rest[0] {
	case "list":
		return listTrashed(entity, rest[1:], out)
	case "restore":
		return restoreTrashed(entity, rest[1:], out)
	default:
		return NewUsageError("未知子命令 %q, 可用: %s list / %s restore <id>", rest[0], entity, entity)
	}
}

// trashedCardItem 对应回收站 Card 列表项.
type trashedCardItem struct {
	ID              int64     `json:"id"`
	Front           string    `json:"front"`
	Back            string    `json:"back"`
	EnableEmbedding bool      `json:"enable_embedding"`
	Version         int64     `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	TrashedAt       time.Time `json:"trashed_at"`
}

// trashedTopicItem 对应回收站 Topic 列表项.
// 关联卡在回收时已清空, card_count 恒为 0.
type trashedTopicItem struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CardCount   int64     `json:"card_count"`
	Version     int64     `json:"version"`
	TrashedAt   time.Time `json:"trashed_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// trashedGlossaryItem 对应回收站 Glossary 列表项.
type trashedGlossaryItem struct {
	ID         int64     `json:"id"`
	Term       string    `json:"term"`
	Definition string    `json:"definition"`
	Version    int64     `json:"version"`
	TrashedAt  time.Time `json:"trashed_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// trashedListResult 是三类回收站共用的分页信封.
// Items 用 json.RawMessage 延迟解码, 由各实体的展示分支自行解析,
// 避免 reflect 或三份重复的分页样板.
type trashedListResult struct {
	Items    []json.RawMessage `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

// --- list ---

func listTrashed(entity string, args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("trash "+entity+" list", flag.ContinueOnError)
	page := fs.Int("page", 1, "页码, 从 1 开始")
	pageSize := fs.Int("page-size", 20, "每页条数, 上限 100")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("trash %s list: %v", entity, err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("trash %s list 不接受额外参数: %s", entity, fs.Arg(0))
	}
	if *page < 1 {
		return NewUsageError("--page 必须从 1 开始")
	}
	if *pageSize < 1 || *pageSize > 100 {
		return NewUsageError("--page-size 必须在 1 到 100 之间")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var result trashedListResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/cli/trash/%s?page=%d&page_size=%d", entity, *page, *pageSize),
	}, &result)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, result)
	}
	return printTrashedList(entity, result, out)
}

// printTrashedList 按实体类型展示回收站列表.
func printTrashedList(entity string, result trashedListResult, out io.Writer) error {
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	switch entity {
	case "cards":
		fmt.Fprintln(tw, "ID\tFRONT\tVERSION\tTRASHED_AT")
		for _, raw := range result.Items {
			var c trashedCardItem
			if err := json.Unmarshal(raw, &c); err != nil {
				return fmt.Errorf("回收站 Card 数据解析失败: %w", err)
			}
			fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n",
				c.ID, truncateRunes(c.Front, 30), c.Version, c.TrashedAt.Local().Format("2006-01-02 15:04"))
		}
	case "topics":
		fmt.Fprintln(tw, "ID\tNAME\tVERSION\tTRASHED_AT")
		for _, raw := range result.Items {
			var t trashedTopicItem
			if err := json.Unmarshal(raw, &t); err != nil {
				return fmt.Errorf("回收站 Topic 数据解析失败: %w", err)
			}
			fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n",
				t.ID, truncateRunes(t.Name, 30), t.Version, t.TrashedAt.Local().Format("2006-01-02 15:04"))
		}
	case "glossary":
		fmt.Fprintln(tw, "ID\tTERM\tVERSION\tTRASHED_AT")
		for _, raw := range result.Items {
			var g trashedGlossaryItem
			if err := json.Unmarshal(raw, &g); err != nil {
				return fmt.Errorf("回收站 Glossary 数据解析失败: %w", err)
			}
			fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n",
				g.ID, truncateRunes(g.Term, 30), g.Version, g.TrashedAt.Local().Format("2006-01-02 15:04"))
		}
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

// --- restore ---

func restoreTrashed(entity string, args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("trash "+entity+" restore", flag.ContinueOnError)
	expectedVersion := fs.Int64("expected-version", 0, "乐观锁版本号, 取自回收站列表的 version (必填)")
	topicID := fs.Int64("topic-id", 0, "恢复后的 Card 所属 Topic, 0 或省略表示无 Topic (仅 cards 适用)")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("trash %s restore: %v", entity, err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: trash %s restore <id> --expected-version <v>", entity)
	}
	id, err := parseID(positional[0])
	if err != nil {
		return err
	}
	if *expectedVersion < 1 {
		return NewUsageError("--expected-version 必填且为正整数")
	}

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if setFlags["topic-id"] && entity != "cards" {
		return NewUsageError("--topic-id 仅适用于 cards 恢复")
	}
	if setFlags["topic-id"] && *topicID < 0 {
		return NewUsageError("--topic-id 不能为负数 (0 表示无 Topic)")
	}

	body := map[string]any{"expected_version": *expectedVersion}
	if setFlags["topic-id"] {
		// 服务端 topic_id 为可空指针: 0 与省略都表示恢复为无 Topic.
		if *topicID == 0 {
			body["topic_id"] = nil
		} else {
			body["topic_id"] = *topicID
		}
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/api/cli/trash/%s/%d/restore", entity, id),
		Body:   body,
	})
	if err != nil {
		return wrapVersionConflict(err)
	}
	if raw.Status == http.StatusAccepted {
		return printProposalOutcome(raw, *jsonOut, out)
	}
	return printRestoreResult(entity, raw.Body, *jsonOut, out)
}

// printRestoreResult 展示恢复结果. 三类实体的直写响应结构不同:
// cards 返回新旧 id 映射 (恢复会生成新 id), topics/glossary 返回实体详情.
func printRestoreResult(entity string, body []byte, jsonOut bool, out io.Writer) error {
	switch entity {
	case "cards":
		var restored struct {
			TrashCardID int64 `json:"trash_card_id"`
			NewCardID   int64 `json:"new_card_id"`
		}
		if err := json.Unmarshal(body, &restored); err != nil {
			return fmt.Errorf("响应体不是合法 JSON: %w", err)
		}
		if jsonOut {
			return writeJSON(out, map[string]any{"outcome": "applied", "trash_card_id": restored.TrashCardID, "new_card_id": restored.NewCardID})
		}
		fmt.Fprintf(out, "已恢复回收站卡 %d 为新卡 %d\n", restored.TrashCardID, restored.NewCardID)
		return nil
	case "topics":
		var topic topicResult
		if err := json.Unmarshal(body, &topic); err != nil {
			return fmt.Errorf("响应体不是合法 JSON: %w", err)
		}
		if jsonOut {
			return writeJSON(out, map[string]any{"outcome": "applied", "topic": topic})
		}
		printTopic(out, topic)
		return nil
	default: // glossary
		var glossary struct {
			ID         int64     `json:"id"`
			Term       string    `json:"term"`
			Definition string    `json:"definition"`
			Version    int64     `json:"version"`
			CreatedAt  time.Time `json:"created_at"`
			UpdatedAt  time.Time `json:"updated_at"`
		}
		if err := json.Unmarshal(body, &glossary); err != nil {
			return fmt.Errorf("响应体不是合法 JSON: %w", err)
		}
		if jsonOut {
			return writeJSON(out, map[string]any{"outcome": "applied", "glossary": glossary})
		}
		fmt.Fprintf(out, "id:         %d\n", glossary.ID)
		fmt.Fprintf(out, "term:       %s\n", glossary.Term)
		fmt.Fprintf(out, "definition: %s\n", glossary.Definition)
		fmt.Fprintf(out, "version:    %d\n", glossary.Version)
		fmt.Fprintf(out, "created_at: %s\n", glossary.CreatedAt.Local().Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "updated_at: %s\n", glossary.UpdatedAt.Local().Format("2006-01-02 15:04:05"))
		return nil
	}
}
