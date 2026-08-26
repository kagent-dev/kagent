package taskstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const defaultPageSize = 50

type record struct {
	ID              string `gorm:"primaryKey"`
	Task            []byte
	Version         int64
	User            string `gorm:"index"`
	ContextID       string `gorm:"index"`
	State           string `gorm:"index"`
	StatusTimestamp *time.Time
	UpdatedAt       time.Time `gorm:"index"`
}

type Local struct {
	db            *gorm.DB
	authenticator a2ataskstore.Authenticator
}

var _ a2ataskstore.Store = (*Local)(nil)

func New(sessionDBURL string, authenticator a2ataskstore.Authenticator) (*Local, error) {
	path, err := pathFromSessionDBURL(sessionDBURL)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open local task DB %q: %w", path, err)
	}
	if err := db.AutoMigrate(&record{}); err != nil {
		return nil, fmt.Errorf("migrate local task DB %q: %w", path, err)
	}
	return &Local{db: db, authenticator: authenticator}, nil
}

func (s *Local) Create(ctx context.Context, task *a2atype.Task) (a2ataskstore.TaskVersion, error) {
	if task == nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("task cannot be nil")
	}
	user, err := s.user(ctx)
	if err != nil {
		return a2ataskstore.TaskVersionMissing, err
	}
	data, err := encode(task)
	if err != nil {
		return a2ataskstore.TaskVersionMissing, err
	}
	record := recordFromTask(task, data, user, 1)
	if err := s.db.WithContext(ctx).Create(&record).Error; errors.Is(err, gorm.ErrDuplicatedKey) {
		return a2ataskstore.TaskVersionMissing, a2ataskstore.ErrTaskAlreadyExists
	} else if err != nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("create local task: %w", err)
	}
	return 1, nil
}

func (s *Local) Update(ctx context.Context, req *a2ataskstore.UpdateRequest) (a2ataskstore.TaskVersion, error) {
	if req == nil || req.Task == nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("update task cannot be nil")
	}
	user, err := s.user(ctx)
	if err != nil {
		return a2ataskstore.TaskVersionMissing, err
	}
	data, err := encode(req.Task)
	if err != nil {
		return a2ataskstore.TaskVersionMissing, err
	}
	query := s.db.WithContext(ctx).Model(&record{}).Where("id = ? AND user = ?", req.Task.ID, user)
	if req.PrevVersion != a2ataskstore.TaskVersionMissing {
		query = query.Where("version = ?", req.PrevVersion)
	}
	result := query.Updates(map[string]any{
		"task": data, "version": gorm.Expr("version + 1"), "context_id": req.Task.ContextID,
		"state": req.Task.Status.State, "status_timestamp": req.Task.Status.Timestamp, "updated_at": time.Now(),
	})
	if result.Error != nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("update local task: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&record{}).Where("id = ? AND user = ?", req.Task.ID, user).Count(&count).Error; err != nil {
			return a2ataskstore.TaskVersionMissing, fmt.Errorf("check local task: %w", err)
		}
		if count == 0 {
			return a2ataskstore.TaskVersionMissing, a2atype.ErrTaskNotFound
		}
		return a2ataskstore.TaskVersionMissing, a2ataskstore.ErrConcurrentModification
	}
	var stored record
	if err := s.db.WithContext(ctx).Select("version").First(&stored, "id = ? AND user = ?", req.Task.ID, user).Error; err != nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("load updated local task: %w", err)
	}
	return a2ataskstore.TaskVersion(stored.Version), nil
}

func (s *Local) Get(ctx context.Context, taskID a2atype.TaskID) (*a2ataskstore.StoredTask, error) {
	user, err := s.user(ctx)
	if err != nil {
		return nil, err
	}
	var stored record
	if err := s.db.WithContext(ctx).First(&stored, "id = ? AND user = ?", taskID, user).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, a2atype.ErrTaskNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load local task: %w", err)
	}
	task, err := decode(stored.Task)
	if err != nil {
		return nil, err
	}
	return &a2ataskstore.StoredTask{Task: task, Version: a2ataskstore.TaskVersion(stored.Version), User: stored.User}, nil
}

func (s *Local) List(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	user, err := s.user(ctx)
	if err != nil || user == "" {
		return nil, a2atype.ErrUnauthenticated
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = defaultPageSize
	} else if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100 inclusive, got %d: %w", pageSize, a2atype.ErrInvalidRequest)
	}
	query := s.db.WithContext(ctx).Model(&record{}).Where("user = ?", user)
	if req.ContextID != "" {
		query = query.Where("context_id = ?", req.ContextID)
	}
	if req.Status != a2atype.TaskStateUnspecified {
		query = query.Where("state = ?", req.Status)
	}
	if req.StatusTimestampAfter != nil {
		query = query.Where("status_timestamp IS NULL OR status_timestamp >= ?", req.StatusTimestampAfter)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count local tasks: %w", err)
	}
	if req.PageToken != "" {
		updatedAt, id, err := decodePageToken(req.PageToken)
		if err != nil {
			return nil, err
		}
		query = query.Where("updated_at < ? OR (updated_at = ? AND id < ?)", updatedAt, updatedAt, id)
	}
	var records []record
	if err := query.Order("updated_at DESC, id DESC").Limit(pageSize + 1).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list local tasks: %w", err)
	}
	nextPageToken := ""
	if len(records) > pageSize {
		last := records[pageSize-1]
		nextPageToken = encodePageToken(last.UpdatedAt, a2atype.TaskID(last.ID))
		records = records[:pageSize]
	}
	tasks := make([]*a2atype.Task, 0, len(records))
	for _, record := range records {
		task, err := decode(record.Task)
		if err != nil {
			return nil, err
		}
		shape(task, req)
		tasks = append(tasks, task)
	}
	return &a2atype.ListTasksResponse{Tasks: tasks, TotalSize: int(total), PageSize: pageSize, NextPageToken: nextPageToken}, nil
}

func (s *Local) user(ctx context.Context) (string, error) {
	user, err := s.authenticator(ctx)
	if err != nil {
		return "", fmt.Errorf("taskstore auth failed: %w", err)
	}
	return user, nil
}

func pathFromSessionDBURL(dbURL string) (string, error) {
	scheme, rest, ok := strings.Cut(dbURL, ":")
	if !ok || (scheme != "sqlite" && !strings.HasPrefix(scheme, "sqlite+")) {
		return "", fmt.Errorf("unsupported session DB URL %q: expected sqlite[+driver]:////<path>", dbURL)
	}
	path := "/" + strings.TrimLeft(rest, "/")
	if path == "/" {
		return "", fmt.Errorf("session DB URL %q has no path", dbURL)
	}
	return filepath.Join(filepath.Dir(path), "tasks.db"), nil
}

func recordFromTask(task *a2atype.Task, data []byte, user string, version int64) record {
	return record{ID: string(task.ID), Task: data, Version: version, User: user, ContextID: task.ContextID, State: string(task.Status.State), StatusTimestamp: task.Status.Timestamp}
}

func encode(task *a2atype.Task) ([]byte, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("encode task: %w", err)
	}
	return data, nil
}

func decode(data []byte) (*a2atype.Task, error) {
	var task a2atype.Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("decode task: %w", err)
	}
	return &task, nil
}

func shape(task *a2atype.Task, req *a2atype.ListTasksRequest) {
	historyLength := 100
	if req.HistoryLength != nil {
		historyLength = *req.HistoryLength
	}
	if historyLength <= 0 {
		task.History = []*a2atype.Message{}
	} else if len(task.History) > historyLength {
		task.History = task.History[len(task.History)-historyLength:]
	}
	if !req.IncludeArtifacts {
		task.Artifacts = nil
	}
}

func encodePageToken(updatedAt time.Time, id a2atype.TaskID) string {
	return base64.URLEncoding.EncodeToString(fmt.Appendf(nil, "%s_%s", updatedAt.Format(time.RFC3339Nano), id))
}

func decodePageToken(token string) (time.Time, a2atype.TaskID, error) {
	decoded, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, "", a2atype.ErrParseError
	}
	timestamp, id, ok := strings.Cut(string(decoded), "_")
	if !ok {
		return time.Time{}, "", a2atype.ErrParseError
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return time.Time{}, "", a2atype.ErrParseError
	}
	return updatedAt, a2atype.TaskID(id), nil
}
