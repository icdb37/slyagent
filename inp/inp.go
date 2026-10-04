package inp

import (
	"bufio"
	"os"
	"strings"
)

var reader = bufio.NewReader(os.Stdin)

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
