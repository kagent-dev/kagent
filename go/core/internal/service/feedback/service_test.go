package feedback_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kagent-dev/kagent/go/api/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/feedback"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type feedbackStore struct {
	database.Client
	stored     *database.Feedback
	listed     []database.Feedback
	listedUser string
	err        error
}

func (store *feedbackStore) StoreFeedback(_ context.Context, value *database.Feedback) error {
	copy := *value
	store.stored = &copy
	return store.err
}

func (store *feedbackStore) ListFeedback(_ context.Context, userID string) ([]database.Feedback, error) {
	store.listedUser = userID
	return store.listed, store.err
}

func TestServiceCreateAndList(t *testing.T) {
	store := &feedbackStore{listed: []database.Feedback{{ID: 7, UserID: "user-1", FeedbackText: "helpful"}}}
	service := feedback.NewService(store)
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user-1"}}})
	messageID := int64(42)
	issueType := database.FeedbackIssueTypeFactual

	err := service.Create(ctx, feedback.CreateRequest{
		MessageID:    &messageID,
		IsPositive:   false,
		FeedbackText: "incorrect answer",
		IssueType:    &issueType,
	})
	require.NoError(t, err)
	require.NotNil(t, store.stored)
	assert.Equal(t, "user-1", store.stored.UserID)
	assert.Equal(t, messageID, *store.stored.MessageID)
	assert.Equal(t, issueType, *store.stored.IssueType)

	result, err := service.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, store.listed, result)
	assert.Equal(t, "user-1", store.listedUser)
}

func TestServiceErrors(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user-1"}}})
	service := feedback.NewService(&feedbackStore{})

	err := service.Create(ctx, feedback.CreateRequest{})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)

	err = service.Create(t.Context(), feedback.CreateRequest{FeedbackText: "text"})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated), err)

	store := &feedbackStore{err: errors.New("database unavailable")}
	service = feedback.NewService(store)
	err = service.Create(ctx, feedback.CreateRequest{FeedbackText: "text"})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal), err)
	_, err = service.List(ctx)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal), err)
}
