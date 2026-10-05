package repl

type Config struct {
	APIKey      string
	BaseURL     string
	ModelName   string
	MaxTokens   int
	Temperature float64
}

const (
	MessageHistorySize   = 1024 * 10
	MessageRecentNum     = 2
	MessageRecentMul1    = 4
	MessageRecentMu2     = 6
	MessageToolResultMax = 2000 // tool_result 字符串超长时按 rune 截断，避免反复塞进历史
	SaveThinking         = true // 保留大模型的思考过程
)
