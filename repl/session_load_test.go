package repl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"slyagent/enum"
)

// TestRunSessionBareIDLoadsOrCreates 验证 "/session foo" 这种无 subcommand 的
// 形式被 runSession 当成 LoadSession("foo") 处理：
//   - foo 不存在 → SessionLoad{ID: "foo", Items: []} 事件
//   - foo 存在     → SessionLoad{ID: "foo", Items: [...]} 事件
//
// 这是"加载并取代老 SessionNew 命令"的回归保护：去掉了 "new" 之后，
// 用户的习惯用法 `/session foo` 必须直接落到 LoadSession。
func TestRunSessionBareIDLoadsOrCreates(t *testing.T) {
	t.Run("不存在则新建", func(t *testing.T) {
		cfg := &Config{ModelName: "test"}
		c, _ := New(cfg)
		c.Start()
		defer c.Stop()

		go func() {
			c.cmd <- Cmd{Kind: CmdSession, Msg: "fresh-id"}
		}()

		got := waitForEvent(t, c, SessionLoad, 2*time.Second)
		p, err := EncodeEvent[EvtPayloadSessionLoad](got)
		if err != nil {
			t.Fatalf("解码 payload 失败：%v", err)
		}
		if p.ID != "fresh-id" {
			t.Fatalf("期望 ID=fresh-id，实际 %q", p.ID)
		}
		if len(p.Items) != 0 {
			t.Fatalf("新建会话应无 items，得到 %d 条", len(p.Items))
		}
	})

	t.Run("存在则加载", func(t *testing.T) {
		tmp := t.TempDir()
		dir := filepath.Join(tmp, ".slyagent", "exists-id")
		store := map[string]any{
			"id":      "exists-id",
			"history": []map[string]any{{"role": "user", "content": "hi"}},
		}
		raw, _ := json.Marshal(store)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "data.json"), raw, 0644); err != nil {
			t.Fatal(err)
		}

		origDir, _ := os.Getwd()
		if err := os.Chdir(tmp); err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(origDir)

		cfg := &Config{ModelName: "test"}
		c, _ := New(cfg)
		c.Start()
		defer c.Stop()

		go func() {
			c.cmd <- Cmd{Kind: CmdSession, Msg: "exists-id"}
		}()

		got := waitForEvent(t, c, SessionLoad, 2*time.Second)
		p, err := EncodeEvent[EvtPayloadSessionLoad](got)
		if err != nil {
			t.Fatalf("解码 payload 失败：%v", err)
		}
		if p.ID != "exists-id" {
			t.Fatalf("期望 ID=exists-id，实际 %q", p.ID)
		}
		if len(p.Items) != 1 {
			t.Fatalf("期望 1 条历史，得到 %d", len(p.Items))
		}
		if p.Items[0].Role != enum.RoleUser {
			t.Fatalf("期望 role=user，得到 %q", p.Items[0].Role)
		}
	})
}

// waitForEvent 从 c.Events() 收一条指定 kind 的事件，超时即失败。
func waitForEvent(t *testing.T, c *Client, kind EvtKind, timeout time.Duration) *EvtRepl {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-c.Events():
			if er, ok := ev.(*EvtRepl); ok && er.Kind == kind {
				return er
			}
		case <-deadline:
			t.Fatalf("超时：未收到 kind=%s 事件", kind)
			return nil
		}
	}
}