package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"slyagent/repl"
)

// TestStartupLoadsExistingSession 验证启动时 cfg.SessID 对应的会话文件存在时，
// SessionLoad 事件能送达 Update 并把 history 填进 m.items。
//
// 这条回归覆盖：Client.New() 默认 id="default"、cfg.SessID 默认 "default"，
// LoadSession 不能因 "id == c.id" 短路跳过读盘（启动时 c.history 还是空，
// 必须走完整加载路径）。
func TestStartupLoadsExistingSession(t *testing.T) {
	tmp := t.TempDir()
	store := map[string]any{
		"id": "default",
		"history": []map[string]any{
			{"role": "user", "content": "鸡兔同笼"},
			{"role": "assistant", "content": []map[string]any{
				{"type": "thinking", "thinking": "列方程", "signature": "sig"},
				{"type": "text", "text": "鸡5兔5"},
			}},
		},
	}
	raw, _ := json.Marshal(store)
	if err := os.MkdirAll(filepath.Join(tmp, ".slyagent", "default"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".slyagent", "default", "data.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	cfg := &repl.Config{ModelName: "test", SessID: "default"}
	m := newModel(cfg)
	m.client.Start()
	defer m.client.Stop()

	go func() {
		m.cmd <- repl.Cmd{Kind: repl.CmdSession, Msg: "load default"}
	}()

	done := make(chan struct{})
	go func() {
		for ev := range m.client.Events() {
			if er, ok := ev.(*repl.EvtRepl); ok && er.Kind == repl.SessionLoad {
				updated, _ := m.Update(er)
				m = updated.(*model)
				close(done)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("超时：SessionLoad 未到达 Update")
	}

	if len(m.items) < 2 {
		t.Fatalf("期望至少 2 条 historyItem（系统提示 + user），得到 %d", len(m.items))
	}

	var sawUser, sawAssistant bool
	for _, it := range m.items {
		switch it.kind {
		case itemUser:
			if strings.Contains(it.text, "鸡兔同笼") {
				sawUser = true
			}
		case itemAssistant, itemThinking:
			sawAssistant = true
		}
	}
	if !sawUser {
		t.Fatalf("m.items 缺少 user 条目（应包含 '鸡兔同笼'）：%+v", m.items)
	}
	if !sawAssistant {
		t.Fatalf("m.items 缺少 assistant / thinking 条目：%+v", m.items)
	}
}

// TestSaveSessionEmptyDoesNotOverwrite 验证 c.history 为空时 SaveSession 不会写盘，
// 避免启动时把已存在的 data.json 覆盖成空文件。
func TestSaveSessionEmptyDoesNotOverwrite(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".slyagent", "default")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"id":"default","history":[{"role":"user","content":"important"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "data.json"), original, 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	cfg := &repl.Config{ModelName: "test", SessID: "default"}
	m := newModel(cfg)
	m.client.Start()
	defer m.client.Stop()

	// 触发 SaveSession（LoadSession 进来先 Save 当前空 history）
	if err := m.client.LoadSession("default"); err != nil {
		t.Fatalf("LoadSession 失败：%v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatalf("读 data.json 失败：%v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("SaveSession 空 history 覆盖了原文件！\n原：%s\n新：%s", original, got)
	}
}