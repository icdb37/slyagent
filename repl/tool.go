package repl

import (
	"encoding/json"
	"time"
)

type UnitTool struct {
	Schema  *XllmTool
	Param   any
	Process func(any) (any, error)
}

var BuiltIns = map[string]*UnitTool{
	"get_current_datetime": {
		Schema: &XllmTool{
			Name:        "get_current_datetime",
			Description: "获取服务器当前时间。无需参数；返回值格式 yyyy-MM-dd HH:mm:ss，例如 2026-10-03 16:12:00",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		Param: &struct{}{},
		Process: func(a any) (any, error) {
			return time.Now().Format(time.DateTime), nil
		},
	},
}
