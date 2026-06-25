package plugin

import (
	"context"
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/plugin/proto"
)

// engineServer adapts an in-tree engine.Driver (+ capabilities) to the Engine gRPC
// service. It runs inside the plugin process (wired by the SDK's Serve). Optional
// capabilities are dispatched via type assertion and advertised by Capabilities so
// the host only calls what the driver actually implements.
type engineServer struct {
	proto.UnimplementedEngineServer
	drv engine.Driver
}

func newEngineServer(drv engine.Driver) *engineServer { return &engineServer{drv: drv} }

func (s *engineServer) Type(context.Context, *proto.Empty) (*proto.TypeResponse, error) {
	return &proto.TypeResponse{Type: s.drv.Type()}, nil
}

// capability ids advertised in the handshake; pluginDriver gates its RPCs on these.
const (
	capConverger   = "converger"
	capInventory   = "inventory"
	capPruner      = "pruner"
	capAttributer  = "attributer"
	capEnv         = "env"
	capBackendURL  = "backend_url"
	capLifecycle   = "lifecycle"
	capHooked      = "hooked"
	capHealth      = "health"
	capRestartable = "restartable"
	capPortBinder  = "port_binder"
	capSpawner     = "spawner"
)

func (s *engineServer) Capabilities(context.Context, *proto.Empty) (*proto.CapabilitiesResponse, error) {
	var caps []string
	add := func(ok bool, id string) {
		if ok {
			caps = append(caps, id)
		}
	}
	_, conv := s.drv.(engine.Converger)
	add(conv, capConverger)
	_, inv := s.drv.(engine.Inventory)
	add(inv, capInventory)
	_, pr := s.drv.(engine.Pruner)
	add(pr, capPruner)
	_, at := s.drv.(engine.Attributer)
	add(at, capAttributer)
	_, en := s.drv.(engine.EnvProvider)
	add(en, capEnv)
	_, bu := s.drv.(engine.BackendProvider)
	add(bu, capBackendURL)
	_, lc := s.drv.(engine.Lifecycle)
	add(lc, capLifecycle)
	_, hk := s.drv.(engine.Hooked)
	add(hk, capHooked)
	_, he := s.drv.(engine.HealthChecker)
	add(he, capHealth)
	_, rs := s.drv.(engine.Restartable)
	add(rs, capRestartable)
	_, pb := s.drv.(engine.PortBinder)
	add(pb, capPortBinder)
	_, sp := s.drv.(engine.Spawner)
	add(sp, capSpawner)
	return &proto.CapabilitiesResponse{Capabilities: caps}, nil
}

// stripSchema removes the fields core consumes before an engine sees its body: the
// meta-args (count/for_each/depends_on) and the common fields (version/listen). The
// engine-specific remainder is what the driver decodes — identical to in-tree.
var stripSchema = &hcl.BodySchema{Attributes: []hcl.AttributeSchema{
	{Name: "count"}, {Name: "for_each"}, {Name: "depends_on"},
	{Name: "version"}, {Name: "listen"},
}}

// DecodeConfig re-parses the source file the plugin's block came from, isolates the
// block by type+label, strips the fields core owns, reconstructs the eval context
// from the wire variables, and runs the driver's own gohcl decode — then returns
// the config gob-encoded. The plugin's ConfigDecoder is unchanged from in-tree.
func (s *engineServer) DecodeConfig(_ context.Context, req *proto.DecodeRequest) (*proto.DecodeResponse, error) {
	dec, ok := s.drv.(engine.ConfigDecoder)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "engine has no config decoder")
	}
	file, diags := hclparse.NewParser().ParseHCL(req.File, req.BlockLabel+".doze.hcl")
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing config: %s", diags)
	}
	content, _, diags := file.Body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{{Type: req.BlockType, LabelNames: []string{"name"}}},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("reading config blocks: %s", diags)
	}
	var blockBody hcl.Body
	for _, b := range content.Blocks {
		if len(b.Labels) == 1 && b.Labels[0] == req.BlockLabel {
			blockBody = b.Body
			break
		}
	}
	if blockBody == nil {
		return nil, fmt.Errorf("block %s %q not found in source", req.BlockType, req.BlockLabel)
	}
	_, remain, diags := blockBody.PartialContent(stripSchema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("stripping config: %s", diags)
	}
	vars, err := varsFromJSON(req.Variables)
	if err != nil {
		return nil, fmt.Errorf("decoding variables: %w", err)
	}
	ctx := &hcl.EvalContext{Variables: vars}
	spec, err := dec.DecodeConfig(remain, ctx, req.BaseDir)
	if err != nil {
		return nil, err
	}
	b, err := encodeSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("encoding config: %w", err)
	}
	return &proto.DecodeResponse{Spec: b}, nil
}

func (s *engineServer) Resolve(ctx context.Context, req *proto.ResolveRequest) (*proto.ResolveResponse, error) {
	lk := &capturingLocker{in: pinFromProto(req.Locked), inOK: req.Locked != nil}
	tc, err := s.drv.Resolve(ctx, engine.VersionSpec(req.Spec), platformFromProto(req.Platform), lk, stubFetcher{})
	if err != nil {
		return nil, err
	}
	pin := lk.out
	if !lk.recorded {
		pin = engine.Pin{Resolved: tc.Full}
	}
	return &proto.ResolveResponse{Toolchain: toolchainToProto(tc), Pin: pinToProto(pin)}, nil
}

func (s *engineServer) Provision(ctx context.Context, req *proto.ProvisionRequest) (*proto.Empty, error) {
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return &proto.Empty{}, s.drv.Provision(ctx, inst, toolchainFromProto(req.Toolchain))
}

func (s *engineServer) Provisioned(_ context.Context, req *proto.ProvisionedRequest) (*proto.ProvisionedResponse, error) {
	return &proto.ProvisionedResponse{Provisioned: s.drv.Provisioned(req.DataDir)}, nil
}

func (s *engineServer) BackendSocket(_ context.Context, req *proto.BackendSocketRequest) (*proto.BackendSocketResponse, error) {
	return &proto.BackendSocketResponse{Path: s.drv.BackendSocket(req.SocketDir, int(req.Port))}, nil
}

func (s *engineServer) ConnString(_ context.Context, req *proto.ConnStringRequest) (*proto.ConnStringResponse, error) {
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	envVar, url := s.drv.ConnString(inst, endpointFromProto(req.Endpoint))
	return &proto.ConnStringResponse{EnvVar: envVar, Url: url}, nil
}

func (s *engineServer) Plan(ctx context.Context, req *proto.PlanRequest) (*proto.SpawnPlan, error) {
	sp, ok := s.drv.(engine.Spawner)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "engine is not a Spawner")
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	plan, err := sp.Plan(ctx, inst, toolchainFromProto(req.Toolchain))
	if err != nil {
		return nil, err
	}
	return planToProto(plan), nil
}

func (s *engineServer) Converge(ctx context.Context, req *proto.ConvergeRequest) (*proto.Empty, error) {
	c, ok := s.drv.(engine.Converger)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "not a Converger")
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return &proto.Empty{}, c.Converge(ctx, inst, toolchainFromProto(req.Toolchain), endpointFromProto(req.Endpoint))
}

func (s *engineServer) Objects(_ context.Context, req *proto.ObjectsRequest) (*proto.ObjectsResponse, error) {
	inv, ok := s.drv.(engine.Inventory)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "not an Inventory")
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return &proto.ObjectsResponse{Objects: objectsToProto(inv.Objects(inst))}, nil
}

func (s *engineServer) Prune(ctx context.Context, req *proto.PruneRequest) (*proto.Empty, error) {
	pr, ok := s.drv.(engine.Pruner)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "not a Pruner")
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return &proto.Empty{}, pr.Prune(ctx, inst, toolchainFromProto(req.Toolchain), endpointFromProto(req.Endpoint), objectsFromProto(req.Removed))
}

func (s *engineServer) Env(_ context.Context, req *proto.EnvRequest) (*proto.EnvResponse, error) {
	ep, ok := s.drv.(engine.EnvProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "not an EnvProvider")
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return &proto.EnvResponse{Env: ep.Env(inst, endpointFromProto(req.Endpoint))}, nil
}

func (s *engineServer) BackendURL(_ context.Context, req *proto.BackendURLRequest) (*proto.BackendURLResponse, error) {
	bp, ok := s.drv.(engine.BackendProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "not a BackendProvider")
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return &proto.BackendURLResponse{Url: bp.BackendURL(inst)}, nil
}

func (s *engineServer) Supervised(_ context.Context, req *proto.SupervisedRequest) (*proto.SupervisedResponse, error) {
	lc, ok := s.drv.(engine.Lifecycle)
	if !ok {
		return &proto.SupervisedResponse{Supervised: false}, nil
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return &proto.SupervisedResponse{Supervised: lc.Supervised(inst)}, nil
}

func (s *engineServer) Hook(ctx context.Context, req *proto.HookRequest) (*proto.Empty, error) {
	h, ok := s.drv.(engine.Hooked)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "not Hooked")
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	switch req.Phase {
	case "pre_start":
		err = h.PreStart(ctx, inst)
	case "post_start":
		err = h.PostStart(ctx, inst)
	case "pre_stop":
		err = h.PreStop(ctx, inst)
	default:
		err = status.Errorf(codes.InvalidArgument, "unknown hook phase %q", req.Phase)
	}
	return &proto.Empty{}, err
}

func (s *engineServer) CheckHealth(ctx context.Context, req *proto.HealthRequest) (*proto.Empty, error) {
	hc, ok := s.drv.(engine.HealthChecker)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "not a HealthChecker")
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return &proto.Empty{}, hc.CheckHealth(ctx, inst)
}

func (s *engineServer) RestartPolicy(_ context.Context, req *proto.RestartPolicyRequest) (*proto.RestartSpec, error) {
	rs, ok := s.drv.(engine.Restartable)
	if !ok {
		return restartToProto(engine.RestartSpec{Policy: engine.RestartNo}), nil
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	return restartToProto(rs.RestartPolicy(inst)), nil
}

func (s *engineServer) AdvertisedAddr(_ context.Context, req *proto.AddrRequest) (*proto.AddrResponse, error) {
	pb, ok := s.drv.(engine.PortBinder)
	if !ok {
		return &proto.AddrResponse{Ok: false}, nil
	}
	inst, err := instanceFromProto(req.Instance)
	if err != nil {
		return nil, err
	}
	addr, has := pb.AdvertisedAddr(inst)
	return &proto.AddrResponse{Addr: addr, Ok: has}, nil
}

// capturingLocker feeds the host-supplied locked pin to drv.Resolve and captures
// whatever the driver records, so the server can return the new pin to core (which
// owns doze.lock).
type capturingLocker struct {
	in       engine.Pin
	inOK     bool
	out      engine.Pin
	recorded bool
}

func (l *capturingLocker) Get(string, engine.VersionSpec, engine.Platform) (engine.Pin, bool) {
	return l.in, l.inOK
}
func (l *capturingLocker) Record(_ string, _ engine.VersionSpec, _ engine.Platform, pin engine.Pin) {
	l.out, l.recorded = pin, true
}

// stubFetcher is a placeholder until plugins fetch via the doze-binaries module
// (Phase 1). A plugin that does not download (the echo test plugin) never calls it.
type stubFetcher struct{}

func (stubFetcher) ResolveMajor(string, string) (string, error) {
	return "", status.Error(codes.Unimplemented, "fetch not wired in this build")
}
func (stubFetcher) Ensure(context.Context, string, string, engine.Platform, string) (string, string, error) {
	return "", "", status.Error(codes.Unimplemented, "fetch not wired in this build")
}
