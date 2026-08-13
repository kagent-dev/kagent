package database

import (
	"context"
	"encoding/json"
	"fmt"

	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/gen"
	"github.com/kagent-dev/kagent/go/core/internal/preparation"
)

type PreparedAttachment struct {
	Namespace         string
	AgentTemplateName string
	AgentTemplateUID  string
	HarnessName       string
	HarnessUID        string
	DesiredRevision   string
}

type PreparedRevision struct {
	Revision           string
	Namespace          string
	AgentTemplateName  string
	AgentTemplateUID   string
	HarnessName        string
	HarnessUID         string
	SourceSnapshot     json.RawMessage
	EgressDestinations []string
	BackingResource    preparation.BackingResource
}

type PreparedRevisionRef struct {
	Revision        string
	BackingResource preparation.BackingResource
}

type PreparedRevisionStore interface {
	UpsertPreparedAttachment(context.Context, PreparedAttachment) error
	UpsertPreparedRevision(context.Context, PreparedRevision) error
	MarkPreparedRevisionSuccessful(context.Context, PreparedAttachment) error
	RetireAgentTemplateAttachments(context.Context, string, string) error
	RetireHarnessAttachment(context.Context, string, string, string) error
	RetireOtherHarnessAttachments(context.Context, string, string, []string) error
	ListUnreferencedPreparedRevisions(context.Context) ([]PreparedRevisionRef, error)
	DeleteUnreferencedPreparedRevision(context.Context, string) error
}

func (c *postgresClient) UpsertPreparedAttachment(ctx context.Context, attachment PreparedAttachment) error {
	return c.q.UpsertAgentTemplateAttachment(ctx, dbgen.UpsertAgentTemplateAttachmentParams{
		Namespace: attachment.Namespace, AgentTemplateName: attachment.AgentTemplateName,
		AgentTemplateUid: attachment.AgentTemplateUID, HarnessName: attachment.HarnessName,
		HarnessUid: attachment.HarnessUID, DesiredRevision: attachment.DesiredRevision,
	})
}

func (c *postgresClient) UpsertPreparedRevision(ctx context.Context, revision PreparedRevision) error {
	r := revision.BackingResource
	if err := c.q.UpsertPreparedRevision(ctx, dbgen.UpsertPreparedRevisionParams{
		Revision: revision.Revision, Namespace: revision.Namespace,
		AgentTemplateName: revision.AgentTemplateName, AgentTemplateUid: revision.AgentTemplateUID,
		HarnessName: revision.HarnessName, HarnessUid: revision.HarnessUID,
		SourceSnapshot: revision.SourceSnapshot, EgressDestinations: revision.EgressDestinations,
		BackingApiVersion: r.APIVersion, BackingKind: r.Kind, BackingNamespace: r.Namespace,
		BackingName: r.Name, BackingUid: r.UID, Phase: r.Phase, GoldenSnapshot: r.GoldenSnapshot,
	}); err != nil {
		return fmt.Errorf("upsert prepared revision %s: %w", revision.Revision, err)
	}
	return nil
}

func (c *postgresClient) MarkPreparedRevisionSuccessful(ctx context.Context, attachment PreparedAttachment) error {
	revision := attachment.DesiredRevision
	return c.q.MarkPreparedRevisionSuccessful(ctx, dbgen.MarkPreparedRevisionSuccessfulParams{
		Revision: &revision, Namespace: attachment.Namespace,
		AgentTemplateUid: attachment.AgentTemplateUID, HarnessUid: attachment.HarnessUID,
	})
}

func (c *postgresClient) RetireAgentTemplateAttachments(ctx context.Context, namespace, name string) error {
	return c.q.RetireAgentTemplateAttachments(ctx, dbgen.RetireAgentTemplateAttachmentsParams{Namespace: namespace, AgentTemplateName: name})
}

func (c *postgresClient) RetireHarnessAttachment(ctx context.Context, namespace, template, harness string) error {
	return c.q.RetireHarnessAttachment(ctx, dbgen.RetireHarnessAttachmentParams{Namespace: namespace, AgentTemplateName: template, HarnessName: harness})
}

func (c *postgresClient) RetireOtherHarnessAttachments(ctx context.Context, namespace, templateUID string, harnesses []string) error {
	return c.q.RetireOtherHarnessAttachments(ctx, dbgen.RetireOtherHarnessAttachmentsParams{
		Namespace: namespace, AgentTemplateUid: templateUID, HarnessNames: harnesses,
	})
}

func (c *postgresClient) ListUnreferencedPreparedRevisions(ctx context.Context) ([]PreparedRevisionRef, error) {
	rows, err := c.q.ListUnreferencedPreparedRevisions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unreferenced prepared revisions: %w", err)
	}
	result := make([]PreparedRevisionRef, 0, len(rows))
	for _, row := range rows {
		result = append(result, PreparedRevisionRef{Revision: row.Revision, BackingResource: preparation.BackingResource{
			APIVersion: row.BackingApiVersion, Kind: row.BackingKind, Namespace: row.BackingNamespace,
			Name: row.BackingName, UID: row.BackingUid, Phase: row.Phase, GoldenSnapshot: row.GoldenSnapshot,
		}})
	}
	return result, nil
}

func (c *postgresClient) DeleteUnreferencedPreparedRevision(ctx context.Context, revision string) error {
	return c.q.DeleteUnreferencedPreparedRevision(ctx, revision)
}

var _ PreparedRevisionStore = (*postgresClient)(nil)
