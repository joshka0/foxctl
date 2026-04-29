package runtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidSchedulerConfig marks malformed scheduler configuration.
	ErrInvalidSchedulerConfig = errors.New("rlm runtime: invalid scheduler config")
	// ErrSchedulerClosed marks operations against a stopped scheduler.
	ErrSchedulerClosed = errors.New("rlm runtime: scheduler closed")
	// ErrInvalidQueryRequest marks malformed async query requests.
	ErrInvalidQueryRequest = errors.New("rlm runtime: invalid query request")
	// ErrInvalidWaitRequest marks malformed wait requests.
	ErrInvalidWaitRequest = errors.New("rlm runtime: invalid wait request")
	// ErrNodeOwnership marks wait attempts across non-child nodes.
	ErrNodeOwnership = errors.New("rlm runtime: node ownership violation")
)

const (
	defaultSchedulerWorkers = 1
	schedulerQueueFactor    = 4
)

// SchedulerConfig wires scheduler dependencies and runtime bounds.
type SchedulerConfig struct {
	Store         NodeStore
	Budget        *Budget
	BudgetConfig  *BudgetConfig
	Backend       NodeBackend
	Recorder      *Recorder
	RunID         string
	RootNodeID    string
	OutputRoot    string
	MaxWorkers    int
	MaxConcurrent int
	MaxChildren   int
	RootContext   context.Context
}

// NodeHandle is the immediate result of one Submit call.
type NodeHandle struct {
	RunID        string     `json:"run_id"`
	NodeID       string     `json:"node_id"`
	ParentNodeID string     `json:"parent_node_id"`
	Depth        int        `json:"depth"`
	Status       NodeStatus `json:"status"`
}

// WaitRequest controls fan-in behavior for one parent node.
type WaitRequest struct {
	ChildNodeIDs []string      `json:"child_node_ids,omitempty"`
	MinComplete  int           `json:"min_complete,omitempty"`
	Timeout      time.Duration `json:"timeout,omitempty"`
}

// WaitResult groups child nodes by terminal vs pending status.
type WaitResult struct {
	Completed []Node `json:"completed,omitempty"`
	Failed    []Node `json:"failed,omitempty"` // Includes failed and canceled nodes.
	Pending   []Node `json:"pending,omitempty"`
}

// Scheduler executes async child nodes with a bounded worker pool.
type Scheduler struct {
	store      NodeStore
	budget     *Budget
	backend    NodeBackend
	recorder   *Recorder
	runID      string
	rootNodeID string
	outputRoot string

	maxChildren int
	queue       chan *scheduledTask

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu               sync.Mutex
	tasks            map[string]*scheduledTask
	nextChildOrdinal map[string]int
	waitCh           chan struct{}

	rootCancelOnce sync.Once
}

type scheduledTask struct {
	nodeID   string
	parentID string
	input    NodeInput

	cancelMu sync.Mutex
	cancel   context.CancelFunc

	done     chan struct{}
	doneOnce sync.Once
}

// NewScheduler starts a bounded worker scheduler.
func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: missing store", ErrInvalidSchedulerConfig)
	}
	if cfg.Backend == nil {
		return nil, ErrMissingNodeBackend
	}
	runID := strings.TrimSpace(cfg.RunID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run id is required", ErrInvalidSchedulerConfig)
	}
	rootNodeID := strings.TrimSpace(cfg.RootNodeID)
	if rootNodeID == "" {
		return nil, fmt.Errorf("%w: root node id is required", ErrInvalidSchedulerConfig)
	}

	workers := cfg.MaxWorkers
	if workers <= 0 {
		workers = cfg.MaxConcurrent
	}
	if workers <= 0 {
		workers = defaultSchedulerWorkers
	}
	if cfg.MaxConcurrent > 0 && cfg.MaxConcurrent < workers {
		workers = cfg.MaxConcurrent
	}
	queueSize := workers * schedulerQueueFactor
	if queueSize < workers {
		queueSize = workers
	}

	budget := cfg.Budget
	if budget == nil && cfg.BudgetConfig != nil {
		created, err := NewBudget(*cfg.BudgetConfig)
		if err != nil {
			return nil, err
		}
		budget = created
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler{
		store:            cfg.Store,
		budget:           budget,
		backend:          cfg.Backend,
		recorder:         cfg.Recorder,
		runID:            runID,
		rootNodeID:       rootNodeID,
		outputRoot:       strings.TrimSpace(cfg.OutputRoot),
		maxChildren:      cfg.MaxChildren,
		queue:            make(chan *scheduledTask, queueSize),
		ctx:              ctx,
		cancel:           cancel,
		tasks:            make(map[string]*scheduledTask),
		nextChildOrdinal: make(map[string]int),
		waitCh:           make(chan struct{}, 1),
	}

	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	if cfg.RootContext != nil {
		go func(rootCtx context.Context) {
			select {
			case <-rootCtx.Done():
				s.cancelRoot(rootCtx.Err())
			case <-s.ctx.Done():
			}
		}(cfg.RootContext)
	}

	return s, nil
}

// Close stops workers and waits for exit.
func (s *Scheduler) Close() error {
	if s == nil {
		return nil
	}
	s.cancel()
	s.cancelOutstanding(ErrSchedulerClosed)
	s.wg.Wait()
	return nil
}

// Submit enqueues one child query for asynchronous execution.
func (s *Scheduler) Submit(ctx context.Context, parentID string, request QueryRequest) (NodeHandle, error) {
	if s == nil {
		return NodeHandle{}, fmt.Errorf("%w: nil scheduler", ErrInvalidSchedulerConfig)
	}
	if err := checkContext(ctx); err != nil {
		return NodeHandle{}, err
	}
	if s.ctx.Err() != nil {
		return NodeHandle{}, ErrSchedulerClosed
	}

	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return NodeHandle{}, fmt.Errorf("%w: parent node id is required", ErrInvalidQueryRequest)
	}
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Prompt == "" {
		return NodeHandle{}, fmt.Errorf("%w: prompt is required", ErrInvalidQueryRequest)
	}

	parent, err := s.store.GetNode(ctx, s.runID, parentID)
	if err != nil {
		return NodeHandle{}, err
	}

	if s.maxChildren > 0 {
		children, err := s.store.ListChildren(ctx, s.runID, parentID)
		if err != nil {
			return NodeHandle{}, err
		}
		used := len(children)
		if used+1 > s.maxChildren {
			return NodeHandle{}, LimitExceededError{
				Limit:     LimitChildren,
				Used:      used,
				Attempted: used + 1,
				Max:       s.maxChildren,
			}
		}
	}

	childDepth := parent.Depth + 1
	if s.budget != nil {
		if err := s.budget.CheckDepth(childDepth); err != nil {
			return NodeHandle{}, err
		}
		if err := s.budget.ConsumeChild(ctx); err != nil {
			return NodeHandle{}, err
		}
		if err := s.budget.ConsumeNode(ctx); err != nil {
			return NodeHandle{}, err
		}
	}

	childID, err := s.nextChildID(ctx, parentID)
	if err != nil {
		return NodeHandle{}, err
	}

	layout, err := PlanNodeLayout(s.outputRoot, s.runID, childID)
	if err != nil {
		return NodeHandle{}, err
	}

	metadata := cloneMapAny(request.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["output_root"] = layout.OutputRoot
	metadata["node_dir"] = layout.NodeDir
	metadata["result_path"] = layout.ResultJSON

	node := Node{
		RunID:        s.runID,
		ID:           childID,
		ParentNodeID: parentID,
		Depth:        childDepth,
		Status:       NodeStatusQueued,
		Prompt:       request.Prompt,
		Metadata:     metadata,
	}
	if _, err := s.store.CreateNode(ctx, node); err != nil {
		return NodeHandle{}, err
	}
	s.recordNodeQueued(node)

	task := &scheduledTask{
		nodeID:   childID,
		parentID: parentID,
		input: NodeInput{
			Prompt:           request.Prompt,
			MaxIterations:    request.MaxIterations,
			SummaryMaxChars:  request.SummaryMaxChars,
			RequiredSubcalls: request.RequiredSubcalls,
			Metadata:         cloneMapAny(request.Metadata),
		},
		done: make(chan struct{}),
	}
	s.registerTask(task)

	select {
	case s.queue <- task:
		return nodeToHandle(node), nil
	case <-ctx.Done():
		s.markNodeCanceled(childID, ctx.Err())
		s.finishTask(childID)
		return NodeHandle{}, ctx.Err()
	case <-s.ctx.Done():
		s.markNodeCanceled(childID, ErrSchedulerClosed)
		s.finishTask(childID)
		return NodeHandle{}, ErrSchedulerClosed
	}
}

// Wait gathers direct child node states for a parent, optionally blocking.
func (s *Scheduler) Wait(ctx context.Context, parentID string, request WaitRequest) (WaitResult, error) {
	if s == nil {
		return WaitResult{}, fmt.Errorf("%w: nil scheduler", ErrInvalidSchedulerConfig)
	}
	if err := checkContext(ctx); err != nil {
		return WaitResult{}, err
	}

	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return WaitResult{}, fmt.Errorf("%w: parent node id is required", ErrInvalidWaitRequest)
	}
	if request.MinComplete < 0 {
		return WaitResult{}, fmt.Errorf("%w: min_complete must be >= 0", ErrInvalidWaitRequest)
	}
	if request.Timeout < 0 {
		return WaitResult{}, fmt.Errorf("%w: timeout must be >= 0", ErrInvalidWaitRequest)
	}

	if _, err := s.store.GetNode(ctx, s.runID, parentID); err != nil {
		return WaitResult{}, err
	}

	nodeIDs, err := s.resolveWaitNodeIDs(ctx, parentID, request.ChildNodeIDs)
	if err != nil {
		return WaitResult{}, err
	}
	if len(nodeIDs) == 0 {
		return WaitResult{}, nil
	}
	s.recordWaitStarted(parentID, nodeIDs, request)

	var deadline time.Time
	if request.Timeout > 0 {
		deadline = time.Now().UTC().Add(request.Timeout)
	}

	for {
		result, terminal, err := s.collectWaitResult(ctx, parentID, nodeIDs)
		if err != nil {
			return WaitResult{}, err
		}

		if request.MinComplete > 0 {
			if terminal >= request.MinComplete || len(result.Pending) == 0 {
				s.recordWaitCompleted(parentID, nodeIDs, request, result)
				return result, nil
			}
		} else if len(result.Pending) == 0 {
			s.recordWaitCompleted(parentID, nodeIDs, request, result)
			return result, nil
		}

		if !deadline.IsZero() && !time.Now().UTC().Before(deadline) {
			s.recordWaitCompleted(parentID, nodeIDs, request, result)
			return result, nil
		}

		waitCtx := ctx
		var cancel context.CancelFunc
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				s.recordWaitCompleted(parentID, nodeIDs, request, result)
				return result, nil
			}
			waitCtx, cancel = context.WithTimeout(ctx, remaining)
		}

		select {
		case <-waitCtx.Done():
			if cancel != nil {
				cancel()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) && !deadline.IsZero() {
				s.recordWaitCompleted(parentID, nodeIDs, request, result)
				return result, nil
			}
			return WaitResult{}, waitCtx.Err()
		case <-s.waitCh:
			if cancel != nil {
				cancel()
			}
		case <-s.ctx.Done():
			if cancel != nil {
				cancel()
			}
			s.cancelOutstanding(ErrSchedulerClosed)
			s.cancelWaitPendingChildren(ctx, parentID, nodeIDs, ErrSchedulerClosed)
			result, _, err := s.collectWaitResult(ctx, parentID, nodeIDs)
			if err != nil {
				return WaitResult{}, err
			}
			s.recordWaitCompleted(parentID, nodeIDs, request, result)
			return result, nil
		}
	}
}

func (s *Scheduler) worker() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case task := <-s.queue:
			if task == nil {
				continue
			}
			s.processTask(task)
		}
	}
}

func (s *Scheduler) processTask(task *scheduledTask) {
	if s.ctx.Err() != nil {
		s.markNodeCanceled(task.nodeID, ErrSchedulerClosed)
		s.finishTask(task.nodeID)
		return
	}

	node, err := s.store.GetNode(context.Background(), s.runID, task.nodeID)
	if err != nil {
		s.finishTask(task.nodeID)
		return
	}
	if node.Status.IsTerminal() {
		s.finishTask(task.nodeID)
		return
	}

	node, err = s.store.UpdateNodeStatus(context.Background(), s.runID, task.nodeID, NodeStatusRunning)
	if err != nil {
		if !errors.Is(err, ErrInvalidNodeStatusTransition) {
			s.failNode(task.nodeID, "status_update_failed", err)
		}
		s.finishTask(task.nodeID)
		return
	}
	s.recordNodeStarted(node)

	var concurrentLease *ConcurrentLease
	if s.budget != nil {
		lease, err := s.budget.ReserveConcurrent(s.ctx)
		if err != nil {
			s.failNode(task.nodeID, "budget_concurrent", err)
			s.finishTask(task.nodeID)
			return
		}
		concurrentLease = lease
		defer func() {
			_ = concurrentLease.Release()
		}()
	}

	runCtx, cancel := context.WithCancel(s.ctx)
	task.setCancel(cancel)
	defer func() {
		task.clearCancel()
		cancel()
	}()

	result, err := s.backend.RunNode(runCtx, node, task.input)
	if err != nil {
		metadata := map[string]any{}
		if task.input.RequiredSubcalls > 0 {
			metadata["required_subcalls"] = task.input.RequiredSubcalls
		}
		if errors.Is(err, context.Canceled) || errors.Is(runCtx.Err(), context.Canceled) {
			s.markNodeCanceledWithMetadata(task.nodeID, err, metadata)
		} else if errors.Is(err, ErrRequiredSubcallsNotSatisfied) {
			s.failNodeWithMetadata(task.nodeID, "required_subcalls", err, metadata)
		} else {
			s.failNodeWithMetadata(task.nodeID, "backend_error", err, metadata)
		}
		s.finishTask(task.nodeID)
		return
	}

	if strings.TrimSpace(result.Summary) == "" {
		result.Summary = strings.TrimSpace(result.Answer)
	}
	result.Status = NodeStatusCompleted
	completed, err := s.store.SetNodeResult(context.Background(), s.runID, task.nodeID, result)
	if err != nil {
		if !errors.Is(err, ErrInvalidNodeStatusTransition) {
			s.failNode(task.nodeID, "store_set_result_failed", err)
		}
	} else {
		s.recordNodeCompleted(completed)
	}

	s.finishTask(task.nodeID)
}

func (s *Scheduler) registerTask(task *scheduledTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.nodeID] = task
}

func (s *Scheduler) finishTask(nodeID string) {
	s.mu.Lock()
	task, exists := s.tasks[nodeID]
	if exists {
		delete(s.tasks, nodeID)
	}
	s.mu.Unlock()

	if exists {
		task.markDone()
	}
	s.notifyWaiters()
}

func (s *Scheduler) notifyWaiters() {
	select {
	case s.waitCh <- struct{}{}:
	default:
	}
}

func (s *Scheduler) nextChildID(ctx context.Context, parentID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ordinal, ok := s.nextChildOrdinal[parentID]
	if !ok {
		children, err := s.store.ListChildren(ctx, s.runID, parentID)
		if err != nil {
			return "", err
		}
		ordinal = maxChildOrdinal(parentID, children)
	}
	ordinal++
	s.nextChildOrdinal[parentID] = ordinal
	return fmt.Sprintf("%s.%d", parentID, ordinal), nil
}

func maxChildOrdinal(parentID string, children []Node) int {
	maxOrdinal := 0
	prefix := parentID + "."
	for _, child := range children {
		if !strings.HasPrefix(child.ID, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(child.ID, prefix)
		if strings.ContainsRune(suffix, '.') {
			continue
		}
		n, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		if n > maxOrdinal {
			maxOrdinal = n
		}
	}
	if maxOrdinal == 0 {
		maxOrdinal = len(children)
	}
	return maxOrdinal
}

func (s *Scheduler) resolveWaitNodeIDs(ctx context.Context, parentID string, requested []string) ([]string, error) {
	if len(requested) == 0 {
		children, err := s.store.ListChildren(ctx, s.runID, parentID)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(children))
		for _, child := range children {
			out = append(out, child.ID)
		}
		return out, nil
	}

	out := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, raw := range requested {
		nodeID := strings.TrimSpace(raw)
		if nodeID == "" {
			return nil, fmt.Errorf("%w: child node id is required", ErrInvalidWaitRequest)
		}
		if _, exists := seen[nodeID]; exists {
			continue
		}
		child, err := s.store.GetNode(ctx, s.runID, nodeID)
		if err != nil {
			return nil, err
		}
		if child.ParentNodeID != parentID {
			return nil, fmt.Errorf("%w: node=%q is not a direct child of %q", ErrNodeOwnership, nodeID, parentID)
		}
		seen[nodeID] = struct{}{}
		out = append(out, nodeID)
	}
	return out, nil
}

func (s *Scheduler) collectWaitResult(ctx context.Context, parentID string, nodeIDs []string) (WaitResult, int, error) {
	out := WaitResult{
		Completed: make([]Node, 0, len(nodeIDs)),
		Failed:    make([]Node, 0),
		Pending:   make([]Node, 0),
	}
	terminal := 0

	for _, nodeID := range nodeIDs {
		if err := checkContext(ctx); err != nil {
			return WaitResult{}, 0, err
		}
		node, err := s.store.GetNode(ctx, s.runID, nodeID)
		if err != nil {
			return WaitResult{}, 0, err
		}
		if node.ParentNodeID != parentID {
			return WaitResult{}, 0, fmt.Errorf("%w: node=%q is not a direct child of %q", ErrNodeOwnership, nodeID, parentID)
		}
		switch node.Status {
		case NodeStatusCompleted:
			out.Completed = append(out.Completed, node)
			terminal++
		case NodeStatusFailed, NodeStatusCanceled:
			out.Failed = append(out.Failed, node)
			terminal++
		default:
			out.Pending = append(out.Pending, node)
		}
	}

	return out, terminal, nil
}

func (s *Scheduler) recordNodeQueued(node Node) {
	s.recordNode(EventTypeNodeQueued, node, "")
}

func (s *Scheduler) recordNodeStarted(node Node) {
	s.recordNode(EventTypeNodeStarted, node, "")
}

func (s *Scheduler) recordNodeCompleted(node Node) {
	s.recordNode(EventTypeNodeCompleted, node, "")
}

func (s *Scheduler) recordNodeFailed(node Node, message string) {
	s.recordNode(EventTypeNodeFailed, node, message)
}

func (s *Scheduler) recordNodeCanceled(node Node, message string) {
	s.recordNode(EventTypeNodeCanceled, node, message)
}

func (s *Scheduler) recordNode(eventType EventType, node Node, message string) {
	if s == nil || s.recorder == nil || strings.TrimSpace(node.ID) == "" {
		return
	}
	event := NodeEvent{
		RunID:           node.RunID,
		NodeID:          node.ID,
		ParentNodeID:    node.ParentNodeID,
		Depth:           node.Depth,
		Status:          node.Status,
		OutputNamespace: stringFromMapAny(node.Metadata, "node_dir"),
		Message:         strings.TrimSpace(message),
	}
	if node.Result != nil {
		event.RequiredSubcalls = intFromAny(node.Result.Metadata["required_subcalls"])
		event.RequiredSubcallAttempts = intFromAny(node.Result.Metadata["required_subcall_attempts"])
		event.RecursiveSubcallsUsed = intFromAny(node.Result.Metadata["recursive_subcalls_used"])
	}
	switch eventType {
	case EventTypeNodeQueued:
		s.recorder.RecordNodeQueued(event)
	case EventTypeNodeStarted:
		s.recorder.RecordNodeStarted(event)
	case EventTypeNodeCompleted:
		s.recorder.RecordNodeCompleted(event)
	case EventTypeNodeFailed:
		s.recorder.RecordNodeFailed(event)
	case EventTypeNodeCanceled:
		s.recorder.RecordNodeCanceled(event)
	}
}

func (s *Scheduler) recordWaitStarted(parentID string, nodeIDs []string, request WaitRequest) {
	if s == nil || s.recorder == nil {
		return
	}
	s.recorder.RecordNodeWaitStarted(waitEventFromRequest(s.runID, parentID, nodeIDs, request, WaitResult{}))
}

func (s *Scheduler) recordWaitCompleted(parentID string, nodeIDs []string, request WaitRequest, result WaitResult) {
	if s == nil || s.recorder == nil {
		return
	}
	s.recorder.RecordNodeWaitCompleted(waitEventFromRequest(s.runID, parentID, nodeIDs, request, result))
}

func waitEventFromRequest(runID string, parentID string, nodeIDs []string, request WaitRequest, result WaitResult) WaitEvent {
	return WaitEvent{
		RunID:        runID,
		ParentNodeID: parentID,
		ChildIDs:     append([]string(nil), nodeIDs...),
		Completed:    len(result.Completed),
		Failed:       len(result.Failed),
		Pending:      len(result.Pending),
		MinComplete:  request.MinComplete,
		TimeoutMS:    request.Timeout.Milliseconds(),
	}
}

func stringFromMapAny(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func (s *Scheduler) failNode(nodeID, code string, failure error) {
	s.failNodeWithMetadata(nodeID, code, failure, nil)
}

func (s *Scheduler) failNodeWithMetadata(nodeID, code string, failure error, metadata map[string]any) {
	message := ""
	if failure != nil {
		message = strings.TrimSpace(failure.Error())
	}
	result := NodeResult{
		Status:       NodeStatusFailed,
		Summary:      message,
		ErrorCode:    strings.TrimSpace(code),
		ErrorMessage: message,
		Metadata:     cloneMapAny(metadata),
	}
	node, _ := s.store.SetNodeResult(context.Background(), s.runID, nodeID, result)
	s.recordNodeFailed(node, message)
	s.notifyWaiters()
}

func (s *Scheduler) markNodeCanceled(nodeID string, cause error) {
	s.markNodeCanceledWithMetadata(nodeID, cause, nil)
}

func (s *Scheduler) markNodeCanceledWithMetadata(nodeID string, cause error, metadata map[string]any) {
	message := "canceled"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = strings.TrimSpace(cause.Error())
	}
	result := NodeResult{
		Status:       NodeStatusCanceled,
		Summary:      message,
		ErrorCode:    "canceled",
		ErrorMessage: message,
		Metadata:     cloneMapAny(metadata),
	}
	node, _ := s.store.SetNodeResult(context.Background(), s.runID, nodeID, result)
	s.recordNodeCanceled(node, message)
	s.notifyWaiters()
}

func (s *Scheduler) cancelRoot(cause error) {
	s.rootCancelOnce.Do(func() {
		s.cancel()
		s.cancelOutstanding(cause)
	})
}

func (s *Scheduler) cancelOutstanding(cause error) {
	s.mu.Lock()
	tasks := make([]*scheduledTask, 0, len(s.tasks))
	for nodeID, task := range s.tasks {
		tasks = append(tasks, task)
		delete(s.tasks, nodeID)
	}
	s.mu.Unlock()

	for _, task := range tasks {
		task.cancelRun()
		s.markNodeCanceled(task.nodeID, cause)
		task.markDone()
	}
	s.notifyWaiters()
}

func (s *Scheduler) cancelWaitPendingChildren(ctx context.Context, parentID string, nodeIDs []string, cause error) {
	for _, nodeID := range nodeIDs {
		node, err := s.store.GetNode(ctx, s.runID, nodeID)
		if err != nil || node.ParentNodeID != parentID || node.Status.IsTerminal() {
			continue
		}
		s.markNodeCanceled(nodeID, cause)
	}
}

func nodeToHandle(node Node) NodeHandle {
	return NodeHandle{
		RunID:        node.RunID,
		NodeID:       node.ID,
		ParentNodeID: node.ParentNodeID,
		Depth:        node.Depth,
		Status:       node.Status,
	}
}

func (t *scheduledTask) setCancel(cancel context.CancelFunc) {
	t.cancelMu.Lock()
	defer t.cancelMu.Unlock()
	t.cancel = cancel
}

func (t *scheduledTask) clearCancel() {
	t.cancelMu.Lock()
	defer t.cancelMu.Unlock()
	t.cancel = nil
}

func (t *scheduledTask) cancelRun() {
	t.cancelMu.Lock()
	cancel := t.cancel
	t.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (t *scheduledTask) markDone() {
	t.doneOnce.Do(func() {
		close(t.done)
	})
}
