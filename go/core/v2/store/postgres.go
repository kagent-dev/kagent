package store

import (
	"context"
	"encoding/json"
	"fmt"

	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/gen"
	"github.com/kagent-dev/kagent/go/core/v2/revision"
)

type Postgres struct {
	q *dbgen.Queries
}

func NewPostgres(db dbgen.DBTX) *Postgres {
	return &Postgres{q: dbgen.New(db)}
}

type AgentTemplateAttachment struct {
	Namespace         string
	AgentTemplateName string
	AgentTemplateUID  string
	HarnessName       string
	HarnessUID        string
	DesiredRevision   string
}

type RuntimeRevision struct {
	Revision           string
	Namespace          string
	AgentTemplateName  string
	AgentTemplateUID   string
	HarnessName        string
	HarnessUID         string
	SourceSnapshot     json.RawMessage
	EgressDestinations []string
	ActorTemplate      revision.ActorTemplateRef
}

type RuntimeRevisionRef struct {
	Revision      string
	ActorTemplate revision.ActorTemplateRef
}

type RuntimeRevisionStore interface {
	UpsertAgentTemplateAttachment(context.Context, AgentTemplateAttachment) error
	UpsertRuntimeRevision(context.Context, RuntimeRevision) error
	MarkRuntimeRevisionSuccessful(context.Context, AgentTemplateAttachment) error
	RetireAgentTemplateAttachments(context.Context, string, string) error
	RetireHarnessAttachment(context.Context, string, string, string) error
	RetireOtherHarnessAttachments(context.Context, string, string, []string) error
	ListUnreferencedRuntimeRevisions(context.Context) ([]RuntimeRevisionRef, error)
	DeleteUnreferencedRuntimeRevision(context.Context, string) error
}

func (c *Postgres) UpsertAgentTemplateAttachment(ctx context.Context, attachment AgentTemplateAttachment) error {
	return c.q.UpsertAgentTemplateAttachment(ctx, dbgen.UpsertAgentTemplateAttachmentParams{
		Namespace: attachment.Namespace, AgentTemplateName: attachment.AgentTemplateName,
		AgentTemplateUid: attachment.AgentTemplateUID, HarnessName: attachment.HarnessName,
		HarnessUid: attachment.HarnessUID, DesiredRevision: attachment.DesiredRevision,
	})
}

func (c *Postgres) UpsertRuntimeRevision(ctx context.Context, value RuntimeRevision) error {
	t := value.ActorTemplate
	if err := c.q.UpsertRuntimeRevision(ctx, dbgen.UpsertRuntimeRevisionParams{
		Revision: value.Revision, Namespace: value.Namespace,
		AgentTemplateName: value.AgentTemplateName, AgentTemplateUid: value.AgentTemplateUID,
		HarnessName: value.HarnessName, HarnessUid: value.HarnessUID,
		SourceSnapshot: value.SourceSnapshot, EgressDestinations: value.EgressDestinations,
		ActorTemplateNamespace: t.Namespace, ActorTemplateName: t.Name,
		ActorTemplateUid: t.UID, Phase: t.Phase, GoldenSnapshot: t.GoldenSnapshot,
	}); err != nil {
		return fmt.Errorf("upsert runtime revision %s: %w", value.Revision, err)
	}
	return nil
}

func (c *Postgres) MarkRuntimeRevisionSuccessful(ctx context.Context, attachment AgentTemplateAttachment) error {
	revision := attachment.DesiredRevision
	return c.q.MarkRuntimeRevisionSuccessful(ctx, dbgen.MarkRuntimeRevisionSuccessfulParams{
		Revision: &revision, Namespace: attachment.Namespace,
		AgentTemplateUid: attachment.AgentTemplateUID, HarnessUid: attachment.HarnessUID,
	})
}

func (c *Postgres) RetireAgentTemplateAttachments(ctx context.Context, namespace, name string) error {
	return c.q.RetireAgentTemplateAttachments(ctx, dbgen.RetireAgentTemplateAttachmentsParams{Namespace: namespace, AgentTemplateName: name})
}

func (c *Postgres) RetireHarnessAttachment(ctx context.Context, namespace, template, harness string) error {
	return c.q.RetireHarnessAttachment(ctx, dbgen.RetireHarnessAttachmentParams{Namespace: namespace, AgentTemplateName: template, HarnessName: harness})
}

func (c *Postgres) RetireOtherHarnessAttachments(ctx context.Context, namespace, templateUID string, harnesses []string) error {
	return c.q.RetireOtherHarnessAttachments(ctx, dbgen.RetireOtherHarnessAttachmentsParams{
		Namespace: namespace, AgentTemplateUid: templateUID, HarnessNames: harnesses,
	})
}

func (c *Postgres) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]RuntimeRevisionRef, error) {
	rows, err := c.q.ListUnreferencedRuntimeRevisions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unreferenced runtime revisions: %w", err)
	}
	result := make([]RuntimeRevisionRef, 0, len(rows))
	for _, row := range rows {
		result = append(result, RuntimeRevisionRef{Revision: row.Revision, ActorTemplate: revision.ActorTemplateRef{
			Namespace: row.ActorTemplateNamespace, Name: row.ActorTemplateName,
			UID: row.ActorTemplateUid, Phase: row.Phase, GoldenSnapshot: row.GoldenSnapshot,
		}})
	}
	return result, nil
}

func (c *Postgres) DeleteUnreferencedRuntimeRevision(ctx context.Context, revision string) error {
	return c.q.DeleteUnreferencedRuntimeRevision(ctx, revision)
}

var _ RuntimeRevisionStore = (*Postgres)(nil)
