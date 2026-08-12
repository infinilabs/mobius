package bq

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	connection "cloud.google.com/go/bigquery/connection/apiv1"
	"cloud.google.com/go/bigquery/connection/apiv1/connectionpb"
	crm "google.golang.org/api/cloudresourcemanager/v1"
)

// connRoles are the roles the connection's service account needs: Vertex AI
// inference for AI.GENERATE_TABLE and GCS read for object tables.
var connRoles = []string{"roles/aiplatform.user", "roles/storage.objectViewer"}

// EnsureTaggingSetup makes the whole media-tagging stack ready at system
// start: cloud-resource connection → IAM grants for its service account →
// creatives dataset + remote model. Everything is idempotent. IAM failures
// (the app's credentials often lack IAM-admin) degrade to a loud log with the
// manual fallback (`make bq-connection`) instead of failing the boot.
func (bq *Client) EnsureTaggingSetup(ctx context.Context) error {
	sa, err := bq.ensureConnection(ctx)
	if err != nil {
		return fmt.Errorf("ensure connection %q: %w (manual fallback: `make bq-connection`)", bq.connection, err)
	}
	if err := ensureProjectRoles(ctx, bq.projectID, sa, connRoles); err != nil {
		slog.Error("could not grant IAM roles to the tagging connection's service account; "+
			"tag_media will fail on GCS/Vertex access until an IAM admin runs `make bq-connection`",
			"service_account", sa, "roles", connRoles, "error", err)
	}
	return bq.EnsureTaggingInfra(ctx)
}

// ensureConnection gets or creates the cloud-resource connection referenced by
// conf.yaml (e.g. "us.mobius_conn") and returns its service account.
func (bq *Client) ensureConnection(ctx context.Context) (string, error) {
	loc, id, ok := strings.Cut(bq.connection, ".")
	if !ok {
		return "", fmt.Errorf("connection %q must be <location>.<id>", bq.connection)
	}
	c, err := connection.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("connection client: %w", err)
	}
	defer c.Close()

	name := fmt.Sprintf("projects/%s/locations/%s/connections/%s", bq.projectID, loc, id)
	conn, err := c.GetConnection(ctx, &connectionpb.GetConnectionRequest{Name: name})
	if err != nil {
		conn, err = c.CreateConnection(ctx, &connectionpb.CreateConnectionRequest{
			Parent:       fmt.Sprintf("projects/%s/locations/%s", bq.projectID, loc),
			ConnectionId: id,
			Connection: &connectionpb.Connection{
				Properties: &connectionpb.Connection_CloudResource{
					CloudResource: &connectionpb.CloudResourceProperties{},
				},
			},
		})
		if err != nil {
			return "", fmt.Errorf("create: %w", err)
		}
		slog.Info("BigQuery cloud-resource connection created", "connection", name)
	}
	cr := conn.GetCloudResource()
	if cr == nil || cr.ServiceAccountId == "" {
		return "", fmt.Errorf("%s is not a cloud-resource connection (no service account)", name)
	}
	return cr.ServiceAccountId, nil
}

// ensureProjectRoles grants the given project-level roles to the service
// account if missing (read-modify-write on the IAM policy, etag-guarded).
func ensureProjectRoles(ctx context.Context, projectID, serviceAccount string, roles []string) error {
	svc, err := crm.NewService(ctx)
	if err != nil {
		return fmt.Errorf("resourcemanager client: %w", err)
	}
	policy, err := svc.Projects.GetIamPolicy(projectID, &crm.GetIamPolicyRequest{}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("get IAM policy: %w", err)
	}
	member := "serviceAccount:" + serviceAccount
	changed := false
	for _, role := range roles {
		var binding *crm.Binding
		for _, b := range policy.Bindings {
			if b.Role == role && b.Condition == nil {
				binding = b
				break
			}
		}
		if binding == nil {
			policy.Bindings = append(policy.Bindings, &crm.Binding{Role: role, Members: []string{member}})
			changed = true
			continue
		}
		if !slices.Contains(binding.Members, member) {
			binding.Members = append(binding.Members, member)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if _, err := svc.Projects.SetIamPolicy(projectID, &crm.SetIamPolicyRequest{Policy: policy}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("set IAM policy: %w", err)
	}
	slog.Info("granted tagging connection roles", "service_account", serviceAccount, "roles", roles)
	return nil
}
