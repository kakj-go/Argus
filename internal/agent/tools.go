package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type ResourceTools struct {
	Store     *postgres.Store
	Resources resource.Service
}

func (tools ResourceTools) Register(registry *mcp.Registry) error {
	if registry == nil || tools.Store == nil {
		return errors.New("resource tool registry is unavailable")
	}
	registrations := []mcp.Metadata{
		tools.query("host.list", true, "host.read", emptyObjectSchema(), noFields, tools.listHosts),
		tools.query("host.get", true, "host.read", idSchema("host_id"), requireID("host_id"), tools.getHost),
		tools.query("kubernetes.cluster.list", true, "kubernetes.read", emptyObjectSchema(), noFields, tools.listClusters),
		tools.query("kubernetes.cluster.get", true, "kubernetes.read", idSchema("cluster_id"), requireID("cluster_id"), tools.getCluster),
		tools.query("connector.list", true, "connector.read", emptyObjectSchema(), noFields, tools.listConnectors),
		tools.query("connector.get", true, "connector.read", idSchema("connector_id"), requireID("connector_id"), tools.getConnector),
		tools.query("pending_action.list", true, "pending_action.read", emptyObjectSchema(), noFields, tools.listPendingActions),
		tools.query("pending_action.get", true, "pending_action.read", stringIDSchema("action_ref"), requireString("action_ref"), tools.getPendingAction),
		tools.mutation("pending_action.cancel", "pending_action.confirm", stringIDSchema("action_ref"), requireString("action_ref"), tools.cancelPendingAction),
		tools.preview("host.create.preview", "host.manage", hostCreateSchema(), validateHostCreate, tools.previewHostCreate),
		tools.preview("host.update.preview", "host.manage", hostUpdateSchema(), validateHostUpdate, tools.previewHostUpdate),
		tools.preview("host.delete.preview", "host.manage", deleteSchema("host_id"), validateDelete("host_id"), tools.previewHostDelete),
		tools.preview("kubernetes.cluster.create.preview", "kubernetes.manage", clusterCreateSchema(), validateClusterCreate, tools.previewClusterCreate),
		tools.preview("kubernetes.cluster.update.preview", "kubernetes.manage", clusterUpdateSchema(), validateClusterUpdate, tools.previewClusterUpdate),
		tools.preview("kubernetes.cluster.delete.preview", "kubernetes.manage", deleteSchema("cluster_id"), validateDelete("cluster_id"), tools.previewClusterDelete),
	}
	for index := range registrations {
		if registrations[index].ID == "pending_action.get" {
			registrations[index].OutputVersion = "argus.pending_action/v1"
		}
	}
	for _, resourceType := range []string{"namespace", "node", "pod", "deployment", "statefulset", "daemonset", "service"} {
		resourceType := resourceType
		permission := "kubernetes.read"
		registrations = append(registrations, tools.query("kubernetes."+resourceType+".list", true, permission,
			kubernetesListSchema(resourceType), validateKubernetesList(resourceType), tools.listKubernetesResource(resourceType)))
	}
	registrations = append(registrations, tools.query("kubernetes.pod.logs", true, "kubernetes.logs",
		podLogsSchema(), validatePodLogs, tools.getKubernetesPodLogs))
	for _, metadata := range registrations {
		if err := registry.Register(metadata); err != nil {
			return err
		}
	}
	for _, preview := range []string{"host.create", "host.update", "host.delete", "kubernetes.cluster.create", "kubernetes.cluster.update", "kubernetes.cluster.delete"} {
		id := preview + ".commit"
		if err := registry.Register(mcp.Metadata{ID: id, Risk: "dangerous", Visibility: mcp.Hidden, ExecutionMode: mcp.Sequential, Required: []string{"internal.action_executor"}, InputVersion: "argus.private_action_plan/v1", OutputVersion: "argus.execution/v1", ProjectionSchema: "none", MaxResultBytes: 1024, InputSchema: emptyObjectSchema(), Execute: func(context.Context, mcp.Call) (mcp.Result, error) {
			return mcp.Result{}, errors.New("commit is executed from the immutable action plan")
		}}); err != nil {
			return err
		}
	}
	return registry.ValidatePairs()
}

func (tools ResourceTools) query(id string, parallel bool, permission string, schema map[string]any, validate func(map[string]any) error, execute func(context.Context, mcp.Call) (mcp.Result, error)) mcp.Metadata {
	mode := mcp.Sequential
	if parallel {
		mode = mcp.ParallelSafe
	}
	return tools.cardMetadata(mcp.Metadata{ID: id, Risk: "read", Visibility: mcp.Visible, ExecutionMode: mode, Required: []string{permission}, InputVersion: id + "/v1", OutputVersion: id + "/v1", ProjectionSchema: "argus.tool_result_projection/v1", MaxResultBytes: 4 << 20, InputSchema: schema, Authorize: tools.authorize(permission), Validate: validate, Execute: execute})
}
func (tools ResourceTools) mutation(id, permission string, schema map[string]any, validate func(map[string]any) error, execute func(context.Context, mcp.Call) (mcp.Result, error)) mcp.Metadata {
	return mcp.Metadata{ID: id, Risk: "write", Visibility: mcp.Visible, ExecutionMode: mcp.Sequential, Required: []string{permission}, InputVersion: id + "/v1", OutputVersion: id + "/v1", ProjectionSchema: "argus.tool_result_projection/v1", MaxResultBytes: 256 << 10, InputSchema: schema, Authorize: tools.authorize(permission), Validate: validate, Execute: execute}
}
func (tools ResourceTools) preview(id, permission string, schema map[string]any, validate func(map[string]any) error, execute func(context.Context, mcp.Call) (mcp.Result, error)) mcp.Metadata {
	metadata := tools.mutation(id, permission, schema, validate, execute)
	metadata.OutputVersion = "argus.pending_action/v1"
	metadata.ProjectionSchema = "argus.pending_action_public/v1"
	return tools.cardMetadata(metadata)
}

func (tools ResourceTools) cardMetadata(metadata mcp.Metadata) mcp.Metadata {
	metadata.CardSafe = true
	metadata.ToolFamily = metadata.ID
	metadata.OutputSchema = outputSchema(metadata.ID)
	metadata.SemanticFields = semanticFields(metadata.ID)
	metadata.FieldTypes = fieldTypes(metadata.ID)
	metadata.CardProjector = func(_ context.Context, _ mcp.Call, result mcp.Result) (map[string]any, bool, error) {
		encoded, err := json.Marshal(result.Structured)
		if err != nil {
			return nil, false, err
		}
		var projected map[string]any
		if err := json.Unmarshal(encoded, &projected); err != nil {
			return nil, false, err
		}
		return projected, result.Partial, nil
	}
	return metadata
}

func fieldTypes(toolID string) map[string]string {
	if strings.HasSuffix(toolID, ".list") {
		return map[string]string{"$.items": "array", "$.items[*].id": "string", "$.items[*].labels": "object"}
	}
	if strings.HasSuffix(toolID, ".preview") || strings.HasPrefix(toolID, "pending_action.") {
		return map[string]string{"$": "object", "$.action_ref": "string", "$.status": "string"}
	}
	if toolID == "kubernetes.pod.logs" {
		return map[string]string{"$": "object", "$.content": "string", "$.truncated": "boolean"}
	}
	return map[string]string{"$": "object", "$.id": "string", "$.labels": "object"}
}

func outputSchema(toolID string) map[string]any {
	identifier := map[string]any{"type": "string", "format": "uuid"}
	labels := map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	resource := map[string]any{"type": "object", "properties": map[string]any{
		"id": identifier, "name": map[string]any{"type": "string"}, "labels": labels, "resource_version": map[string]any{"type": "integer"},
	}, "required": []string{"id", "name"}, "additionalProperties": true}
	if strings.HasSuffix(toolID, ".list") {
		return map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": resource}}, "required": []string{"items"}, "additionalProperties": true}
	}
	if strings.HasSuffix(toolID, ".preview") || strings.HasPrefix(toolID, "pending_action.") {
		return map[string]any{"type": "object", "properties": map[string]any{
			"action_ref": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"},
			"risk": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"},
		}, "required": []string{"action_ref", "status"}, "additionalProperties": true}
	}
	if toolID == "kubernetes.pod.logs" {
		return map[string]any{"type": "object", "properties": map[string]any{"content": map[string]any{"type": "string"}, "truncated": map[string]any{"type": "boolean"}}, "required": []string{"content", "truncated"}, "additionalProperties": true}
	}
	return resource
}

func semanticFields(toolID string) map[string]string {
	if strings.HasSuffix(toolID, ".list") {
		return map[string]string{"$.items": "resource_collection", "$.items[*].id": "resource_id", "$.items[*].labels": "resource_labels"}
	}
	if strings.HasSuffix(toolID, ".preview") || strings.HasPrefix(toolID, "pending_action.") {
		return map[string]string{"$.action_ref": "pending_action_ref", "$.status": "pending_action_status"}
	}
	return map[string]string{"$.id": "resource_id", "$.labels": "resource_labels"}
}

func (tools ResourceTools) authorize(permission string) func(context.Context, mcp.Call) error {
	return func(ctx context.Context, call mcp.Call) error {
		enterpriseID, err := uuid.Parse(call.Enterprise)
		if err != nil {
			return err
		}
		subjectID, err := uuid.Parse(call.Subject)
		if err != nil {
			return err
		}
		var permissions []string
		if call.SubjectType == "service_account" {
			account, getErr := tools.Store.Queries.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: subjectID, EnterpriseID: enterpriseID})
			if getErr != nil || account.Status != "active" || !slices.Contains(account.AllowedToolIds, call.ToolID) {
				return errors.New("service account is not allowed to call the tool")
			}
			permissions, err = tools.Store.Queries.ListEffectiveServiceAccountPermissions(ctx, db.ListEffectiveServiceAccountPermissionsParams{EnterpriseID: enterpriseID, ServiceAccountID: subjectID})
		} else {
			user, getErr := tools.Store.Queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: subjectID, EnterpriseID: enterpriseID})
			if getErr != nil || user.Status != "active" {
				return errors.New("user is unavailable")
			}
			permissions, err = tools.Store.Queries.ListEffectiveUserPermissions(ctx, db.ListEffectiveUserPermissionsParams{EnterpriseID: enterpriseID, UserID: subjectID, DepartmentID: user.DepartmentID})
		}
		if err != nil {
			return err
		}
		if !slices.Contains(permissions, permission) {
			return fmt.Errorf("missing permission %s", permission)
		}
		return nil
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	value := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		value["required"] = required
	}
	return value
}

func emptyObjectSchema() map[string]any { return objectSchema(map[string]any{}) }
func idSchema(name string) map[string]any {
	return objectSchema(map[string]any{name: map[string]any{"type": "string", "format": "uuid"}}, name)
}
func stringIDSchema(name string) map[string]any {
	return objectSchema(map[string]any{name: map[string]any{"type": "string", "minLength": 1, "maxLength": 128}}, name)
}
func labelSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "maxProperties": 64}
}
func commonHostProperties() map[string]any {
	return map[string]any{
		"name": map[string]any{"type": "string"}, "hostname": map[string]any{"type": "string"}, "address": map[string]any{"type": "string"},
		"port": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}, "platform": map[string]any{"type": "string", "enum": []string{"linux", "windows"}},
		"connection_mode": map[string]any{"type": "string", "enum": []string{"via_bastion", "direct_ssh", "direct_winrm"}}, "environment": map[string]any{"type": "string"},
		"username": map[string]any{"type": "string"}, "bastion_scope_id": map[string]any{"type": "string", "format": "uuid"},
		"credential_id": map[string]any{"type": "string", "format": "uuid"}, "connection_test_id": map[string]any{"type": "string", "format": "uuid"},
		"labels": labelSchema(), "request_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
	}
}
func hostCreateSchema() map[string]any {
	return objectSchema(commonHostProperties(), "name", "address", "platform", "connection_mode", "connection_test_id")
}
func hostUpdateSchema() map[string]any {
	properties := commonHostProperties()
	properties["host_id"] = map[string]any{"type": "string", "format": "uuid"}
	properties["expected_version"] = map[string]any{"type": "integer", "minimum": 1}
	return objectSchema(properties, "host_id", "expected_version")
}
func commonClusterProperties() map[string]any {
	return map[string]any{
		"name": map[string]any{"type": "string"}, "api_server": map[string]any{"type": "string"},
		"connection_mode":   map[string]any{"type": "string", "enum": []string{"via_bastion", "direct", "in_cluster"}},
		"default_namespace": map[string]any{"type": "string"}, "environment": map[string]any{"type": "string"},
		"bastion_scope_id": map[string]any{"type": "string", "format": "uuid"}, "credential_id": map[string]any{"type": "string", "format": "uuid"},
		"connection_test_id": map[string]any{"type": "string", "format": "uuid"}, "labels": labelSchema(),
		"request_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
	}
}
func clusterCreateSchema() map[string]any {
	return objectSchema(commonClusterProperties(), "name", "connection_mode")
}
func clusterUpdateSchema() map[string]any {
	properties := commonClusterProperties()
	properties["cluster_id"] = map[string]any{"type": "string", "format": "uuid"}
	properties["expected_version"] = map[string]any{"type": "integer", "minimum": 1}
	return objectSchema(properties, "cluster_id", "expected_version")
}
func deleteSchema(id string) map[string]any {
	return objectSchema(map[string]any{id: map[string]any{"type": "string", "format": "uuid"}, "expected_version": map[string]any{"type": "integer", "minimum": 1}, "request_id": map[string]any{"type": "string"}}, id, "expected_version")
}
func kubernetesListSchema(resourceType string) map[string]any {
	properties := map[string]any{"cluster_id": map[string]any{"type": "string", "format": "uuid"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 500}, "query": map[string]any{"type": "string", "maxLength": 256}}
	required := []string{"cluster_id"}
	if namespacedKubernetesType(resourceType) {
		properties["namespace"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 253}
		required = append(required, "namespace")
	}
	return objectSchema(properties, required...)
}
func podLogsSchema() map[string]any {
	return objectSchema(map[string]any{
		"cluster_id": map[string]any{"type": "string", "format": "uuid"}, "namespace": map[string]any{"type": "string", "minLength": 1},
		"pod": map[string]any{"type": "string", "minLength": 1}, "container": map[string]any{"type": "string"},
		"tail_lines": map[string]any{"type": "integer", "minimum": 1, "maximum": 5000},
	}, "cluster_id", "namespace", "pod")
}

func validateFields(input map[string]any, allowed, required []string, uuidFields ...string) error {
	for key := range input {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("unknown field %s", key)
		}
	}
	for _, key := range required {
		value, ok := input[key]
		if !ok || value == nil || value == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	for _, key := range uuidFields {
		if value, ok := input[key]; ok {
			text, valid := value.(string)
			if !valid || uuid.Validate(text) != nil {
				return fmt.Errorf("%s must be a UUID", key)
			}
		}
	}
	return nil
}
func noFields(input map[string]any) error { return validateFields(input, nil, nil) }
func requireID(key string) func(map[string]any) error {
	return func(input map[string]any) error { return validateFields(input, []string{key}, []string{key}, key) }
}
func requireString(key string) func(map[string]any) error {
	return func(input map[string]any) error { return validateFields(input, []string{key}, []string{key}) }
}
func validateHostCreate(input map[string]any) error {
	return validateFields(input, mapKeys(commonHostProperties()), []string{"name", "address", "platform", "connection_mode", "connection_test_id"}, "bastion_scope_id", "credential_id", "connection_test_id")
}
func validateHostUpdate(input map[string]any) error {
	allowed := append(mapKeys(commonHostProperties()), "host_id", "expected_version")
	return validateFields(input, allowed, []string{"host_id", "expected_version"}, "host_id", "bastion_scope_id", "credential_id", "connection_test_id")
}
func validateClusterCreate(input map[string]any) error {
	return validateFields(input, mapKeys(commonClusterProperties()), []string{"name", "connection_mode"}, "bastion_scope_id", "credential_id", "connection_test_id")
}
func validateClusterUpdate(input map[string]any) error {
	allowed := append(mapKeys(commonClusterProperties()), "cluster_id", "expected_version")
	return validateFields(input, allowed, []string{"cluster_id", "expected_version"}, "cluster_id", "bastion_scope_id", "credential_id", "connection_test_id")
}
func validateDelete(key string) func(map[string]any) error {
	return func(input map[string]any) error {
		return validateFields(input, []string{key, "expected_version", "request_id"}, []string{key, "expected_version"}, key)
	}
}
func validateKubernetesList(resourceType string) func(map[string]any) error {
	return func(input map[string]any) error {
		allowed, required := []string{"cluster_id", "limit", "query"}, []string{"cluster_id"}
		if namespacedKubernetesType(resourceType) {
			allowed, required = append(allowed, "namespace"), append(required, "namespace")
		}
		return validateFields(input, allowed, required, "cluster_id")
	}
}
func validatePodLogs(input map[string]any) error {
	return validateFields(input, []string{"cluster_id", "namespace", "pod", "container", "tail_lines"}, []string{"cluster_id", "namespace", "pod"}, "cluster_id")
}
func mapKeys(values map[string]any) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
func namespacedKubernetesType(value string) bool {
	return value == "pod" || value == "deployment" || value == "statefulset" || value == "daemonset" || value == "service"
}

func (tools ResourceTools) subject(ctx context.Context, call mcp.Call) (resource.Subject, uuid.UUID, error) {
	enterpriseID, err := uuid.Parse(call.Enterprise)
	if err != nil {
		return resource.Subject{}, uuid.Nil, err
	}
	subjectID, err := uuid.Parse(call.Subject)
	if err != nil {
		return resource.Subject{}, uuid.Nil, err
	}
	runID := uuid.NullUUID{}
	if call.RunID != "" {
		parsed, parseErr := uuid.Parse(call.RunID)
		if parseErr != nil || call.Caller != "model" {
			return resource.Subject{}, uuid.Nil, errors.New("run identity unavailable")
		}
		runID = uuid.NullUUID{UUID: parsed, Valid: true}
	}
	if call.SubjectType == "service_account" {
		account, err := tools.Store.Queries.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: subjectID, EnterpriseID: enterpriseID})
		if err != nil || account.Status != "active" {
			return resource.Subject{}, uuid.Nil, errors.New("subject unavailable")
		}
		scopes, err := tools.Store.Queries.ListServiceAccountDataScopes(ctx, account.ID)
		return resource.Subject{ActorID: account.ID.String(), ActorType: "service_account", AuthorizationVersion: account.AuthorizationVersion, DataScopeIDs: scopes, RunID: runID}, enterpriseID, err
	}
	user, err := tools.Store.Queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: subjectID, EnterpriseID: enterpriseID})
	if err != nil || user.Status != "active" {
		return resource.Subject{}, uuid.Nil, errors.New("subject unavailable")
	}
	scopes, err := tools.Store.Queries.ListEffectiveUserDataScopes(ctx, db.ListEffectiveUserDataScopesParams{EnterpriseID: enterpriseID, UserID: user.ID, DepartmentID: user.DepartmentID})
	return resource.Subject{ActorID: user.ID.String(), AuthorizationVersion: user.AuthorizationVersion, DataScopeIDs: scopes, RunID: runID}, enterpriseID, err
}
func callID(call mcp.Call, name string) (uuid.UUID, error) {
	value, ok := call.Input[name].(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("%s is required", name)
	}
	return uuid.Parse(value)
}
func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}
func intValue(input map[string]any, key string) int64 {
	switch value := input[key].(type) {
	case float64:
		return int64(value)
	case int:
		return int64(value)
	case int64:
		return value
	default:
		return 0
	}
}
func labelsValue(input map[string]any) map[string]string {
	raw, ok := input["labels"].(map[string]any)
	if !ok {
		return nil
	}
	result := map[string]string{}
	for key, value := range raw {
		if text, ok := value.(string); ok {
			result[key] = text
		}
	}
	return result
}
func nullID(input map[string]any, key string) uuid.NullUUID {
	value := stringValue(input, key)
	parsed, err := uuid.Parse(value)
	return uuid.NullUUID{UUID: parsed, Valid: err == nil}
}
func idempotency(call mcp.Call) string {
	identity := call.InvocationID
	if identity == "" {
		identity = call.RunID
	}
	if call.CallID != "" {
		identity += "-" + call.CallID
	}
	if identity == "" {
		identity = call.Subject
	}
	return "tool-" + call.ToolID + "-" + identity
}

func (tools ResourceTools) listHosts(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	items, err := tools.Resources.ListHosts(ctx, enterpriseID, subject.DataScopeIDs)
	if err != nil {
		return mcp.Result{}, err
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		labels, _ := resource.DecodeLabels(item.Labels)
		result = append(result, map[string]any{"id": item.ID, "name": item.Name, "hostname": item.Hostname, "address": item.Address, "platform": item.Platform, "connection_mode": item.ConnectionMode, "connection_status": item.ConnectionStatus, "labels": labels, "resource_version": item.ResourceVersion})
	}
	return mcp.Result{Structured: map[string]any{"items": result}}, nil
}
func (tools ResourceTools) getHost(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	hostID, err := callID(call, "host_id")
	if err != nil {
		return mcp.Result{}, err
	}
	item, err := tools.Resources.GetHost(ctx, enterpriseID, hostID, subject.DataScopeIDs)
	if err != nil {
		return mcp.Result{}, err
	}
	labels, _ := resource.DecodeLabels(item.Labels)
	return mcp.Result{Structured: map[string]any{"id": item.ID, "name": item.Name, "hostname": item.Hostname, "address": item.Address, "platform": item.Platform, "labels": labels, "resource_version": item.ResourceVersion}}, nil
}
func (tools ResourceTools) listClusters(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	items, err := tools.Resources.ListKubernetesClusters(ctx, enterpriseID, subject.DataScopeIDs)
	if err != nil {
		return mcp.Result{}, err
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		labels, _ := resource.DecodeLabels(item.Labels)
		result = append(result, map[string]any{"id": item.ID, "name": item.Name, "api_server": item.ApiServer, "connection_mode": item.ConnectionMode, "status": item.Status, "labels": labels, "resource_version": item.ResourceVersion})
	}
	return mcp.Result{Structured: map[string]any{"items": result}}, nil
}
func (tools ResourceTools) getCluster(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	clusterID, err := callID(call, "cluster_id")
	if err != nil {
		return mcp.Result{}, err
	}
	item, err := tools.Resources.GetKubernetesCluster(ctx, enterpriseID, clusterID, subject.DataScopeIDs)
	if err != nil {
		return mcp.Result{}, err
	}
	labels, _ := resource.DecodeLabels(item.Labels)
	return mcp.Result{Structured: map[string]any{"id": item.ID, "name": item.Name, "api_server": item.ApiServer, "connection_mode": item.ConnectionMode, "labels": labels, "resource_version": item.ResourceVersion}}, nil
}
func (tools ResourceTools) listKubernetesResource(resourceType string) func(context.Context, mcp.Call) (mcp.Result, error) {
	return func(ctx context.Context, call mcp.Call) (mcp.Result, error) {
		subject, enterpriseID, err := tools.subject(ctx, call)
		if err != nil {
			return mcp.Result{}, err
		}
		clusterID, err := callID(call, "cluster_id")
		if err != nil {
			return mcp.Result{}, err
		}
		limit := int(intValue(call.Input, "limit"))
		if limit == 0 {
			limit = 100
		}
		items, err := tools.Resources.ListKubernetesResources(ctx, subject, enterpriseID, clusterID, resource.KubernetesQuery{
			ResourceType: resourceType, Namespace: stringValue(call.Input, "namespace"), Query: stringValue(call.Input, "query"), Limit: limit,
		})
		if err != nil {
			return mcp.Result{}, err
		}
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			result = append(result, map[string]any{"resource_type": item.ResourceType, "namespace": item.Namespace, "name": item.Name, "labels": item.Labels, "summary": item.Summary})
		}
		return mcp.Result{Structured: map[string]any{"cluster_id": clusterID, "resource_type": resourceType, "items": result}}, nil
	}
}
func (tools ResourceTools) getKubernetesPodLogs(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	clusterID, err := callID(call, "cluster_id")
	if err != nil {
		return mcp.Result{}, err
	}
	tailLines := intValue(call.Input, "tail_lines")
	if tailLines == 0 {
		tailLines = 500
	}
	content, truncated, err := tools.Resources.GetKubernetesPodLogs(ctx, subject, enterpriseID, clusterID, resource.PodLogsQuery{
		Namespace: stringValue(call.Input, "namespace"), Pod: stringValue(call.Input, "pod"), Container: stringValue(call.Input, "container"), TailLines: tailLines,
	})
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.Result{Structured: map[string]any{"cluster_id": clusterID, "namespace": stringValue(call.Input, "namespace"), "pod": stringValue(call.Input, "pod"),
		"container": stringValue(call.Input, "container"), "content": string(content), "bytes": len(content), "truncated": truncated}, Partial: truncated}, nil
}
func (tools ResourceTools) listConnectors(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	_, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	items, err := tools.Store.Queries.ListConnectors(ctx, enterpriseID)
	if err != nil {
		return mcp.Result{}, err
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{"id": item.ID, "name": item.Name, "role": item.Role, "status": item.Status, "connection_epoch": item.ConnectionEpoch, "last_heartbeat_at": item.LastHeartbeatAt})
	}
	return mcp.Result{Structured: map[string]any{"items": result}}, nil
}
func (tools ResourceTools) getConnector(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	_, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	connectorID, err := callID(call, "connector_id")
	if err != nil {
		return mcp.Result{}, err
	}
	item, err := tools.Store.Queries.GetConnector(ctx, db.GetConnectorParams{ID: connectorID, EnterpriseID: enterpriseID})
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.Result{Structured: map[string]any{"id": item.ID, "name": item.Name, "role": item.Role, "status": item.Status, "connection_epoch": item.ConnectionEpoch}}, nil
}
func (tools ResourceTools) listPendingActions(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	_, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	items, err := tools.Store.Queries.ListPendingActions(ctx, enterpriseID)
	if err != nil {
		return mcp.Result{}, err
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, pendingProjection(item))
	}
	return mcp.Result{Structured: map[string]any{"items": result}}, nil
}
func (tools ResourceTools) getPendingAction(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	_, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	value, err := tools.Store.Queries.GetPendingAction(ctx, db.GetPendingActionParams{ActionRef: stringValue(call.Input, "action_ref"), EnterpriseID: enterpriseID})
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.Result{Structured: pendingProjection(value)}, nil
}
func (tools ResourceTools) cancelPendingAction(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	value, err := tools.Resources.Actions.Cancel(ctx, subject.ActorID, enterpriseID, stringValue(call.Input, "action_ref"), idempotency(call))
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.Result{Structured: pendingProjection(value)}, nil
}
func pendingProjection(value db.PendingAction) map[string]any {
	return map[string]any{"action_ref": value.ActionRef, "action_type": value.ActionType, "title": value.Title, "summary": value.Summary, "risk": value.Risk, "status": value.Status, "resource_type": value.ResourceType, "resource_id": value.ResourceID, "expires_at": value.ExpiresAt.Time}
}

func hostInput(input map[string]any) resource.HostInput {
	return resource.HostInput{Name: stringValue(input, "name"), Hostname: stringValue(input, "hostname"), Address: stringValue(input, "address"), Platform: stringValue(input, "platform"), ConnectionMode: stringValue(input, "connection_mode"), Environment: stringValue(input, "environment"), Username: stringValue(input, "username"), Port: int32(intValue(input, "port")), BastionScopeID: nullID(input, "bastion_scope_id"), CredentialID: nullID(input, "credential_id"), ConnectionTestID: nullID(input, "connection_test_id"), Labels: labelsValue(input), ExpectedVersion: intValue(input, "expected_version")}
}
func clusterInput(input map[string]any) resource.KubernetesInput {
	return resource.KubernetesInput{Name: stringValue(input, "name"), APIServer: stringValue(input, "api_server"), ConnectionMode: stringValue(input, "connection_mode"), DefaultNamespace: stringValue(input, "default_namespace"), Environment: stringValue(input, "environment"), BastionScopeID: nullID(input, "bastion_scope_id"), CredentialID: nullID(input, "credential_id"), ConnectionTestID: nullID(input, "connection_test_id"), Labels: labelsValue(input), ExpectedVersion: intValue(input, "expected_version")}
}
func (tools ResourceTools) previewHostCreate(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	value, err := tools.Resources.PreviewCreateHost(ctx, subject, enterpriseID, hostInput(call.Input), idempotency(call))
	return mcp.Result{Structured: pendingProjection(value)}, err
}
func (tools ResourceTools) previewHostUpdate(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	id, err := callID(call, "host_id")
	if err != nil {
		return mcp.Result{}, err
	}
	value, err := tools.Resources.PreviewUpdateHost(ctx, subject, enterpriseID, id, hostInput(call.Input), idempotency(call))
	return mcp.Result{Structured: pendingProjection(value)}, err
}
func (tools ResourceTools) previewHostDelete(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	id, err := callID(call, "host_id")
	if err != nil {
		return mcp.Result{}, err
	}
	value, err := tools.Resources.PreviewDeleteHost(ctx, subject, enterpriseID, id, intValue(call.Input, "expected_version"), idempotency(call))
	return mcp.Result{Structured: pendingProjection(value)}, err
}
func (tools ResourceTools) previewClusterCreate(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	value, err := tools.Resources.PreviewCreateKubernetesCluster(ctx, subject, enterpriseID, clusterInput(call.Input), idempotency(call))
	return mcp.Result{Structured: pendingProjection(value)}, err
}
func (tools ResourceTools) previewClusterUpdate(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	id, err := callID(call, "cluster_id")
	if err != nil {
		return mcp.Result{}, err
	}
	value, err := tools.Resources.PreviewUpdateKubernetesCluster(ctx, subject, enterpriseID, id, clusterInput(call.Input), idempotency(call))
	return mcp.Result{Structured: pendingProjection(value)}, err
}
func (tools ResourceTools) previewClusterDelete(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	subject, enterpriseID, err := tools.subject(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	id, err := callID(call, "cluster_id")
	if err != nil {
		return mcp.Result{}, err
	}
	value, err := tools.Resources.PreviewDeleteKubernetesCluster(ctx, subject, enterpriseID, id, intValue(call.Input, "expected_version"), idempotency(call))
	return mcp.Result{Structured: pendingProjection(value)}, err
}
