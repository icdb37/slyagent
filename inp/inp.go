package inp

import (
	"bufio"
	"os"
	"strings"
)

var reader = bufio.NewReader(os.Stdin)

// AskYesNo 询问用户是否确认（y/n）。
// 进程可通过重新赋值替换默认实现（如 TUI 模式下用界面代替 stdin）。
var AskYesNo = defaultAskYesNo

func defaultAskYesNo(prompt string) bool {
	os.Stdout.WriteString(prompt)
	ans := ReadAnswer(3, true, true, " 重新输入: ", "y", "yes", "ok", "n", "no")
	return ans == "y" || ans == "yes" || ans == "ok"
}

func ReadLine() string {
	line, _, _ := reader.ReadLine()
	return string(line)
}

func ReadUntil(delim byte) string {
	data, _ := reader.ReadString(delim)
	return data
}

func ReadAnswer(retry int, iglu, trim bool, tip string, results ...string) string {
	ss := map[string]struct{}{}
	for _, r := range results {
		if iglu { // 忽略大小写
			r = strings.ToLower(r)
		}
		ss[r] = struct{}{}
	}
	var line string
	for i := 0; i < retry; i++ {
		line = ReadLine()
		if trim {
			line = strings.TrimSpace(line)
		}
		if iglu {
			line = strings.ToLower(line)
		}
		if _, ok := ss[line]; ok {
			return line
		}
		os.Stdout.WriteString(tip)
	}
	return line
}
