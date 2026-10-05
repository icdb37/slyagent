package repl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slyagent/inp"
	"strings"
)

type ToolWriteParam struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *ToolWriteParam) Format() string {
	return fmt.Sprintf("path=%s, %d 字节", t.Path, len(t.Content))
}

type ToolPatchHunk struct {
	prefix []string // 变动内容之前n行
	suffix []string // 变动内容之后n行
	Old    string   `json:"old"`
	New    string   `json:"new"`
}

type ToolPatchParam struct {
	Path  string           `json:"path"`
	Hunks []*ToolPatchHunk `json:"hunks"`
}

func init() {
	BuiltIns["write"] = &UnitTool{
		Schema: &XllmTool{
			Name:        "write",
			Description: "整体覆盖写入一个文本文件（破坏性操作，会请求用户确认）。path 与 content 均为必填，content 是文件的完整新内容，原内容将被整体替换；缺失的父目录会自动创建。适合新建文件或整体重写，小幅修改请优先用 patch。",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"path": { "type": "string", "description": "要写入的文件路径。" },
					"content": { "type": "string", "description": "文件的完整新内容。" }
				},
				"required":["path","content"],
				"additionalProperties":false
			}`),
		},
		Param: &ToolWriteParam{},
		Process: func(a any) (any, error) {
			tp, ok := a.(*ToolWriteParam)
			if !ok {
				return nil, fmt.Errorf("write param type invalid，请检查代码之后重新运行")
			}
			oldInfo := "新文件"
			if info, err := os.Stat(tp.Path); err == nil && !info.IsDir() {
				oldInfo = fmt.Sprintf("覆盖已有文件（%d 字节）", info.Size())
			}
			prompt := fmt.Sprintf("即将%s：%s（%d 字节）; [y/N]: ", oldInfo, tp.Format(), len(tp.Content))
			os.Stdout.WriteString(prompt)
			ans := inp.ReadAnswer(3, true, true, " 重新输入: : ", "y", "yes", "ok", "n", "no")
			if ans != "y" && ans != "yes" && ans != "ok" {
				return nil, fmt.Errorf("用户取消运行命令")
			}
			if dir := filepath.Dir(tp.Path); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return nil, fmt.Errorf("创建父目录 %s 失败：%w", dir, err)
				}
			}
			if err := os.WriteFile(tp.Path, []byte(tp.Content), 0644); err != nil {
				return nil, fmt.Errorf("写入文件 %s 失败：%w", tp.Path, err)
			}
			return fmt.Sprintf("已写入 %d 字节到 %s", len(tp.Content), tp.Path), nil
		},
	}
	BuiltIns["patch"] = &UnitTool{
		Schema: &XllmTool{
			Name:        "patch",
			Description: "对已有文本文件做局部修改：hunks 里每个 { old, new } 把文件中唯一出现的 old 片段替换为 new。会请求用户确认；old 须在文件中唯一匹配，否则该片段失败。",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"path": { "type": "string", "description": "要修改的文件路径。" },
					"hunks": {
						"type": "array",
						"description": "修改片段列表：每个片段把文件中唯一匹配的 old 替换为 new。",
						"items": {
							"type": "object",
							"properties": {
								"old": { "type": "string", "description": "要从文件中替换的原文片段，须唯一匹配（建议带上足够上下文）。" },
								"new": { "type": "string", "description": "替换后的新文本。" }
							},
							"required": ["old", "new"],
							"additionalProperties": false
						}
					}
				},
				"required": ["path", "hunks"],
				"additionalProperties": false
			}`),
		},
		Param: &ToolPatchParam{},
		Process: func(a any) (any, error) {
			tp, ok := a.(*ToolPatchParam)
			if !ok {
				return nil, fmt.Errorf("patch param type invalid，请检查代码之后重新运行")
			}
			if tp.Path == "" {
				return nil, fmt.Errorf("缺少参数 path")
			}
			if len(tp.Hunks) == 0 {
				return nil, fmt.Errorf("hunks 为空，未做任何修改")
			}
			data, err := os.ReadFile(tp.Path)
			if err != nil {
				return nil, fmt.Errorf("读取文件 %s 失败：%w", tp.Path, err)
			}
			original := string(data)
			patched := original
			for i, h := range tp.Hunks {
				idx, err := processHunk(data, i, h)
				if err != nil {
					return nil, err
				}
				patched = patched[:idx] + h.New + patched[idx+len(h.Old):]
			}
			if patched == original {
				return fmt.Sprintf("替换后内容与原文件一致，未做任何修改：%s", tp.Path), nil
			}
			prompt := fmt.Sprintf("\n即将对 %s 应用 %d 处修改（%d → %d 字节）：\n%s\n确认写入? [y/N]: ",
				tp.Path, len(tp.Hunks), len(original), len(patched), formatHunks(tp.Hunks))
			os.Stdout.WriteString(prompt)
			ans := inp.ReadAnswer(3, true, true, " 重新输入: : ", "y", "yes", "ok", "n", "no")
			if ans != "y" && ans != "yes" && ans != "ok" {
				return nil, fmt.Errorf("用户取消运行命令")
			}
			if err := os.WriteFile(tp.Path, []byte(patched), 0644); err != nil {
				return nil, fmt.Errorf("写入文件 %s 失败：%w", tp.Path, err)
			}
			return fmt.Sprintf("已应用 %d 处修改到 %s", len(tp.Hunks), tp.Path), nil
		},
	}
}

// truncate 把 s 截断到最多 n 个 rune，超长末尾追加 "..."
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// formatHunks 把 hunks 渲染成简单的 diff 摘要，每段用 truncate 限长
func formatHunks(hunks []*ToolPatchHunk) string {
	var sb strings.Builder
	for i, h := range hunks {
		prefixs := make([]string, 0, len(h.prefix))
		for i := range h.prefix {
			size := len(h.prefix[i])
			if size <= patchCtxLineMax {
				prefixs = append(prefixs, h.prefix[i])
				continue
			}
			prefixs = append(prefixs, h.prefix[i][:patchCtxLineMax/2]+" ... "+h.prefix[i][size-patchCtxLineMax/2:])
		}
		suffixs := make([]string, 0, len(h.suffix))
		for i := range h.suffix {
			size := len(h.suffix[i])
			if size <= patchCtxLineMax {
				suffixs = append(suffixs, h.suffix[i])
				continue
			}
			suffixs = append(suffixs, h.suffix[i][:patchCtxLineMax/2]+" ... "+h.suffix[i][size-patchCtxLineMax/2:])
		}
		sepStar := strings.Repeat("*", 30)
		sepLine := fmt.Sprintf("%shunks[%d]%s", sepStar, i, sepStar)
		fmt.Fprintf(&sb, "\n%s\n%s\n- %s\n+ %s\n%s\n%s\n",
			sepLine, strings.Join(prefixs, "\n"), truncate(h.Old, 80),
			truncate(h.New, 80), strings.Join(suffixs, "\n"), sepLine)
	}
	return sb.String()
}

const (
	patchCtxLineNum = 3
	patchCtxLineMax = 1000
)

func processHunk(data []byte, i int, h *ToolPatchHunk) (int, error) {
	if h.Old == "" {
		return 0, fmt.Errorf("hunks[%d] 的 old 为空", i)
	}
	idx := bytes.Index(data, []byte(h.Old))
	if idx < 0 {
		return 0, fmt.Errorf("hunks[%d] 未找到匹配片段：%s", i, truncate(h.Old, 80))
	}
	if bytes.Contains(data[idx+len(h.Old):], []byte(h.Old)) {
		return 0, fmt.Errorf("hunks[%d] 片段匹配到多处，请加长 old 使其唯一：%s", i, truncate(h.Old, 80))
	}
	h.prefix = make([]string, 0, patchCtxLineNum)
	h.suffix = make([]string, 0, patchCtxLineNum)
	posBeg, posEnd := 0, idx
	for i := 0; i < patchCtxLineNum; i++ {
		if posBeg = bytes.LastIndex(data[:posEnd], []byte("\n")); posBeg == -1 {
			break
		}
		h.prefix = append(h.prefix, string(bytes.TrimSpace(data[posBeg:posEnd])))
		posEnd = posBeg
	}
	posBeg, posEnd = idx+len(h.Old), 0
	for i := 0; i < patchCtxLineNum; i++ {
		if posEnd = bytes.Index(data[posBeg:], []byte("\n")); posEnd == -1 {
			break
		}
		h.suffix = append(h.suffix, string(bytes.TrimSpace(data[posBeg:posBeg+posEnd])))
		posBeg = posBeg + posEnd + 1
	}
	return idx, nil
}
