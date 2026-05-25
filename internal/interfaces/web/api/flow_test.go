package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestFlowHandler_ListEmptyContract(t *testing.T) {
	handler := newFlowContractHandler(t)

	rr := serveFlowContract(t, handler, http.MethodGet, "/api/flows?workspace=contract-ws", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	body := decodeResponseBody(t, rr)
	if got := int(body["count"].(float64)); got != 0 {
		t.Fatalf("count=%d want 0", got)
	}
	flows, ok := body["flows"].([]any)
	if !ok {
		t.Fatalf("flows type=%T want []any", body["flows"])
	}
	if len(flows) != 0 {
		t.Fatalf("flows len=%d want 0", len(flows))
	}
}

func TestFlowHandler_CreateShowListContract(t *testing.T) {
	handler := newFlowContractHandler(t)

	createRR := serveFlowContract(t, handler, http.MethodPost, "/api/flows", `{
		"name":"Contract Flow",
		"workspace":"contract-ws",
		"description":"boundary test",
		"room_id":"room-1"
	}`)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d want %d body=%s", createRR.Code, http.StatusCreated, createRR.Body.String())
	}
	created := flowContractMap(t, decodeResponseBody(t, createRR), "flow")
	flowID := flowContractString(t, created, "id")
	if got := flowContractString(t, created, "name"); got != "Contract Flow" {
		t.Fatalf("created name=%q want Contract Flow", got)
	}
	if got := flowContractString(t, created, "workspace"); got != "contract-ws" {
		t.Fatalf("created workspace=%q want contract-ws", got)
	}
	if got := flowContractString(t, created, "state"); got != "draft" {
		t.Fatalf("created state=%q want draft", got)
	}

	listRR := serveFlowContract(t, handler, http.MethodGet, "/api/flows?workspace=contract-ws", "")
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status=%d want %d body=%s", listRR.Code, http.StatusOK, listRR.Body.String())
	}
	listBody := decodeResponseBody(t, listRR)
	if got := int(listBody["count"].(float64)); got != 1 {
		t.Fatalf("count=%d want 1", got)
	}
	flows := listBody["flows"].([]any)
	listed := flows[0].(map[string]any)
	if got := flowContractString(t, listed, "id"); got != flowID {
		t.Fatalf("listed id=%q want %q", got, flowID)
	}

	showRR := serveFlowContract(t, handler, http.MethodGet, "/api/flows/"+flowID, "")
	if showRR.Code != http.StatusOK {
		t.Fatalf("show status=%d want %d body=%s", showRR.Code, http.StatusOK, showRR.Body.String())
	}
	detail := decodeResponseBody(t, showRR)
	if got := flowContractString(t, detail, "id"); got != flowID {
		t.Fatalf("detail id=%q want %q", got, flowID)
	}
	if nodes := detail["nodes"].([]any); len(nodes) != 0 {
		t.Fatalf("nodes len=%d want 0", len(nodes))
	}
	if edges := detail["edges"].([]any); len(edges) != 0 {
		t.Fatalf("edges len=%d want 0", len(edges))
	}
}

func TestFlowHandler_CreateRejectsBadRequestsContract(t *testing.T) {
	handler := newFlowContractHandler(t)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty name", body: `{"name":"   "}`},
		{name: "invalid json", body: `{bad`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := serveFlowContract(t, handler, http.MethodPost, "/api/flows", tt.body)
			assertFlowErrorContract(t, rr, http.StatusBadRequest)
		})
	}
}

func TestFlowHandler_DeleteRemovesFlowContract(t *testing.T) {
	handler := newFlowContractHandler(t)
	flowID := createFlowContract(t, handler, "delete-contract")

	deleteRR := serveFlowContract(t, handler, http.MethodDelete, "/api/flows/"+flowID, "")
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("delete status=%d want %d body=%s", deleteRR.Code, http.StatusOK, deleteRR.Body.String())
	}
	deleteBody := decodeResponseBody(t, deleteRR)
	if got, ok := deleteBody["deleted"].(bool); !ok || !got {
		t.Fatalf("deleted=%v want true", deleteBody["deleted"])
	}
	if got := flowContractString(t, deleteBody, "id"); got != flowID {
		t.Fatalf("deleted id=%q want %q", got, flowID)
	}

	showRR := serveFlowContract(t, handler, http.MethodGet, "/api/flows/"+flowID, "")
	assertFlowErrorContract(t, showRR, http.StatusNotFound)
}

func TestFlowHandler_NodeAndEdgeContracts(t *testing.T) {
	handler := newFlowContractHandler(t)
	flowID := createFlowContract(t, handler, "graph-contract")

	sourceID := addFlowNodeContract(t, handler, flowID, "source")
	targetID := addFlowNodeContract(t, handler, flowID, "target")

	edgeRR := serveFlowContract(t, handler, http.MethodPost, "/api/flows/"+flowID+"/edges", `{
		"from_node_id":"`+sourceID+`",
		"to_node_id":"`+targetID+`",
		"transform":"passthrough"
	}`)
	if edgeRR.Code != http.StatusCreated {
		t.Fatalf("edge status=%d want %d body=%s", edgeRR.Code, http.StatusCreated, edgeRR.Body.String())
	}
	edge := flowContractMap(t, decodeResponseBody(t, edgeRR), "edge")
	if got := flowContractString(t, edge, "flow_id"); got != flowID {
		t.Fatalf("edge flow_id=%q want %q", got, flowID)
	}
	if got := flowContractString(t, edge, "from_node_id"); got != sourceID {
		t.Fatalf("edge from_node_id=%q want %q", got, sourceID)
	}
	if got := flowContractString(t, edge, "to_node_id"); got != targetID {
		t.Fatalf("edge to_node_id=%q want %q", got, targetID)
	}
	if got := flowContractString(t, edge, "transform"); got != "passthrough" {
		t.Fatalf("edge transform=%q want passthrough", got)
	}

	showRR := serveFlowContract(t, handler, http.MethodGet, "/api/flows/"+flowID, "")
	if showRR.Code != http.StatusOK {
		t.Fatalf("show status=%d want %d body=%s", showRR.Code, http.StatusOK, showRR.Body.String())
	}
	detail := decodeResponseBody(t, showRR)
	if nodes := detail["nodes"].([]any); len(nodes) != 2 {
		t.Fatalf("nodes len=%d want 2", len(nodes))
	}
	if edges := detail["edges"].([]any); len(edges) != 1 {
		t.Fatalf("edges len=%d want 1", len(edges))
	}
}

func TestFlowHandler_SubresourceRejectsBadRequestsContract(t *testing.T) {
	handler := newFlowContractHandler(t)
	flowID := createFlowContract(t, handler, "reject-contract")
	nodeID := addFlowNodeContract(t, handler, flowID, "same-flow")
	otherFlowID := createFlowContract(t, handler, "other-reject-contract")
	otherNodeID := addFlowNodeContract(t, handler, otherFlowID, "other-flow")

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{
			name:   "invalid node kind",
			method: http.MethodPost,
			path:   "/api/flows/" + flowID + "/nodes",
			body:   `{"kind":"bogus","label":"x"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "invalid edge transform",
			method: http.MethodPost,
			path:   "/api/flows/" + flowID + "/edges",
			body:   `{"from_node_id":"a","to_node_id":"b","transform":"bogus"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "missing edge endpoint",
			method: http.MethodPost,
			path:   "/api/flows/" + flowID + "/edges",
			body:   `{"from_node_id":"missing-node","to_node_id":"` + nodeID + `","transform":"passthrough"}`,
			status: http.StatusNotFound,
		},
		{
			name:   "cross-flow edge endpoint",
			method: http.MethodPost,
			path:   "/api/flows/" + flowID + "/edges",
			body:   `{"from_node_id":"` + nodeID + `","to_node_id":"` + otherNodeID + `","transform":"passthrough"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "unknown subpath",
			method: http.MethodGet,
			path:   "/api/flows/" + flowID + "/nope",
			status: http.StatusNotFound,
		},
		{
			name:   "collection method not allowed",
			method: http.MethodPut,
			path:   "/api/flows",
			status: http.StatusMethodNotAllowed,
		},
		{
			name:   "show missing flow",
			method: http.MethodGet,
			path:   "/api/flows/missing-flow",
			status: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := serveFlowContract(t, handler, tt.method, tt.path, tt.body)
			assertFlowErrorContract(t, rr, tt.status)
		})
	}
}

func newFlowContractHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	return FlowHandler(cfg, zerolog.Nop())
}

func serveFlowContract(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func createFlowContract(t *testing.T, handler http.Handler, name string) string {
	t.Helper()
	rr := serveFlowContract(t, handler, http.MethodPost, "/api/flows", `{"name":"`+name+`","workspace":"contract-ws"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status=%d want %d body=%s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	flow := flowContractMap(t, decodeResponseBody(t, rr), "flow")
	return flowContractString(t, flow, "id")
}

func addFlowNodeContract(t *testing.T, handler http.Handler, flowID, label string) string {
	t.Helper()
	rr := serveFlowContract(t, handler, http.MethodPost, "/api/flows/"+flowID+"/nodes", `{
		"kind":"skill",
		"label":"`+label+`",
		"config":{"skill":"test"}
	}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("node status=%d want %d body=%s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	node := flowContractMap(t, decodeResponseBody(t, rr), "node")
	if got := flowContractString(t, node, "flow_id"); got != flowID {
		t.Fatalf("node flow_id=%q want %q", got, flowID)
	}
	if got := flowContractString(t, node, "kind"); got != "skill" {
		t.Fatalf("node kind=%q want skill", got)
	}
	return flowContractString(t, node, "id")
}

func assertFlowErrorContract(t *testing.T, rr *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("status=%d want %d body=%s", rr.Code, status, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if got := flowContractString(t, body, "status"); got != "error" {
		t.Fatalf("status field=%q want error", got)
	}
	errBody := flowContractMap(t, body, "error")
	wantCode := "http_" + strconv.Itoa(status)
	if text := http.StatusText(status); text != "" {
		wantCode += "_" + strings.ToLower(strings.ReplaceAll(text, " ", "_"))
	}
	if got := flowContractString(t, errBody, "code"); got != wantCode {
		t.Fatalf("error.code=%q want %q", got, wantCode)
	}
	if got := strings.TrimSpace(flowContractString(t, errBody, "message")); got == "" {
		t.Fatal("error.message is empty")
	}
}

func flowContractMap(t *testing.T, body map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := body[key].(map[string]any)
	if !ok {
		t.Fatalf("%s type=%T want map[string]any in %#v", key, body[key], body)
	}
	return value
}

func flowContractString(t *testing.T, body map[string]any, key string) string {
	t.Helper()
	value, ok := body[key].(string)
	if !ok {
		t.Fatalf("%s type=%T want string in %#v", key, body[key], body)
	}
	return value
}
