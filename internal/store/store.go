package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store 封装 SQLite 的所有数据访问。
type Store struct {
	db *sql.DB
}

// New 打开（或创建）SQLite 数据库并执行迁移与默认数据播种。
func New(path string) (*Store, error) {
	// _txlock=immediate 降低并发写冲突；_busy_timeout 避免锁等待失败。
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 写并发受限，单连接配合 WAL 足够
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := s.seed(); err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}
	return s, nil
}

// Close 关闭底层数据库连接。
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS templates (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			system_vars TEXT NOT NULL DEFAULT '{}',
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS versions (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			template_id INTEGER NOT NULL,
			label       TEXT NOT NULL,
			content     TEXT NOT NULL,
			model       TEXT NOT NULL DEFAULT 'gpt-4o-mini',
			weight      INTEGER NOT NULL DEFAULT 100,
			active      INTEGER NOT NULL DEFAULT 1,
			created_at  TEXT NOT NULL,
			FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			trace_id      TEXT NOT NULL DEFAULT '',
			user_id       TEXT NOT NULL DEFAULT '',
			template_name TEXT NOT NULL DEFAULT '',
			version_label TEXT NOT NULL DEFAULT '',
			model         TEXT NOT NULL DEFAULT '',
			input_tokens  INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			status        TEXT NOT NULL DEFAULT 'ok',
			error         TEXT NOT NULL DEFAULT '',
			created_at    TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_versions_template ON versions(template_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return fmt.Errorf("exec %q: %w", st, err)
		}
	}
	return nil
}

// seed 在首次启动时写入示例模板，保证开箱即用体验。
func (s *Store) seed() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM templates`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	now := time.Now().Format(time.RFC3339)
	// 示例模板：代码审查助手，含两个灰度版本（10% 实验版本）
	res, err := s.db.Exec(
		`INSERT INTO templates(name, description, system_vars, created_at, updated_at) VALUES(?,?,?,?,?)`,
		"code-review", "代码审查助手：根据传入代码给出改进建议", `{"lang":"通用","focus":"可读性"}`, now, now,
	)
	if err != nil {
		return err
	}
	tid, _ := res.LastInsertId()
	versions := []Version{
		{TemplateID: tid, Label: "v1-stable", Content: "你是一位资深 {{.lang}} 工程师，请审查以下代码并指出：\n1. 潜在 Bug\n2. 性能问题\n3. 可读性改进\n\n代码：\n{{.code}}", Model: "gpt-4o-mini", Weight: 90, Active: true},
		{TemplateID: tid, Label: "v2-experiment", Content: "作为 {{.lang}} 技术专家，请用结构化清单（Bug/性能/可读性）审查代码，并给出修复示例 diff：\n{{.code}}", Model: "gpt-4o-mini", Weight: 10, Active: true},
	}
	for _, v := range versions {
		if _, err := s.db.Exec(
			`INSERT INTO versions(template_id, label, content, model, weight, active, created_at) VALUES(?,?,?,?,?,?,?)`,
			v.TemplateID, v.Label, v.Content, v.Model, v.Weight, v.Active, now,
		); err != nil {
			return err
		}
	}
	return nil
}

// ---------------- 模板 CRUD ----------------

// ListTemplates 返回所有模板（含版本明细）。
func (s *Store) ListTemplates() ([]PromptTemplate, error) {
	rows, err := s.db.Query(`SELECT id, name, description, system_vars, created_at, updated_at FROM templates ORDER BY id`)
	if err != nil {
		return nil, err
	}
	// 先收集所有模板行并关闭 rows，避免在单连接下嵌套查询造成死锁。
	var out []PromptTemplate
	for rows.Next() {
		var t PromptTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.SystemVars, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 行已关闭，安全地逐个加载版本
	for i := range out {
		out[i].Versions, err = s.listVersions(out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GetTemplate 按 ID 查询模板（含版本）。
func (s *Store) GetTemplate(id int64) (*PromptTemplate, error) {
	var t PromptTemplate
	err := s.db.QueryRow(`SELECT id, name, description, system_vars, created_at, updated_at FROM templates WHERE id=?`, id).
		Scan(&t.ID, &t.Name, &t.Description, &t.SystemVars, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Versions, err = s.listVersions(t.ID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTemplateByName 按名称查询模板（含版本），供缓存预热与路由使用。
func (s *Store) GetTemplateByName(name string) (*PromptTemplate, error) {
	var t PromptTemplate
	err := s.db.QueryRow(`SELECT id, name, description, system_vars, created_at, updated_at FROM templates WHERE name=?`, name).
		Scan(&t.ID, &t.Name, &t.Description, &t.SystemVars, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Versions, err = s.listVersions(t.ID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTemplate 创建模板及其初始版本。
func (s *Store) CreateTemplate(t *PromptTemplate) error {
	now := time.Now().Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO templates(name, description, system_vars, created_at, updated_at) VALUES(?,?,?,?,?)`,
		t.Name, t.Description, defaultIfEmpty(t.SystemVars, "{}"), now, now,
	)
	if err != nil {
		return err
	}
	tid, _ := res.LastInsertId()
	for _, v := range t.Versions {
		if _, err := tx.Exec(
			`INSERT INTO versions(template_id, label, content, model, weight, active, created_at) VALUES(?,?,?,?,?,?,?)`,
			tid, v.Label, v.Content, defaultIfEmpty(v.Model, "gpt-4o-mini"), orDefault(v.Weight, 100), v.Active, now,
		); err != nil {
			return err
		}
	}
	t.ID = tid
	t.CreatedAt = now
	t.UpdatedAt = now
	return tx.Commit()
}

// UpdateTemplate 更新模板基本信息。
func (s *Store) UpdateTemplate(t *PromptTemplate) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE templates SET name=?, description=?, system_vars=?, updated_at=? WHERE id=?`,
		t.Name, t.Description, defaultIfEmpty(t.SystemVars, "{}"), now, t.ID,
	)
	t.UpdatedAt = now
	return err
}

// DeleteTemplate 删除模板（级联删除版本）。
func (s *Store) DeleteTemplate(id int64) error {
	_, err := s.db.Exec(`DELETE FROM templates WHERE id=?`, id)
	return err
}

// ---------------- 版本 CRUD ----------------

func (s *Store) listVersions(templateID int64) ([]Version, error) {
	rows, err := s.db.Query(
		`SELECT id, template_id, label, content, model, weight, active, created_at FROM versions WHERE template_id=? ORDER BY id`,
		templateID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var v Version
		var active int
		if err := rows.Scan(&v.ID, &v.TemplateID, &v.Label, &v.Content, &v.Model, &v.Weight, &active, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Active = active == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

// AddVersion 为模板新增一个版本。
func (s *Store) AddVersion(v *Version) error {
	now := time.Now().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO versions(template_id, label, content, model, weight, active, created_at) VALUES(?,?,?,?,?,?,?)`,
		v.TemplateID, v.Label, v.Content, defaultIfEmpty(v.Model, "gpt-4o-mini"), orDefault(v.Weight, 100), v.Active, now,
	)
	if err != nil {
		return err
	}
	v.ID, _ = res.LastInsertId()
	v.CreatedAt = now
	// 更新模板 updated_at
	_, _ = s.db.Exec(`UPDATE templates SET updated_at=? WHERE id=?`, now, v.TemplateID)
	return nil
}

// UpdateVersion 更新版本内容/权重/启用状态。
func (s *Store) UpdateVersion(v *Version) error {
	_, err := s.db.Exec(
		`UPDATE versions SET label=?, content=?, model=?, weight=?, active=? WHERE id=?`,
		v.Label, v.Content, v.Model, v.Weight, v.Active, v.ID,
	)
	if err == nil {
		now := time.Now().Format(time.RFC3339)
		_, _ = s.db.Exec(`UPDATE templates SET updated_at=? WHERE id=?`, now, v.TemplateID)
	}
	return err
}

// DeleteVersion 删除版本。
func (s *Store) DeleteVersion(id int64) error {
	_, err := s.db.Exec(`DELETE FROM versions WHERE id=?`, id)
	return err
}

// ---------------- 审计日志 ----------------

// InsertAuditLog 写入一条审计记录。
func (s *Store) InsertAuditLog(l *AuditLog) error {
	now := time.Now().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO audit_logs(trace_id, user_id, template_name, version_label, model, input_tokens, output_tokens, status, error, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		l.TraceID, l.UserID, l.TemplateName, l.VersionLabel, l.Model, l.InputTokens, l.OutputTokens, l.Status, l.Error, now,
	)
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	l.CreatedAt = now
	return nil
}

// ListAuditLogs 返回最近 limit 条审计记录。
func (s *Store) ListAuditLogs(limit int) ([]AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, trace_id, user_id, template_name, version_label, model, input_tokens, output_tokens, status, error, created_at
		 FROM audit_logs ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.TraceID, &l.UserID, &l.TemplateName, &l.VersionLabel, &l.Model, &l.InputTokens, &l.OutputTokens, &l.Status, &l.Error, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------------- helpers ----------------

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
