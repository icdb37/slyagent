package outp

import "testing"

func TestOutput(t *testing.T) {
	p := NewStdout(ColorGreen)
	p.Write([]byte("你好！"))
	p.Close()
}
