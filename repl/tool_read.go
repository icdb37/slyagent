package repl

import (
	"encoding/json"
	"fmt"
	"os"
)

type ToolLsParam struct {
	Path string `json:"path"`
}

type ToolReadParam struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
}

type ToolGrepParam struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
}

type ToolFindParam struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
}

func init() {
	BuiltIns["ls"] = &UnitTool{
		Schema: &XllmTool{
			Name:        "ls",
			Description: "列出指定目录的条目（只读）。path 为必填的目录路径，传入 \".\" 表示当前目录。",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"path": { "type": "string", "description": "目录路径，传入 \".\" 表示当前目录。" }
				},
				"required":["path"],
				"additionalProperties":false
			}`),
		},
		Param: &ToolLsParam{},
		Process: func(c *Client, a any) (any, error) {
			tp, ok := a.(*ToolLsParam)
			if !ok {
				return nil, fmt.Errorf("ls param type invalid，请检查代码之后重新运行")
			}
			return run_cmd("ls", tp.Path)
		},
	}
	BuiltIns["read"] = &UnitTool{
		Schema: &XllmTool{
			Name:        "read",
			Description: "读取文本文件内容（只读）。path 为必填的文件路径；offset 为字节偏移（默认 0），从该位置起最多返回 shellOutputSize 字节，超出则截断并在末尾提示续读位置。",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"path": { "type": "string", "description": "文件路径。" },
					"offset": { "type": "int", "description": "字节偏移，默认 0；用于分段续读大文件。" }
				},
				"required":["path"],
				"additionalProperties":false
			}`),
		},
		Param: &ToolReadParam{},
		Process: func(c *Client, a any) (any, error) {
			tp, ok := a.(*ToolReadParam)
			if !ok {
				return nil, fmt.Errorf("read param type invalid，请检查代码之后重新运行")
			}
			data, err := os.ReadFile(tp.Path)
			if err != nil {
				return nil, fmt.Errorf("读取文件： %s 错误：%w", tp.Path, err)
			}
			if tp.Offset < 0 {
				return nil, fmt.Errorf("read offset 必须 >= 0，当前 %d", tp.Offset)
			}
			if tp.Offset >= len(data) {
				return "", nil
			}
			posEnd := tp.Offset + shellOutputSize
			suffix := ""
			if posEnd > len(data) {
				posEnd = len(data)
			} else {
				suffix = fmt.Sprintf("\n...(已截断，续读可用 offset=%d", posEnd)
			}
			return string(data[tp.Offset:posEnd]) + suffix, nil
		},
	}
	BuiltIns["find"] = &UnitTool{
		Schema: &XllmTool{
			Name:        "find",
			Description: "按文件名模式递归查找文件（只读，不含内容）。path 为搜索起点（默认当前目录），pattern 为 glob 模式（如 \"*.go\"、\"test_*.py\"）；命中过多时输出会被截断。",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"path": { "type": "string", "description": "起始目录；省略或空字符串表示当前目录。" },
					"pattern": { "type": "string", "description": "glob 模式，例如 \"*.go\"、\"test_*.py\"、\"config.*\"。" }
				},
				"required":["pattern"],
				"additionalProperties":false
			}`),
		},
		Param: &ToolFindParam{},
		Process: func(c *Client, a any) (any, error) {
			tp, ok := a.(*ToolFindParam)
			if !ok {
				return nil, fmt.Errorf("find param type invalid，请检查代码之后重新运行")
			}
			if tp.Path == "" {
				tp.Path = "."
			}
			return run_cmd("find", tp.Path, "-name", tp.Pattern)
		},
	}
	BuiltIns["grep"] = &UnitTool{
		Schema: &XllmTool{
			Name:        "grep",
			Description: "在文件内容中按正则模式搜索匹配行（只读）。path 为搜索起点（默认当前目录），pattern 为正则表达式；递归扫描并显示文件名与行号，命中过多时输出会被截断。退出码 1 表示无匹配，属正常业务结果而非错误。",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"path": { "type": "string", "description": "起始目录或文件路径；省略或空字符串表示当前目录。" },
					"pattern": { "type": "string", "description": "正则表达式，例如 \"func .*Test\"、\"TODO\\\\b\"、\"^package \"。" }
				},
				"required":["pattern"],
				"additionalProperties":false
			}`),
		},
		Param: &ToolGrepParam{},
		Process: func(c *Client, a any) (any, error) {
			tp, ok := a.(*ToolGrepParam)
			if !ok {
				return nil, fmt.Errorf("grep param type invalid，请检查代码之后重新运行")
			}
			if tp.Path == "" {
				tp.Path = "."
			}
			return run_cmd("grep", "-rn", tp.Pattern, tp.Path)
		},
	}
}
