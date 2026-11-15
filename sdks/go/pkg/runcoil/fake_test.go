package runcoil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
	"github.com/runcoil/runcoil/proto/gen/go/runcoil/v1/runcoilv1connect"
)

const (
	testAPIKey  = "rc_api_test"
	testSession = "session-1"
)

// set changes the fake's behaviour under its lock.
func (f *fakeServer) set(change func(f *fakeServer)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	change(f)
}

// fakeServer is an in-process WorkerService. Each Listen gets the next stream script from
// streams, sends its messages, and then waits until the script ends it or the client goes.
type fakeServer struct {
	runcoilv1connect.UnimplementedWorkerServiceHandler

	t       *testing.T
	streams chan *fakeStream

	mu         sync.Mutex
	registers  []string // the bearer token of each Register
	regReqs    []*runcoilv1.RegisterRequest
	listens    []*runcoilv1.ListenRequest
	listenAuth []string
	steps      []*runcoilv1.CompleteStepRequest
	runs       []*runcoilv1.CompleteRunRequest
	heartbeats int
	// deregisters is the reason of each Deregister, and runsAtDeregister how many Runs
	// had completed by then.
	deregisters      []string
	runsAtDeregister []int
	sessions         int
	stepAnswer       func(*runcoilv1.CompleteStepRequest) (*runcoilv1.CompleteStepResponse, error)
	runAnswer        func(context.Context) error
	// runRefuse refuses a CompleteRun before runAnswer, when it returns an error.
	runRefuse  func(*runcoilv1.CompleteRunRequest) error
	beatAnswer func() error
	regAnswer  error

	// hbInterval is the heartbeat interval Register returns.
	hbInterval time.Duration

	completed chan *runcoilv1.CompleteRunRequest
}

// fakeStream is what the fake does with one Listen.
type fakeStream struct {
	refuse error                          // the Listen fails with this error
	send   chan *runcoilv1.ListenResponse // messages to send; closing it ends the stream
}

func newFakeStream() *fakeStream {
	return &fakeStream{send: make(chan *runcoilv1.ListenResponse, 8)}
}

func newFakeServer(t *testing.T) (*fakeServer, string) {
	t.Helper()

	f := &fakeServer{
		t:          t,
		streams:    make(chan *fakeStream, 8),
		completed:  make(chan *runcoilv1.CompleteRunRequest, 8),
		hbInterval: time.Hour,
	}

	server := connect.NewServer()
	runcoilv1connect.RegisterWorkerServiceHandler(server, f)

	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	srv := &http.Server{Handler: mux, Protocols: protocols, ReadHeaderTimeout: time.Second}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	return f, "http://" + listener.Addr().String()
}

func bearer(ctx context.Context) string {
	info, _ := connect.CallInfoForServerContext(ctx)
	return strings.TrimPrefix(info.RequestHeader().Get("Authorization"), "Bearer ")
}

// Register opens session-1, session-2, ... in turn.
func (f *fakeServer) Register(ctx context.Context, req *runcoilv1.RegisterRequest) (*runcoilv1.RegisterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.registers = append(f.registers, bearer(ctx))
	f.regReqs = append(f.regReqs, req)
	if f.regAnswer != nil {
		return nil, f.regAnswer
	}

	f.sessions++

	return &runcoilv1.RegisterResponse{
		WorkerId:          "worker-1",
		SessionToken:      fmt.Sprintf("session-%d", f.sessions),
		HeartbeatInterval: durationpb.New(f.hbInterval),
	}, nil
}

func (f *fakeServer) Listen(ctx context.Context, req *runcoilv1.ListenRequest, out runcoilv1connect.WorkerServiceListenServerStream) error {
	f.mu.Lock()
	f.listens = append(f.listens, req)
	f.listenAuth = append(f.listenAuth, bearer(ctx))
	f.mu.Unlock()

	var st *fakeStream
	select {
	case st = <-f.streams:
	case <-ctx.Done():
		return nil
	}

	if st.refuse != nil {
		return st.refuse
	}

	for {
		select {
		case msg, ok := <-st.send:
			if !ok {
				return nil
			}

			if err := out.Send(msg); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (f *fakeServer) Deregister(_ context.Context, req *runcoilv1.DeregisterRequest) (*runcoilv1.DeregisterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deregisters = append(f.deregisters, req.GetReason())
	f.runsAtDeregister = append(f.runsAtDeregister, len(f.runs))

	return &runcoilv1.DeregisterResponse{}, nil
}

func (f *fakeServer) Heartbeat(context.Context, *runcoilv1.HeartbeatRequest) (*runcoilv1.HeartbeatResponse, error) {
	f.mu.Lock()
	f.heartbeats++
	answer := f.beatAnswer
	f.mu.Unlock()

	if answer != nil {
		if err := answer(); err != nil {
			return nil, err
		}
	}

	return &runcoilv1.HeartbeatResponse{}, nil
}

func (f *fakeServer) CompleteStep(_ context.Context, req *runcoilv1.CompleteStepRequest) (*runcoilv1.CompleteStepResponse, error) {
	f.mu.Lock()
	f.steps = append(f.steps, req)
	answer := f.stepAnswer
	f.mu.Unlock()

	if answer != nil {
		return answer(req)
	}

	return &runcoilv1.CompleteStepResponse{Result: &runcoilv1.CompleteStepResponse_Record{Record: storedRecord(req)}}, nil
}

func (f *fakeServer) CompleteRun(ctx context.Context, req *runcoilv1.CompleteRunRequest) (*runcoilv1.CompleteRunResponse, error) {
	f.mu.Lock()
	f.runs = append(f.runs, req)
	answer, refuse := f.runAnswer, f.runRefuse
	f.mu.Unlock()

	if refuse != nil {
		if err := refuse(req); err != nil {
			return nil, err
		}
	}

	if answer != nil {
		if err := answer(ctx); err != nil {
			return nil, err
		}
	}

	f.completed <- req

	return &runcoilv1.CompleteRunResponse{}, nil
}

// storedRecord is the record the server stores for a Step it had none of.
func storedRecord(req *runcoilv1.CompleteStepRequest) *runcoilv1.StepRecord {
	rec := &runcoilv1.StepRecord{StepId: req.GetStepId(), Kind: runcoilv1.StepRecord_KIND_DO}
	if e := req.GetError(); e != nil {
		rec.Result = &runcoilv1.StepRecord_Failure{Failure: &runcoilv1.Failure{
			Code:    runcoilv1.Failure_CODE_ERROR,
			Message: e.GetMessage(),
			StepId:  req.GetStepId(),
		}}

		return rec
	}

	rec.Result = &runcoilv1.StepRecord_Output{Output: req.GetOutput()}

	return rec
}

func (f *fakeServer) snapshot() (registers []string, listens []*runcoilv1.ListenRequest, listenAuth []string, steps []*runcoilv1.CompleteStepRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.registers...),
		append([]*runcoilv1.ListenRequest(nil), f.listens...),
		append([]string(nil), f.listenAuth...),
		append([]*runcoilv1.CompleteStepRequest(nil), f.steps...)
}

// waitRun returns the next CompleteRun the fake received.
func (f *fakeServer) waitRun(t *testing.T) *runcoilv1.CompleteRunRequest {
	t.Helper()

	select {
	case req := <-f.completed:
		return req
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no CompleteRun")
		return nil
	}
}

// noRun checks that no CompleteRun arrives for a while.
func (f *fakeServer) noRun(t *testing.T) {
	t.Helper()

	select {
	case req := <-f.completed:
		require.FailNow(t, "unexpected CompleteRun", "%v", req)
	case <-time.After(300 * time.Millisecond):
	}
}

func assignMsg(runID string, epoch int64, task, input string, steps ...*runcoilv1.StepRecord) *runcoilv1.ListenResponse {
	return &runcoilv1.ListenResponse{Kind: &runcoilv1.ListenResponse_Assign{Assign: &runcoilv1.Assign{
		Lease:    &runcoilv1.Lease{RunId: runID, Epoch: epoch},
		TaskName: task,
		Input:    []byte(input),
		Steps:    steps,
	}}}
}

func leaseLostMsg(runID string, epoch int64) *runcoilv1.ListenResponse {
	return &runcoilv1.ListenResponse{Kind: &runcoilv1.ListenResponse_Cancel{Cancel: &runcoilv1.Cancel{
		Lease:  &runcoilv1.Lease{RunId: runID, Epoch: epoch},
		Reason: runcoilv1.Cancel_REASON_LEASE_LOST,
	}}}
}

// codedError is an error as the server sends it: a Connect code with a Runcoil code.
func codedError(t *testing.T, code connect.Code, errCode runcoilv1.ErrorCode, msg string) error {
	t.Helper()

	detail, err := connectproto.NewErrorDetail(&runcoilv1.ErrorDetail{Code: errCode})
	require.NoError(t, err)

	return connect.NewError(code, msg).WithDetail(detail)
}

// startWorker starts a worker of the app "shop" on the fake, with the given Tasks, and
// shuts it down when the test ends.
func startWorker(t *testing.T, url string, tasks map[string]Handler) *Worker {
	t.Helper()

	client, err := NewClient(URL(url), APIKey(testAPIKey))
	require.NoError(t, err)

	w, err := NewWorker(client, "shop", Slots(2))
	require.NoError(t, err)

	for name, h := range tasks {
		require.NoError(t, w.NewTask(name, h))
	}

	require.NoError(t, w.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = w.Shutdown(ctx)
	})

	return w
}

// sentRuns returns every CompleteRun the fake received, refused ones included.
func (f *fakeServer) sentRuns() []*runcoilv1.CompleteRunRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]*runcoilv1.CompleteRunRequest(nil), f.runs...)
}
