package outp

import (
	"slyagent/enum"
	"testing"
)

func TestOutput(t *testing.T) {
	p := NewStdout(enum.RoleAssistant)
	p.Write([]byte("你好！"))
	p.Close()
}
