package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StoreIssue struct {
	Index   int    `json:"index"`
	Token   string `json:"token"`
	Message string `json:"message"`
}
type StoreBackup struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	ValidRecords int       `json:"valid_records"`
	Issues       int       `json:"issues"`
	Readable     bool      `json:"readable"`
}
type StoreReport struct {
	Enabled      bool          `json:"enabled"`
	WriteBlocked bool          `json:"write_blocked"`
	Issues       []StoreIssue  `json:"issues"`
	Archive      string        `json:"archive"`
	Backups      []StoreBackup `json:"backups"`
}
type RestorePreview struct {
	BackupID string        `json:"backup_id"`
	Digest   string        `json:"digest"`
	Add      []SessionInfo `json:"add"`
	Existing int           `json:"existing"`
	Issues   []StoreIssue  `json:"issues"`
}

func validateStoredInfo(info *SessionInfo) error {
	if !tokenPattern.MatchString(info.Token) {
		return errors.New("项目标识不合法")
	}
	if err := ValidateTarget(info.TargetURL); err != nil {
		return err
	}
	if !validSessionScreen(info.Resolution, info.FPS) {
		return errors.New("分辨率或帧率不合法")
	}
	if info.RenderMode != "cpu" && info.RenderMode != "nvidia" {
		return errors.New("渲染模式不合法")
	}
	if info.ConcurrencyMode != "single" && info.ConcurrencyMode != "multi" {
		return errors.New("观看模式不合法")
	}
	if info.MaxViewers < 1 || info.MaxViewers > 100 {
		return errors.New("观看人数上限不合法")
	}
	if info.ConcurrencyMode == "single" && info.MaxViewers != 1 || info.ConcurrencyMode == "multi" && info.MaxViewers < 2 {
		return errors.New("观看人数上限与观看模式不一致")
	}
	if info.PreloadCount == 0 {
		info.PreloadCount = 1
	}
	if info.PreloadCount < 1 || info.PreloadCount > info.MaxViewers {
		return errors.New("预加载路数不合法")
	}
	if info.FramePolicy == "" {
		info.FramePolicy = "interactive"
	}
	if (info.FramePolicy != "interactive" && info.FramePolicy != "animation") || info.ReservedPreloads < 0 || info.ReservedPreloads > info.PreloadCount {
		return errors.New("性能策略不合法")
	}
	return validateReadySelector(info.ReadySelector)
}

func decodeStoredRecords(data []byte) ([]SessionInfo, []StoreIssue, error) {
	var raw []json.RawMessage
	if strings.TrimSpace(string(data)) == "null" {
		return nil, nil, errors.New("项目文件应为 JSON 数组")
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, err
	}
	infos := []SessionInfo{}
	issues := []StoreIssue{}
	seen := map[string]bool{}
	for i, record := range raw {
		var info SessionInfo
		err := json.Unmarshal(record, &info)
		if err == nil {
			// Older project files have no display setting; keep FPS visible.
			var presentation struct {
				ShowFPS *bool `json:"show_fps"`
			}
			err = json.Unmarshal(record, &presentation)
			if presentation.ShowFPS == nil {
				info.ShowFPS = true
			}
		}
		if err == nil {
			err = validateStoredInfo(&info)
		}
		if err == nil && seen[info.Token] {
			err = errors.New("项目标识重复，已保留第一条合法记录")
		}
		if err != nil {
			token := info.Token
			if len(token) > 80 {
				token = token[:80]
			}
			issues = append(issues, StoreIssue{i + 1, token, err.Error()})
			continue
		}
		seen[info.Token] = true
		infos = append(infos, info)
	}
	return infos, issues, nil
}

// Preserve damaged bytes before a later save can replace the source. A bad
// record cannot prevent unrelated valid links from loading.
func (m *SessionManager) EnableStore(path string) error {
	if path == "" {
		return errors.New("session store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	infos := []SessionInfo{}
	issues := []StoreIssue{}
	var decodeErr error
	if !missing {
		infos, issues, decodeErr = decodeStoredRecords(data)
	}
	if decodeErr != nil {
		issues = append(issues, StoreIssue{Message: "项目文件无法解析：" + decodeErr.Error()})
	}
	report := StoreReport{Enabled: true, WriteBlocked: decodeErr != nil, Issues: issues}
	if len(issues) > 0 {
		id, err := writeStoreBackup(path, data, "damaged")
		if err != nil {
			return fmt.Errorf("preserve damaged session store: %w", err)
		}
		report.Archive = id
	}
	loaded := map[string]*session{}
	for _, info := range infos {
		s := &session{info: info, published: true}
		s.cacheStatusLocked()
		loaded[info.Token] = s
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) != 0 {
		return errors.New("session store must be enabled before creating links")
	}
	m.sessions = loaded
	m.storePath = path
	m.storeReport = report
	return nil
}

func atomicStoreWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gostream3d-store-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func writeStoreBackup(path string, data []byte, kind string) (string, error) {
	dir := path + ".backups"
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "sessions-"+kind+"-"+time.Now().UTC().Format("20060102T150405")+"-*.json")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Close()
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	// Retain 20 automatic revisions; preserve damaged and manual copies.
	if kind == "auto" {
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "sessions-auto-") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for len(names) > 20 {
			_ = os.Remove(filepath.Join(dir, names[0]))
			names = names[1:]
		}
	}
	return filepath.Base(name), nil
}

func (m *SessionManager) storedInfos() []SessionInfo {
	m.mu.RLock()
	items := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, s)
	}
	m.mu.RUnlock()
	infos := []SessionInfo{}
	for _, s := range items {
		s.mu.Lock()
		if s.published && !s.deleted {
			infos = append(infos, s.info)
		}
		s.mu.Unlock()
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Token < infos[j].Token })
	return infos
}
func (m *SessionManager) save() error {
	if m.storePath == "" {
		return nil
	}
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	return m.saveStoreLocked(false)
}
func (m *SessionManager) saveStoreLocked(recovery bool) error {
	if m.storeReport.WriteBlocked && !recovery {
		return errors.New("项目文件已损坏，写入已保护；请先在后台恢复备份")
	}
	data, err := json.MarshalIndent(m.storedInfos(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	old, err := os.ReadFile(m.storePath)
	if err == nil && string(old) != string(data) {
		if _, err = writeStoreBackup(m.storePath, old, "auto"); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Keep the latest healthy state too, including the first created project.
	if string(old) != string(data) {
		if _, err := writeStoreBackup(m.storePath, data, "auto"); err != nil {
			return err
		}
	}
	return atomicStoreWrite(m.storePath, data)
}

func (m *SessionManager) StoreStatus() StoreReport {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	report := m.storeReport
	report.Issues = append([]StoreIssue{}, report.Issues...)
	report.Backups = []StoreBackup{}
	if m.storePath == "" {
		return report
	}
	entries, _ := os.ReadDir(m.storePath + ".backups")
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "sessions-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.storePath+".backups", e.Name()))
		if err != nil {
			continue
		}
		infos, issues, decodeErr := decodeStoredRecords(data)
		stat, err := e.Info()
		if err != nil {
			continue
		}
		report.Backups = append(report.Backups, StoreBackup{e.Name(), stat.ModTime(), len(infos), len(issues), decodeErr == nil})
	}
	sort.Slice(report.Backups, func(i, j int) bool { return report.Backups[i].CreatedAt.After(report.Backups[j].CreatedAt) })
	return report
}
func (m *SessionManager) BackupStore() (string, error) {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	if m.storePath == "" || m.storeReport.WriteBlocked {
		return "", errors.New("当前项目文件尚未恢复或未启用存储")
	}
	data, err := json.MarshalIndent(m.storedInfos(), "", "  ")
	if err != nil {
		return "", err
	}
	return writeStoreBackup(m.storePath, append(data, '\n'), "manual")
}
func (m *SessionManager) previewStoreLocked(id string) (RestorePreview, error) {
	p := RestorePreview{BackupID: id, Add: []SessionInfo{}, Issues: []StoreIssue{}}
	if m.storePath == "" || id != filepath.Base(id) || !strings.HasPrefix(id, "sessions-") || !strings.HasSuffix(id, ".json") || strings.ContainsAny(id, "/\\:") {
		return p, errors.New("备份标识不合法")
	}
	data, err := os.ReadFile(filepath.Join(m.storePath+".backups", id))
	if err != nil {
		return p, err
	}
	infos, issues, err := decodeStoredRecords(data)
	if err != nil {
		return p, errors.New("此备份无法解析，请选择其他备份")
	}
	hash := sha256.Sum256(data)
	p.Digest = hex.EncodeToString(hash[:])
	p.Issues = issues
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, info := range infos {
		if _, ok := m.sessions[info.Token]; ok {
			p.Existing++
			continue
		}
		info.RenderPaused = true
		p.Add = append(p.Add, info)
	}
	return p, nil
}
func (m *SessionManager) PreviewRestore(id string) (RestorePreview, error) {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	return m.previewStoreLocked(id)
}
func (m *SessionManager) RestoreMissing(id, digest string) (RestorePreview, error) {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	p, err := m.previewStoreLocked(id)
	if err != nil {
		return p, err
	}
	if digest == "" || p.Digest != digest {
		return p, errors.New("备份已变化，请重新预览")
	}
	if len(p.Add) == 0 {
		return p, errors.New("备份中没有可恢复的缺失项目")
	}
	m.mu.Lock()
	for _, info := range p.Add {
		s := &session{info: info, published: true}
		s.cacheStatusLocked()
		m.sessions[info.Token] = s
	}
	m.mu.Unlock()
	if err = m.saveStoreLocked(true); err != nil {
		m.mu.Lock()
		for _, info := range p.Add {
			delete(m.sessions, info.Token)
		}
		m.mu.Unlock()
		return p, err
	}
	m.storeReport.WriteBlocked = false
	return p, nil
}
