package outp

import (
	"fmt"
	"io"
	"os"
)

// 问答输出展示

type Color string

const (
	ColorCyan    Color = "\x1b[36m"
	ColorGreen   Color = "\x1b[32m"
	ColorYellow  Color = "\x1b[33m"
	ColorRed     Color = "\x1b[31m"
	ColorGrey    Color = "\x1b[90m"
	ColorBlue    Color = "\x1b[34m"
	ColorMagenta Color = "\x1b[35m"
	ColorWhite   Color = "\x1b[37m"
	ColorBlack   Color = "\x1b[30m"
)
const (
	colorReset = "\x1b[0m"
)

type Terminal struct {
	color Color
	outer io.Writer
	nnl   bool
}

func NewStdout(c Color) *Terminal {
	t := &Terminal{
		color: c,
		outer: os.Stdout,
		nnl:   true,
	}
	t.outer.Write([]byte(c))
	return t
}

func (t *Terminal) Write(data []byte) (int, error) {
	return t.outer.Write(data)
}

func (t *Terminal) Close() error {
	if t.nnl {
		t.outer.Write([]byte("\n"))
	}
	t.outer.Write([]byte(colorReset))
	return nil
}

func (t *Terminal) ResetColor(c Color) {
	t.outer.Write([]byte(c))
}

func Print(v any, c Color) {
	t := NewStdout(c)
	t.Write([]byte(fmt.Append(nil, v)))
	t.Close()
}
func Prompt(c Color) {
	t := NewStdout(c)
	t.nnl = false
	t.Write([]byte("> "))
	t.Close()
}
