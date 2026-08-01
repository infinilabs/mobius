package tools

import (
	"context"
	"fmt"
	"mobius/internal/config"
	"mobius/internal/domain"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegexVerifyOutput(t *testing.T) {
	cases := []struct {
		name       string
		stdout     string
		stderr     string
		wantOk     bool
		wantConf   float64
		wantMsg    string
		wantErrStr string
	}{
		{
			name: "successful verification",
			stdout: `[分析中] 正在执行频域扫描...
[验证结果] 匹配成功！(置信度: 98.7%)
[提取内容] "Mobius"`,
			wantOk:   true,
			wantConf: 98.7,
			wantMsg:  "Mobius",
		},
		{
			name: "failed verification",
			stdout: `[分析中] 正在执行频域扫描...
[验证结果] 失败。未检测到有效水印，或密码错误。`,
			wantOk: false,
		},
		{
			name:       "unparseable output",
			stdout:     `random junk`,
			wantOk:     false,
			wantErrStr: "unparseable verify output",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.wantOk {
				if !verifySuccessRegex.MatchString(c.stdout) {
					t.Errorf("expected success regex to match stdout")
				}
				matches := verifySuccessRegex.FindStringSubmatch(c.stdout)
				if len(matches) < 2 || matches[1] != fmt.Sprintf("%.1f", c.wantConf) {
					t.Errorf("expected confidence %f, got matches: %v", c.wantConf, matches)
				}
				msgMatches := verifyMessageRegex.FindStringSubmatch(c.stdout)
				if len(msgMatches) < 2 || msgMatches[1] != c.wantMsg {
					t.Errorf("expected message %q, got matches: %v", c.wantMsg, msgMatches)
				}
			} else if c.wantErrStr == "" {
				if !verifyFailureRegex.MatchString(c.stdout) {
					t.Errorf("expected failure regex to match stdout")
				}
			} else {
				if verifySuccessRegex.MatchString(c.stdout) || verifyFailureRegex.MatchString(c.stdout) {
					t.Errorf("expected neither regex to match for error case")
				}
			}
		})
	}
}

func TestRegexCapacityError(t *testing.T) {
	stderr := `[错误] message exceeds capacity: 40 bytes, max 11 bytes (mode: global-dwt)`
	if !capacityErrorRegex.MatchString(stderr) {
		t.Error("expected capacityErrorRegex to match stderr")
	}
}

// 3. Mock DB and domain.Task Driver state transitions
type mockDBClient struct {
	transitions []string
	comments    []string
	updated     bool
	result      string
}

func (m *mockDBClient) UpdateTaskStatus(ctx context.Context, id, newStatus, actorID string, feedback ...string) error {
	m.transitions = append(m.transitions, newStatus)
	return nil
}

func (m *mockDBClient) AddTaskComment(ctx context.Context, taskID, authorID, content string) (*domain.TaskComment, error) {
	m.comments = append(m.comments, content)
	return &domain.TaskComment{}, nil
}

func (m *mockDBClient) UpdateTask(ctx context.Context, id string, title, body, priority *string, assigneeID *string, result *string) error {
	m.updated = true
	if result != nil {
		m.result = *result
	}
	return nil
}

func TestTaskDriver_Transitions(t *testing.T) {
	db := &mockDBClient{}
	driver := NewWatermarkTaskDriver(db, nil, nil, "actor-123", "secret-password")

	t.Run("transitionTask to in_progress", func(t *testing.T) {
		db.transitions = nil
		err := driver.transitionTask(context.Background(), "task-1", "in_progress")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(db.transitions) != 2 || db.transitions[0] != "ready" || db.transitions[1] != "in_progress" {
			t.Errorf("expected transitions [ready, in_progress], got %v", db.transitions)
		}
	})

	t.Run("completeTask", func(t *testing.T) {
		db.transitions = nil
		db.comments = nil
		db.updated = false
		db.result = ""

		err := driver.completeTask(context.Background(), "task-1", "gs://bucket/out.png")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !db.updated || db.result != "gs://bucket/out.png" {
			t.Errorf("expected task to be updated with result, got updated=%t, result=%q", db.updated, db.result)
		}
		if len(db.transitions) != 1 || db.transitions[0] != "needs_review" {
			t.Errorf("expected transition to needs_review, got %v", db.transitions)
		}
		if len(db.comments) != 1 || !strings.Contains(db.comments[0], "complete") {
			t.Errorf("expected completion comment, got %v", db.comments)
		}
	})
}

func TestValidateLocalInput(t *testing.T) {
	allowed := UploadsDir
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid file inside uploads", filepath.Join(UploadsDir, "image.png"), false},
		{"valid sub-folder file", filepath.Join(UploadsDir, "folder/image.png"), false},
		{"reject parent directory escape", filepath.Join(UploadsDir, "../secret.txt"), true},
		{"reject absolute path outside", "/etc/passwd", true},
		{"reject folder prefix match bypass", UploadsDir + "-other/image.png", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := validateLocalInput(c.input, allowed)
			if c.wantErr && err == nil {
				t.Errorf("expected error for input %q, got nil", c.input)
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error for input %q: %v", c.input, err)
			}
		})
	}
}

func TestRedactSecret(t *testing.T) {
	db := &mockDBClient{}
	driver := NewWatermarkTaskDriver(db, nil, nil, "actor-123", "secret123")

	input := "Failed running command: infinishield embed -p secret123 -m hello"
	expected := "Failed running command: infinishield embed -p ****** -m hello"

	redacted := driver.redactSecret(input)
	if redacted != expected {
		t.Errorf("expected %q, got %q", expected, redacted)
	}

	// Empty password should not change string
	driverNoPass := NewWatermarkTaskDriver(db, nil, nil, "actor-123", "")
	if driverNoPass.redactSecret(input) != input {
		t.Errorf("expected no redaction when password is empty")
	}
}

func TestExecTools_Confinement(t *testing.T) {
	cfg := &config.Config{}
	cfg.GoogleCloud.GCS.Bucket = "test-bucket"

	t.Run("verify tool rejects path outside upload dir", func(t *testing.T) {
		args := map[string]any{
			"input_path": "/etc/passwd",
			"password":   "pass123",
		}
		res := ExecVerifyWatermarkTool(context.Background(), nil, cfg, "agent-1", args)
		errVal, ok := res["error"].(string)
		if !ok || !strings.Contains(errVal, "outside the allowed upload directory") {
			t.Errorf("expected confinement error, got: %v", res)
		}
	})

	t.Run("embed tool rejects path outside upload dir", func(t *testing.T) {
		args := map[string]any{
			"input_path":  "/etc/passwd",
			"output_path": "/tmp/out.png",
			"message":     "hello",
			"password":    "pass123",
		}
		res := ExecWatermarkAssetsTool(context.Background(), nil, nil, cfg, nil, "agent-1", args)
		errVal, ok := res["error"].(string)
		if !ok || !strings.Contains(errVal, "outside the allowed upload directory") {
			t.Errorf("expected confinement error, got: %v", res)
		}
	})
}
