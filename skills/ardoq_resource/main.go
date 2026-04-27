package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	ardoqclient "github.com/joshka0/foxctl/internal/interfaces/ardoq"
)

const command = "ardoq/resource"

type input struct {
	APIHost         string           `json:"api_host"`
	OrgLabel        string           `json:"org_label"`
	Format          string           `json:"format"`
	FormatLimit     int              `json:"format_limit"`
	Operation       string           `json:"operation"`
	Workspaces      *workspacesReq   `json:"workspaces"`
	Workspace       *idReq           `json:"workspace"`
	Context         *idReq           `json:"context"`
	Components      *componentsReq   `json:"components"`
	Component       *idReq           `json:"component"`
	References      *referencesReq   `json:"references"`
	Reference       *idReq           `json:"reference"`
	Inventory       *inventoryReq    `json:"inventory"`
	OwnerLookup     *ownerLookupReq  `json:"owner_lookup"`
	Batch           *batchReq        `json:"batch"`
	UpsertComponent *singleUpsertReq `json:"upsert_component"`
	UpsertReference *singleUpsertReq `json:"upsert_reference"`
}

type workspacesReq struct {
	Name  string         `json:"name"`
	Query map[string]any `json:"query"`
}

type componentsReq struct {
	RootWorkspace string         `json:"root_workspace"`
	TypeID        string         `json:"type_id"`
	Name          string         `json:"name"`
	ComponentKey  string         `json:"component_key"`
	Query         map[string]any `json:"query"`
}

type referencesReq struct {
	RootWorkspace   string         `json:"root_workspace"`
	TargetWorkspace string         `json:"target_workspace"`
	Source          string         `json:"source"`
	Target          string         `json:"target"`
	Type            any            `json:"type"`
	Query           map[string]any `json:"query"`
}

type idReq struct {
	ID string `json:"id"`
}

type batchReq struct {
	Body map[string]any `json:"body"`
}

type inventoryReq struct {
	IncludeEmpty bool `json:"include_empty"`
}

type ownerLookupReq struct {
	Name               string   `json:"name"`
	Email              string   `json:"email"`
	PersonID           string   `json:"person_id"`
	ReferenceTypeNames []string `json:"reference_type_names"`
	IncludeExpert      bool     `json:"include_expert"`
}

type singleUpsertReq struct {
	BatchID  string         `json:"batch_id"`
	UniqueBy []string       `json:"unique_by"`
	Body     map[string]any `json:"body"`
	Aliases  map[string]any `json:"aliases"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	in.Operation = oputil.DefaultOp(in.Operation, "list_workspaces")

	client, err := newClient(in)
	if err != nil {
		return skillerr.Arg(
			"ardoq configuration is incomplete",
			skillerr.WithCause(err),
			skillerr.WithHint("Set ARDOQ_API_TOKEN and ARDOQ_ORG_LABEL, or pass org_label. Optionally set ARDOQ_API_HOST or pass api_host; the default host is https://app.ardoq.com."),
		)
	}

	data, err := oputil.NewSwitch(in.Operation).
		Case("list_workspaces", func() (map[string]any, error) { return listWorkspaces(ctx, rc, client, in.Workspaces) }).
		Case("get_workspace", func() (map[string]any, error) { return getWorkspace(ctx, rc, client, in.Workspace) }).
		Case("workspace_context", func() (map[string]any, error) { return getWorkspaceContext(ctx, rc, client, in.Context) }).
		Case("list_components", func() (map[string]any, error) { return listComponents(ctx, rc, client, in.Components) }).
		Case("get_component", func() (map[string]any, error) { return getComponent(ctx, rc, client, in.Component) }).
		Case("list_references", func() (map[string]any, error) { return listReferences(ctx, rc, client, in.References) }).
		Case("get_reference", func() (map[string]any, error) { return getReference(ctx, rc, client, in.Reference) }).
		Case("inventory", func() (map[string]any, error) { return inventory(ctx, rc, client, in.Inventory) }).
		Case("owner_lookup", func() (map[string]any, error) { return ownerLookup(ctx, rc, client, in.OwnerLookup) }).
		Case("batch", func() (map[string]any, error) { return batch(ctx, rc, client, in.Batch) }).
		Case("upsert_component", func() (map[string]any, error) { return upsert(ctx, rc, client, "components", in.UpsertComponent) }).
		Case("upsert_reference", func() (map[string]any, error) { return upsert(ctx, rc, client, "references", in.UpsertReference) }).
		Run()
	if err != nil {
		var invalid *oputil.InvalidOpError
		if errors.As(err, &invalid) {
			return skillerr.Arg(err.Error(), skillerr.WithHint("Use one of: batch, get_component, get_reference, get_workspace, inventory, list_components, list_references, list_workspaces, owner_lookup, upsert_component, upsert_reference, workspace_context."))
		}
		return err
	}

	formatted, err := formatOutput(in, data)
	if err != nil {
		return err
	}
	return skillout.Emit(rc, command, formatted)
}

func newClient(in input) (*ardoqclient.Client, error) {
	return ardoqclient.NewClient(ardoqclient.Config{
		BaseURL:  firstNonEmpty(in.APIHost, os.Getenv("ARDOQ_API_HOST")),
		OrgLabel: firstNonEmpty(in.OrgLabel, os.Getenv("ARDOQ_ORG_LABEL")),
		APIToken: os.Getenv("ARDOQ_API_TOKEN"),
	})
}

func listWorkspaces(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *workspacesReq) (map[string]any, error) {
	query := map[string]any{}
	if req != nil {
		query = cloneMap(req.Query)
		addNonEmpty(query, "name", req.Name)
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListWorkspaces(ctx, query)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("list workspaces", err)
	}
	result["operation"] = "list_workspaces"
	return result, nil
}

func getWorkspace(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *idReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, skillerr.Arg("workspace.id is required")
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.GetWorkspace(ctx, req.ID)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("get workspace", err)
	}
	result["operation"] = "get_workspace"
	return result, nil
}

func getWorkspaceContext(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *idReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, skillerr.Arg("context.id is required")
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.GetWorkspaceContext(ctx, req.ID)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("get workspace context", err)
	}
	result["operation"] = "workspace_context"
	return result, nil
}

func listComponents(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *componentsReq) (map[string]any, error) {
	query := map[string]any{}
	if req != nil {
		query = cloneMap(req.Query)
		addNonEmpty(query, "rootWorkspace", req.RootWorkspace)
		addNonEmpty(query, "typeId", req.TypeID)
		addNonEmpty(query, "name", req.Name)
		addNonEmpty(query, "componentKey", req.ComponentKey)
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListComponents(ctx, query)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("list components", err)
	}
	result["operation"] = "list_components"
	return result, nil
}

func getComponent(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *idReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, skillerr.Arg("component.id is required")
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.GetComponent(ctx, req.ID)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("get component", err)
	}
	result["operation"] = "get_component"
	return result, nil
}

func listReferences(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *referencesReq) (map[string]any, error) {
	query := map[string]any{}
	if req != nil {
		query = cloneMap(req.Query)
		addNonEmpty(query, "rootWorkspace", req.RootWorkspace)
		addNonEmpty(query, "targetWorkspace", req.TargetWorkspace)
		addNonEmpty(query, "source", req.Source)
		addNonEmpty(query, "target", req.Target)
		if req.Type != nil {
			query["type"] = req.Type
		}
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListReferences(ctx, query)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("list references", err)
	}
	result["operation"] = "list_references"
	return result, nil
}

func getReference(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *idReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, skillerr.Arg("reference.id is required")
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.GetReference(ctx, req.ID)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("get reference", err)
	}
	result["operation"] = "get_reference"
	return result, nil
}

func inventory(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *inventoryReq) (map[string]any, error) {
	if req == nil {
		req = &inventoryReq{}
	}
	workspaces, err := fetchWorkspaces(ctx, rc, client)
	if err != nil {
		return nil, err
	}
	components, err := fetchComponents(ctx, rc, client)
	if err != nil {
		return nil, err
	}

	workspaceRows := buildWorkspaceRows(workspaces, components, req.IncludeEmpty)
	totalComponents := 0
	for _, row := range workspaceRows {
		totalComponents += intValue(row["component_count"])
	}
	return map[string]any{
		"operation":         "inventory",
		"summary":           fmt.Sprintf("%d workspaces, %d components", len(workspaces), totalComponents),
		"workspace_count":   len(workspaces),
		"component_count":   totalComponents,
		"workspaces":        workspaceRows,
		"component_summary": markdownInventory(workspaceRows, len(workspaceRows), 0),
	}, nil
}

func ownerLookup(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *ownerLookupReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg("owner_lookup options are required")
	}
	if strings.TrimSpace(req.PersonID) == "" && strings.TrimSpace(req.Name) == "" && strings.TrimSpace(req.Email) == "" {
		return nil, skillerr.Arg("owner_lookup requires person_id, name, or email")
	}

	workspaces, err := fetchWorkspaces(ctx, rc, client)
	if err != nil {
		return nil, err
	}
	components, err := fetchComponents(ctx, rc, client)
	if err != nil {
		return nil, err
	}
	references, err := fetchReferences(ctx, rc, client)
	if err != nil {
		return nil, err
	}

	componentByID := componentsByID(components)
	workspaceByID := workspacesByID(workspaces)
	people := findPeople(components, req)
	if len(people) == 0 {
		return map[string]any{
			"operation": "owner_lookup",
			"summary":   "No matching person component found",
			"matches":   []any{},
			"items":     []any{},
		}, nil
	}

	allowedTypes := ownerReferenceTypeNames(req)
	contextCache := map[string]map[int]string{}
	items := make([]map[string]any, 0)
	for _, person := range people {
		personID := stringValue(person["_id"])
		for _, ref := range references {
			if stringValue(ref["source"]) != personID {
				continue
			}
			rootWorkspace := stringValue(ref["rootWorkspace"])
			typeName, err := referenceTypeName(ctx, rc, client, contextCache, rootWorkspace, intValue(ref["type"]))
			if err != nil {
				return nil, err
			}
			if !allowedTypes[typeName] {
				continue
			}
			targetID := stringValue(ref["target"])
			target := componentByID[targetID]
			targetWorkspace := stringValue(ref["targetWorkspace"])
			if targetWorkspace == "" && target != nil {
				targetWorkspace = stringValue(target["rootWorkspace"])
			}
			items = append(items, map[string]any{
				"relationship":        typeName,
				"reference_id":        stringValue(ref["_id"]),
				"component_id":        targetID,
				"component_key":       stringValue(valueFrom(target, "componentKey")),
				"component_name":      stringValue(valueFrom(target, "name")),
				"component_type":      stringValue(valueFrom(target, "type")),
				"workspace_id":        targetWorkspace,
				"workspace_name":      workspaceName(workspaceByID, targetWorkspace),
				"source_person_id":    personID,
				"source_person_name":  stringValue(person["name"]),
				"source_person_email": personEmail(person),
			})
		}
	}
	sortOwnerItems(items)

	return map[string]any{
		"operation":            "owner_lookup",
		"summary":              fmt.Sprintf("Found %d owned items for %s", len(items), ownerLabel(req, people[0])),
		"owner":                personSummary(people[0]),
		"matches":              summarizePeople(people),
		"reference_type_names": keysOfBoolMap(allowedTypes),
		"item_count":           len(items),
		"items":                items,
		"ownership_summary":    markdownOwnerItems(items, len(items), 0),
		"component_population": fmt.Sprintf("%d components scanned", len(components)),
		"reference_population": fmt.Sprintf("%d references scanned", len(references)),
	}, nil
}

func formatOutput(in input, data map[string]any) (map[string]any, error) {
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" || format == "json" {
		return data, nil
	}
	limit := in.FormatLimit
	if limit <= 0 {
		limit = 100
	}

	var rendered string
	switch format {
	case "markdown", "md":
		rendered = renderMarkdown(data, limit)
		format = "markdown"
	case "text", "plain":
		rendered = markdownToPlain(renderMarkdown(data, limit))
		format = "text"
	case "graph", "mermaid":
		rendered = renderMermaid(data, limit)
		format = "graph"
	case "ascii", "tree":
		rendered = renderASCII(data, limit)
		format = "ascii"
	default:
		return nil, skillerr.Arg("format must be one of: json, markdown, text, graph, ascii")
	}
	if strings.TrimSpace(rendered) == "" {
		rendered = fmt.Sprintf("%v", data)
	}

	out := map[string]any{
		"operation": data["operation"],
		"format":    format,
		"summary":   data["summary"],
		"rendered":  rendered,
	}
	for _, key := range []string{"workspace_count", "component_count", "item_count", "owner", "reference_type_names"} {
		if value, ok := data[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func renderMarkdown(data map[string]any, limit int) string {
	switch stringValue(data["operation"]) {
	case "inventory":
		if rows, ok := mapSlice(data["workspaces"]); ok {
			return markdownInventory(limited(rows, limit), len(rows), limit)
		}
		return stringValue(data["component_summary"])
	case "owner_lookup":
		if items, ok := mapSlice(data["items"]); ok {
			return markdownOwnerItems(limited(items, limit), len(items), limit)
		}
		return stringValue(data["ownership_summary"])
	case "list_workspaces":
		return renderWorkspaceListMarkdown(data, limit)
	case "list_components":
		return renderComponentListMarkdown(data, limit)
	case "list_references":
		return renderReferenceListMarkdown(data, limit)
	case "workspace_context":
		return renderWorkspaceContextMarkdown(data, limit)
	case "get_workspace", "get_component", "get_reference":
		return renderObjectMarkdown(data)
	default:
		return renderObjectMarkdown(data)
	}
}

func renderWorkspaceListMarkdown(data map[string]any, limit int) string {
	values := objectValues(data)
	var b strings.Builder
	writeLimitNote(&b, len(values), limit)
	b.WriteString("| Workspace | Key | ID |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, workspace := range limited(values, limit) {
		b.WriteString("| ")
		b.WriteString(escapePipes(stringValue(workspace["name"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(workspace["workspaceKey"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(workspace["_id"])))
		b.WriteString(" |\n")
	}
	return b.String()
}

func renderComponentListMarkdown(data map[string]any, limit int) string {
	values := objectValues(data)
	var b strings.Builder
	writeLimitNote(&b, len(values), limit)
	b.WriteString("| Component | Key | Type | Workspace ID |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, component := range limited(values, limit) {
		b.WriteString("| ")
		b.WriteString(escapePipes(stringValue(component["name"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(component["componentKey"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(component["type"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(component["rootWorkspace"])))
		b.WriteString(" |\n")
	}
	return b.String()
}

func renderReferenceListMarkdown(data map[string]any, limit int) string {
	values := objectValues(data)
	var b strings.Builder
	writeLimitNote(&b, len(values), limit)
	b.WriteString("| Reference ID | Type | Source | Target | Source workspace | Target workspace |\n")
	b.WriteString("| --- | ---: | --- | --- | --- | --- |\n")
	for _, ref := range limited(values, limit) {
		b.WriteString("| ")
		b.WriteString(escapePipes(stringValue(ref["_id"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(ref["type"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(ref["source"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(ref["target"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(ref["rootWorkspace"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(ref["targetWorkspace"])))
		b.WriteString(" |\n")
	}
	return b.String()
}

func renderWorkspaceContextMarkdown(data map[string]any, limit int) string {
	var b strings.Builder
	if refTypes, ok := data["referenceTypes"].([]any); ok {
		writeLimitNote(&b, len(refTypes), limit)
		b.WriteString("| Reference type | ID | Custom fields |\n")
		b.WriteString("| --- | ---: | --- |\n")
		for _, item := range limitedAny(refTypes, limit) {
			obj, _ := item.(map[string]any)
			b.WriteString("| ")
			b.WriteString(escapePipes(stringValue(obj["name"])))
			b.WriteString(" | ")
			b.WriteString(escapePipes(stringValue(obj["type"])))
			b.WriteString(" | ")
			b.WriteString(escapePipes(stringSliceText(obj["customFields"])))
			b.WriteString(" |\n")
		}
	}
	if componentTypes, ok := data["componentTypes"].([]any); ok {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		writeLimitNote(&b, len(componentTypes), limit)
		b.WriteString("| Component type | ID |\n")
		b.WriteString("| --- | --- |\n")
		for _, item := range limitedAny(componentTypes, limit) {
			obj, _ := item.(map[string]any)
			b.WriteString("| ")
			b.WriteString(escapePipes(firstNonEmpty(stringValue(obj["name"]), stringValue(obj["label"]))))
			b.WriteString(" | ")
			b.WriteString(escapePipes(firstNonEmpty(stringValue(obj["id"]), stringValue(obj["typeId"]), stringValue(obj["type"]))))
			b.WriteString(" |\n")
		}
	}
	return b.String()
}

func renderMermaid(data map[string]any, limit int) string {
	switch stringValue(data["operation"]) {
	case "owner_lookup":
		if items, ok := mapSlice(data["items"]); ok {
			return renderOwnerMermaid(items, data["owner"], limit)
		}
	case "inventory":
		if rows, ok := mapSlice(data["workspaces"]); ok {
			return renderInventoryMermaid(rows, limit)
		}
	case "list_references":
		return renderReferencesMermaid(objectValues(data), limit)
	}
	return renderObjectMermaid(data)
}

func renderASCII(data map[string]any, limit int) string {
	switch stringValue(data["operation"]) {
	case "owner_lookup":
		if items, ok := mapSlice(data["items"]); ok {
			return renderOwnerASCII(items, data["owner"], limit)
		}
	case "inventory":
		if rows, ok := mapSlice(data["workspaces"]); ok {
			return renderInventoryASCII(rows, limit)
		}
	case "list_references":
		return renderReferencesASCII(objectValues(data), limit)
	}
	return renderObjectASCII(data)
}

func renderOwnerASCII(items []map[string]any, owner any, limit int) string {
	ownerName := "Owner"
	if ownerMap, ok := owner.(map[string]any); ok {
		ownerName = firstNonEmpty(stringValue(ownerMap["name"]), stringValue(ownerMap["email"]), ownerName)
	}
	var b strings.Builder
	b.WriteString(ownerName)
	b.WriteString("\n")
	writeASCIILimitNote(&b, len(items), limit)
	grouped := groupOwnerItems(limited(items, limit))
	relationships := keysOfNestedMap(grouped)
	for relIndex, relationship := range relationships {
		lastRelationship := relIndex == len(relationships)-1
		relBranch, relChild := asciiBranches(lastRelationship)
		b.WriteString(relBranch)
		b.WriteString(relationship)
		b.WriteString("\n")
		workspaces := keysOfMapSlice(grouped[relationship])
		for wsIndex, workspace := range workspaces {
			lastWorkspace := wsIndex == len(workspaces)-1
			wsBranch, wsChild := asciiBranches(lastWorkspace)
			b.WriteString(relChild)
			b.WriteString(wsBranch)
			b.WriteString(workspace)
			b.WriteString("\n")
			components := grouped[relationship][workspace]
			for itemIndex, item := range components {
				lastItem := itemIndex == len(components)-1
				itemBranch, _ := asciiBranches(lastItem)
				b.WriteString(relChild)
				b.WriteString(wsChild)
				b.WriteString(itemBranch)
				b.WriteString(firstNonEmpty(stringValue(item["component_name"]), stringValue(item["component_id"])))
				if key := stringValue(item["component_key"]); key != "" {
					b.WriteString(" [")
					b.WriteString(key)
					b.WriteString("]")
				}
				if typ := stringValue(item["component_type"]); typ != "" {
					b.WriteString(" (")
					b.WriteString(typ)
					b.WriteString(")")
				}
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func renderInventoryASCII(rows []map[string]any, limit int) string {
	var b strings.Builder
	b.WriteString("Ardoq workspaces")
	b.WriteString("\n")
	writeASCIILimitNote(&b, len(rows), limit)
	visible := limited(rows, limit)
	for i, row := range visible {
		lastWorkspace := i == len(visible)-1
		wsBranch, wsChild := asciiBranches(lastWorkspace)
		b.WriteString(wsBranch)
		b.WriteString(firstNonEmpty(stringValue(row["name"]), stringValue(row["id"])))
		b.WriteString(fmt.Sprintf(" (%d)", intValue(row["component_count"])))
		if key := stringValue(row["key"]); key != "" {
			b.WriteString(" [")
			b.WriteString(key)
			b.WriteString("]")
		}
		b.WriteString("\n")
		if types, ok := mapSlice(row["types"]); ok {
			visibleTypes := limited(types, 5)
			for j, typeCount := range visibleTypes {
				lastType := j == len(visibleTypes)-1
				typeBranch, _ := asciiBranches(lastType)
				b.WriteString(wsChild)
				b.WriteString(typeBranch)
				b.WriteString(fmt.Sprintf("%s:%d", stringValue(typeCount["type"]), intValue(typeCount["count"])))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func renderReferencesASCII(references []map[string]any, limit int) string {
	var b strings.Builder
	b.WriteString("Ardoq references")
	b.WriteString("\n")
	writeASCIILimitNote(&b, len(references), limit)
	visible := limited(references, limit)
	for i, ref := range visible {
		last := i == len(visible)-1
		branch, child := asciiBranches(last)
		b.WriteString(branch)
		b.WriteString(stringValue(ref["source"]))
		b.WriteString(fmt.Sprintf(" --%s--> ", stringValue(ref["type"])))
		b.WriteString(stringValue(ref["target"]))
		b.WriteString("\n")
		if id := stringValue(ref["_id"]); id != "" {
			b.WriteString(child)
			b.WriteString("ref ")
			b.WriteString(id)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderObjectASCII(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		if key == "values" || strings.HasPrefix(key, "_") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(firstNonEmpty(stringValue(data["operation"]), "ardoq"))
	b.WriteString("\n")
	for i, key := range keys {
		last := i == len(keys)-1
		branch, _ := asciiBranches(last)
		b.WriteString(branch)
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(stringValue(data[key]))
		b.WriteString("\n")
	}
	return b.String()
}

func groupOwnerItems(items []map[string]any) map[string]map[string][]map[string]any {
	grouped := map[string]map[string][]map[string]any{}
	for _, item := range items {
		relationship := firstNonEmpty(stringValue(item["relationship"]), "Related")
		workspace := firstNonEmpty(stringValue(item["workspace_name"]), "Unknown workspace")
		if grouped[relationship] == nil {
			grouped[relationship] = map[string][]map[string]any{}
		}
		grouped[relationship][workspace] = append(grouped[relationship][workspace], item)
	}
	return grouped
}

func keysOfNestedMap(values map[string]map[string][]map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func keysOfMapSlice(values map[string][]map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func asciiBranches(last bool) (branch string, childPrefix string) {
	if last {
		return "`-- ", "    "
	}
	return "|-- ", "|   "
}

func writeASCIILimitNote(b *strings.Builder, total, limit int) {
	if limit > 0 && total > limit {
		b.WriteString(fmt.Sprintf("|-- showing %d of %d rows; increase format_limit to render more\n", limit, total))
	}
}

func renderOwnerMermaid(items []map[string]any, owner any, limit int) string {
	var ownerName string
	if ownerMap, ok := owner.(map[string]any); ok {
		ownerName = firstNonEmpty(stringValue(ownerMap["name"]), stringValue(ownerMap["email"]), "Owner")
	}
	if ownerName == "" {
		ownerName = "Owner"
	}
	var b strings.Builder
	b.WriteString("graph LR\n")
	writeMermaidLimitComment(&b, len(items), limit)
	ownerID := "owner"
	b.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", ownerID, mermaidLabel(ownerName)))
	for i, item := range limited(items, limit) {
		itemID := fmt.Sprintf("item%d", i)
		workspaceID := fmt.Sprintf("workspace%d", i)
		b.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", itemID, mermaidLabel(firstNonEmpty(stringValue(item["component_name"]), stringValue(item["component_id"])))))
		b.WriteString(fmt.Sprintf("  %s((\"%s\"))\n", workspaceID, mermaidLabel(stringValue(item["workspace_name"]))))
		b.WriteString(fmt.Sprintf("  %s -- \"%s\" --> %s\n", ownerID, mermaidLabel(stringValue(item["relationship"])), itemID))
		b.WriteString(fmt.Sprintf("  %s -. \"in\" .-> %s\n", itemID, workspaceID))
	}
	return b.String()
}

func renderInventoryMermaid(rows []map[string]any, limit int) string {
	var b strings.Builder
	b.WriteString("graph TD\n")
	writeMermaidLimitComment(&b, len(rows), limit)
	b.WriteString("  org[\"Ardoq workspaces\"]\n")
	for i, row := range limited(rows, limit) {
		workspaceID := fmt.Sprintf("workspace%d", i)
		label := fmt.Sprintf("%s (%d)", stringValue(row["name"]), intValue(row["component_count"]))
		b.WriteString(fmt.Sprintf("  org --> %s[\"%s\"]\n", workspaceID, mermaidLabel(label)))
		if types, ok := mapSlice(row["types"]); ok {
			for j, typeCount := range limited(types, 5) {
				typeID := fmt.Sprintf("%s_type%d", workspaceID, j)
				typeLabel := fmt.Sprintf("%s:%d", stringValue(typeCount["type"]), intValue(typeCount["count"]))
				b.WriteString(fmt.Sprintf("  %s --> %s[\"%s\"]\n", workspaceID, typeID, mermaidLabel(typeLabel)))
			}
		}
	}
	return b.String()
}

func renderReferencesMermaid(references []map[string]any, limit int) string {
	var b strings.Builder
	b.WriteString("graph LR\n")
	writeMermaidLimitComment(&b, len(references), limit)
	for i, ref := range limited(references, limit) {
		sourceID := fmt.Sprintf("source%d", i)
		targetID := fmt.Sprintf("target%d", i)
		b.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", sourceID, mermaidLabel(stringValue(ref["source"]))))
		b.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", targetID, mermaidLabel(stringValue(ref["target"]))))
		b.WriteString(fmt.Sprintf("  %s -- \"%s\" --> %s\n", sourceID, mermaidLabel(stringValue(ref["type"])), targetID))
	}
	return b.String()
}

func renderObjectMermaid(data map[string]any) string {
	operation := firstNonEmpty(stringValue(data["operation"]), "ardoq")
	return fmt.Sprintf("graph TD\n  result[\"%s\"]\n", mermaidLabel(operation))
}

func writeMermaidLimitComment(b *strings.Builder, total, limit int) {
	if limit > 0 && total > limit {
		b.WriteString(fmt.Sprintf("  %% Showing %d of %d rows. Increase format_limit to render more.\n", limit, total))
	}
}

func mermaidLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "'")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func renderObjectMarkdown(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		if key == "values" || strings.HasPrefix(key, "_") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("| Field | Value |\n")
	b.WriteString("| --- | --- |\n")
	for _, key := range keys {
		b.WriteString("| ")
		b.WriteString(escapePipes(key))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(data[key])))
		b.WriteString(" |\n")
	}
	return b.String()
}

func markdownToPlain(markdown string) string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		if strings.HasPrefix(trimmed, "| ---") {
			continue
		}
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			cells := strings.Split(strings.Trim(trimmed, "|"), "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(strings.ReplaceAll(cells[i], "\\|", "|"))
			}
			out = append(out, strings.Join(cells, "\t"))
			continue
		}
		out = append(out, trimmed)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func writeLimitNote(b *strings.Builder, total, limit int) {
	if limit > 0 && total > limit {
		b.WriteString(fmt.Sprintf("_Showing %d of %d rows. Increase `format_limit` to render more._\n\n", limit, total))
	}
}

func limited(values []map[string]any, limit int) []map[string]any {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitedAny(values []any, limit int) []any {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func stringSliceText(value any) string {
	switch typed := value.(type) {
	case []string:
		return strings.Join(typed, ", ")
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := stringValue(item); text != "" {
				values = append(values, text)
			}
		}
		return strings.Join(values, ", ")
	default:
		return stringValue(value)
	}
}

func batch(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, req *batchReq) (map[string]any, error) {
	if req == nil || len(req.Body) == 0 {
		return nil, skillerr.Arg("batch.body is required")
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.Batch(ctx, req.Body)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("batch", err)
	}
	if result == nil {
		result = map[string]any{}
	}
	result["operation"] = "batch"
	return result, nil
}

func upsert(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, section string, req *singleUpsertReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg(fmt.Sprintf("upsert_%s options are required", singular(section)))
	}
	uniqueBy := cleanedStrings(req.UniqueBy)
	if len(uniqueBy) == 0 {
		return nil, skillerr.Arg(fmt.Sprintf("upsert_%s.unique_by is required", singular(section)))
	}
	if len(req.Body) == 0 {
		return nil, skillerr.Arg(fmt.Sprintf("upsert_%s.body is required", singular(section)))
	}

	item := map[string]any{
		"uniqueBy": uniqueBy,
		"body":     req.Body,
	}
	if strings.TrimSpace(req.BatchID) != "" {
		item["batchId"] = strings.TrimSpace(req.BatchID)
	}
	payload := map[string]any{
		section: map[string]any{
			"upsert": []map[string]any{item},
		},
	}
	if len(req.Aliases) > 0 {
		payload["aliases"] = req.Aliases
	}

	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.Batch(ctx, payload)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("upsert "+singular(section), err)
	}
	if result == nil {
		result = map[string]any{}
	}
	result["operation"] = "upsert_" + singular(section)
	return result, nil
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func addNonEmpty(query map[string]any, key, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		query[key] = trimmed
	}
}

func fetchWorkspaces(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client) ([]map[string]any, error) {
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListWorkspaces(ctx, nil)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("list workspaces", err)
	}
	return objectValues(result), nil
}

func fetchComponents(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client) ([]map[string]any, error) {
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListComponents(ctx, nil)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("list components", err)
	}
	return objectValues(result), nil
}

func fetchReferences(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client) ([]map[string]any, error) {
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListReferences(ctx, nil)
		return callErr
	})
	if err != nil {
		return nil, wrapArdoqErr("list references", err)
	}
	return objectValues(result), nil
}

func objectValues(result map[string]any) []map[string]any {
	raw, _ := result["values"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

func mapSlice(value any) ([]map[string]any, bool) {
	if typed, ok := value.([]map[string]any); ok {
		return typed, true
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, obj)
	}
	return out, true
}

func buildWorkspaceRows(workspaces, components []map[string]any, includeEmpty bool) []map[string]any {
	typeCountsByWorkspace := map[string]map[string]int{}
	countByWorkspace := map[string]int{}
	for _, component := range components {
		workspaceID := stringValue(component["rootWorkspace"])
		if workspaceID == "" {
			continue
		}
		countByWorkspace[workspaceID]++
		if typeCountsByWorkspace[workspaceID] == nil {
			typeCountsByWorkspace[workspaceID] = map[string]int{}
		}
		typeCountsByWorkspace[workspaceID][stringValue(component["type"])]++
	}

	rows := make([]map[string]any, 0, len(workspaces))
	for _, workspace := range workspaces {
		workspaceID := stringValue(workspace["_id"])
		componentCount := countByWorkspace[workspaceID]
		if componentCount == 0 && !includeEmpty {
			continue
		}
		rows = append(rows, map[string]any{
			"id":              workspaceID,
			"key":             stringValue(workspace["workspaceKey"]),
			"name":            stringValue(workspace["name"]),
			"component_count": componentCount,
			"types":           sortedTypeCounts(typeCountsByWorkspace[workspaceID]),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(stringValue(rows[i]["name"])) < strings.ToLower(stringValue(rows[j]["name"]))
	})
	return rows
}

func sortedTypeCounts(counts map[string]int) []map[string]any {
	type pair struct {
		name  string
		count int
	}
	pairs := make([]pair, 0, len(counts))
	for name, count := range counts {
		pairs = append(pairs, pair{name: name, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].name < pairs[j].name
		}
		return pairs[i].count > pairs[j].count
	})
	out := make([]map[string]any, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, map[string]any{"type": pair.name, "count": pair.count})
	}
	return out
}

func markdownInventory(rows []map[string]any, totalRows, limit int) string {
	var b strings.Builder
	writeLimitNote(&b, totalRows, limit)
	b.WriteString("| Workspace | Key | Components | Top component types |\n")
	b.WriteString("| --- | --- | ---: | --- |\n")
	for _, row := range rows {
		b.WriteString("| ")
		b.WriteString(escapePipes(stringValue(row["name"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(row["key"])))
		b.WriteString(" | ")
		b.WriteString(fmt.Sprintf("%d", intValue(row["component_count"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(typeCountsText(row["types"])))
		b.WriteString(" |\n")
	}
	return b.String()
}

func typeCountsText(value any) string {
	raw, _ := value.([]map[string]any)
	if len(raw) == 0 {
		if generic, ok := value.([]any); ok {
			parts := make([]string, 0, len(generic))
			for _, item := range generic {
				obj, _ := item.(map[string]any)
				if obj != nil {
					parts = append(parts, fmt.Sprintf("%s:%d", stringValue(obj["type"]), intValue(obj["count"])))
				}
			}
			return strings.Join(parts, ", ")
		}
		return ""
	}
	parts := make([]string, 0, len(raw))
	for _, item := range raw {
		parts = append(parts, fmt.Sprintf("%s:%d", stringValue(item["type"]), intValue(item["count"])))
	}
	return strings.Join(parts, ", ")
}

func componentsByID(components []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(components))
	for _, component := range components {
		if id := stringValue(component["_id"]); id != "" {
			out[id] = component
		}
	}
	return out
}

func workspacesByID(workspaces []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(workspaces))
	for _, workspace := range workspaces {
		if id := stringValue(workspace["_id"]); id != "" {
			out[id] = workspace
		}
	}
	return out
}

func findPeople(components []map[string]any, req *ownerLookupReq) []map[string]any {
	personID := strings.TrimSpace(req.PersonID)
	name := strings.ToLower(strings.TrimSpace(req.Name))
	email := strings.ToLower(strings.TrimSpace(req.Email))
	out := make([]map[string]any, 0)
	for _, component := range components {
		if !strings.EqualFold(stringValue(component["type"]), "Person") {
			continue
		}
		if personID != "" && stringValue(component["_id"]) == personID {
			return []map[string]any{component}
		}
		if email != "" && strings.EqualFold(personEmail(component), email) {
			out = append(out, component)
			continue
		}
		if name != "" && strings.EqualFold(stringValue(component["name"]), name) {
			out = append(out, component)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return stringValue(out[i]["name"]) < stringValue(out[j]["name"])
	})
	return out
}

func ownerReferenceTypeNames(req *ownerLookupReq) map[string]bool {
	names := cleanedStrings(req.ReferenceTypeNames)
	if len(names) == 0 {
		names = []string{"Owns", "Technical Owner", "Technical owner of", "Business owner of", "Product owner of"}
	}
	if req.IncludeExpert {
		names = append(names, "Is expert in")
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

func referenceTypeName(ctx context.Context, rc *skillmain.RunContext, client *ardoqclient.Client, cache map[string]map[int]string, workspaceID string, typeID int) (string, error) {
	if workspaceID == "" {
		return fmt.Sprintf("%d", typeID), nil
	}
	if cache[workspaceID] == nil {
		var contextResult map[string]any
		err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
			var callErr error
			contextResult, callErr = client.GetWorkspaceContext(ctx, workspaceID)
			return callErr
		})
		if err != nil {
			return "", wrapArdoqErr("get workspace context", err)
		}
		cache[workspaceID] = referenceTypeMap(contextResult)
	}
	if name := cache[workspaceID][typeID]; name != "" {
		return name, nil
	}
	return fmt.Sprintf("%d", typeID), nil
}

func referenceTypeMap(contextResult map[string]any) map[int]string {
	raw, _ := contextResult["referenceTypes"].([]any)
	out := make(map[int]string, len(raw))
	for _, item := range raw {
		obj, _ := item.(map[string]any)
		if obj == nil {
			continue
		}
		out[intValue(obj["type"])] = stringValue(obj["name"])
	}
	return out
}

func sortOwnerItems(items []map[string]any) {
	sort.Slice(items, func(i, j int) bool {
		keys := []string{"relationship", "workspace_name", "component_type", "component_name"}
		for _, key := range keys {
			a := strings.ToLower(stringValue(items[i][key]))
			b := strings.ToLower(stringValue(items[j][key]))
			if a != b {
				return a < b
			}
		}
		return false
	})
}

func markdownOwnerItems(items []map[string]any, totalRows, limit int) string {
	var b strings.Builder
	writeLimitNote(&b, totalRows, limit)
	b.WriteString("| Relationship | Workspace | Type | Component | Key |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, item := range items {
		b.WriteString("| ")
		b.WriteString(escapePipes(stringValue(item["relationship"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(item["workspace_name"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(item["component_type"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(item["component_name"])))
		b.WriteString(" | ")
		b.WriteString(escapePipes(stringValue(item["component_key"])))
		b.WriteString(" |\n")
	}
	return b.String()
}

func summarizePeople(people []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(people))
	for _, person := range people {
		out = append(out, personSummary(person))
	}
	return out
}

func personSummary(person map[string]any) map[string]any {
	return map[string]any{
		"id":            stringValue(person["_id"]),
		"component_key": stringValue(person["componentKey"]),
		"name":          stringValue(person["name"]),
		"email":         personEmail(person),
		"workspace_id":  stringValue(person["rootWorkspace"]),
	}
}

func personEmail(person map[string]any) string {
	fields, _ := person["customFields"].(map[string]any)
	for _, key := range []string{"user_principal_name", "contact_email"} {
		if value := stringValue(fields[key]); value != "" {
			return value
		}
	}
	return ""
}

func ownerLabel(req *ownerLookupReq, person map[string]any) string {
	if name := strings.TrimSpace(req.Name); name != "" {
		return name
	}
	if email := strings.TrimSpace(req.Email); email != "" {
		return email
	}
	if person != nil {
		if name := stringValue(person["name"]); name != "" {
			return name
		}
	}
	return strings.TrimSpace(req.PersonID)
}

func workspaceName(workspaceByID map[string]map[string]any, id string) string {
	if workspace := workspaceByID[id]; workspace != nil {
		return stringValue(workspace["name"])
	}
	return id
}

func valueFrom(obj map[string]any, key string) any {
	if obj == nil {
		return nil
	}
	return obj[key]
}

func cleanedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func keysOfBoolMap(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case string:
		var out int
		if _, err := fmt.Sscanf(typed, "%d", &out); err == nil {
			return out
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func escapePipes(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}

func singular(section string) string {
	switch section {
	case "components":
		return "component"
	case "references":
		return "reference"
	default:
		return strings.TrimSuffix(section, "s")
	}
}

func wrapArdoqErr(action string, err error) error {
	var ardoqErr *ardoqclient.Error
	if errors.As(err, &ardoqErr) {
		return skillerr.Runtime(fmt.Sprintf("%s failed: %s", action, ardoqErr.Error()))
	}
	return skillerr.WrapRuntime(action, err)
}
