package plugin

import (
	"context"

	"github.com/zclconf/go-cty/cty"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/plugin/proto"
)

// RemoteDecoder is implemented by a plugin driver: it decodes its own HCL block
// out-of-process. The config evaluator calls this instead of engine.ConfigDecoder
// for plugin engines, handing over the source file, the block's address, and the
// flattened eval-context variables; the result is an opaque *RawSpec.
type RemoteDecoder interface {
	DecodeRemote(file []byte, blockType, blockLabel string, vars map[string]cty.Value, baseDir string) (any, error)
}

var _ RemoteDecoder = (*pluginDriver)(nil)

// pluginDriver adapts a plugin's Engine gRPC client back to the in-tree
// engine.Driver + capability interfaces. It implements every optional capability
// method but no-ops the ones the plugin did not advertise, so the runtime's
// type-assertion dispatch keeps working while only advertised work crosses the
// wire. Compile-time: it is a Driver and a Spawner (every engine plugin runs via a
// SpawnPlan).
var (
	_ engine.Driver  = (*pluginDriver)(nil)
	_ engine.Spawner = (*pluginDriver)(nil)
)

type pluginDriver struct {
	client     proto.EngineClient
	engineType string
	caps       map[string]bool
}

func newPluginDriver(c proto.EngineClient) *pluginDriver {
	d := &pluginDriver{client: c, caps: map[string]bool{}}
	ctx := context.Background()
	if resp, err := c.Type(ctx, &proto.Empty{}); err == nil {
		d.engineType = resp.Type
	}
	if resp, err := c.Capabilities(ctx, &proto.Empty{}); err == nil {
		for _, cp := range resp.Capabilities {
			d.caps[cp] = true
		}
	}
	return d
}

func (d *pluginDriver) has(cap string) bool { return d.caps[cap] }

// ── engine.Driver ────────────────────────────────────────────────────────────
func (d *pluginDriver) Type() string { return d.engineType }

func (d *pluginDriver) Resolve(ctx context.Context, spec engine.VersionSpec, plat engine.Platform, lk engine.Locker, _ engine.Fetcher) (engine.Toolchain, error) {
	var locked *proto.Pin
	if pin, ok := lk.Get(d.engineType, spec, plat); ok {
		locked = pinToProto(pin)
	}
	resp, err := d.client.Resolve(ctx, &proto.ResolveRequest{
		Spec: string(spec), Platform: platformToProto(plat), Locked: locked,
	})
	if err != nil {
		return engine.Toolchain{}, err
	}
	if resp.Pin != nil {
		lk.Record(d.engineType, spec, plat, pinFromProto(resp.Pin))
	}
	return toolchainFromProto(resp.Toolchain), nil
}

func (d *pluginDriver) Provision(ctx context.Context, inst engine.Instance, tc engine.Toolchain) error {
	pi, err := instanceToProto(inst)
	if err != nil {
		return err
	}
	_, err = d.client.Provision(ctx, &proto.ProvisionRequest{Instance: pi, Toolchain: toolchainToProto(tc)})
	return err
}

func (d *pluginDriver) Provisioned(dataDir string) bool {
	resp, err := d.client.Provisioned(context.Background(), &proto.ProvisionedRequest{DataDir: dataDir})
	return err == nil && resp.Provisioned
}

func (d *pluginDriver) BackendSocket(socketDir string, port int) string {
	resp, err := d.client.BackendSocket(context.Background(), &proto.BackendSocketRequest{SocketDir: socketDir, Port: int32(port)})
	if err != nil {
		return ""
	}
	return resp.Path
}

func (d *pluginDriver) ConnString(inst engine.Instance, ep engine.Endpoint) (string, string) {
	pi, err := instanceToProto(inst)
	if err != nil {
		return "", ""
	}
	resp, err := d.client.ConnString(context.Background(), &proto.ConnStringRequest{Instance: pi, Endpoint: endpointToProto(ep)})
	if err != nil {
		return "", ""
	}
	return resp.EnvVar, resp.Url
}

// DecodeRemote sends the block's source file + flattened variables to the plugin,
// which decodes its own config and returns it as opaque gob bytes (a RawSpec).
func (d *pluginDriver) DecodeRemote(file []byte, blockType, blockLabel string, vars map[string]cty.Value, baseDir string) (any, error) {
	vj, err := varsToJSON(vars)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.DecodeConfig(context.Background(), &proto.DecodeRequest{
		File: file, BlockType: blockType, BlockLabel: blockLabel, Variables: vj, BaseDir: baseDir,
	})
	if err != nil {
		return nil, err
	}
	return &RawSpec{Bytes: resp.Spec}, nil
}

// ── engine.Spawner ───────────────────────────────────────────────────────────
func (d *pluginDriver) Plan(ctx context.Context, inst engine.Instance, tc engine.Toolchain) (engine.SpawnPlan, error) {
	pi, err := instanceToProto(inst)
	if err != nil {
		return engine.SpawnPlan{}, err
	}
	resp, err := d.client.Plan(ctx, &proto.PlanRequest{Instance: pi, Toolchain: toolchainToProto(tc)})
	if err != nil {
		return engine.SpawnPlan{}, err
	}
	return planFromProto(resp), nil
}

// ── optional capabilities (no-op unless advertised) ──────────────────────────
func (d *pluginDriver) Converge(ctx context.Context, inst engine.Instance, tc engine.Toolchain, ep engine.Endpoint) error {
	if !d.has(capConverger) {
		return nil
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return err
	}
	_, err = d.client.Converge(ctx, &proto.ConvergeRequest{Instance: pi, Toolchain: toolchainToProto(tc), Endpoint: endpointToProto(ep)})
	return err
}

func (d *pluginDriver) Objects(inst engine.Instance) []engine.Object {
	if !d.has(capInventory) {
		return nil
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return nil
	}
	resp, err := d.client.Objects(context.Background(), &proto.ObjectsRequest{Instance: pi})
	if err != nil {
		return nil
	}
	return objectsFromProto(resp.Objects)
}

func (d *pluginDriver) Prune(ctx context.Context, inst engine.Instance, tc engine.Toolchain, ep engine.Endpoint, removed []engine.Object) error {
	if !d.has(capPruner) {
		return nil
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return err
	}
	_, err = d.client.Prune(ctx, &proto.PruneRequest{Instance: pi, Toolchain: toolchainToProto(tc), Endpoint: endpointToProto(ep), Removed: objectsToProto(removed)})
	return err
}

func (d *pluginDriver) Env(inst engine.Instance, ep engine.Endpoint) map[string]string {
	if !d.has(capEnv) {
		return nil
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return nil
	}
	resp, err := d.client.Env(context.Background(), &proto.EnvRequest{Instance: pi, Endpoint: endpointToProto(ep)})
	if err != nil {
		return nil
	}
	return resp.Env
}

func (d *pluginDriver) BackendURL(inst engine.Instance) string {
	if !d.has(capBackendURL) {
		return ""
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return ""
	}
	resp, err := d.client.BackendURL(context.Background(), &proto.BackendURLRequest{Instance: pi})
	if err != nil {
		return ""
	}
	return resp.Url
}

func (d *pluginDriver) Supervised(inst engine.Instance) bool {
	if !d.has(capLifecycle) {
		return false
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return false
	}
	resp, err := d.client.Supervised(context.Background(), &proto.SupervisedRequest{Instance: pi})
	return err == nil && resp.Supervised
}

func (d *pluginDriver) PreStart(ctx context.Context, inst engine.Instance) error {
	return d.hook(ctx, inst, "pre_start")
}
func (d *pluginDriver) PostStart(ctx context.Context, inst engine.Instance) error {
	return d.hook(ctx, inst, "post_start")
}
func (d *pluginDriver) PreStop(ctx context.Context, inst engine.Instance) error {
	return d.hook(ctx, inst, "pre_stop")
}
func (d *pluginDriver) hook(ctx context.Context, inst engine.Instance, phase string) error {
	if !d.has(capHooked) {
		return nil
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return err
	}
	_, err = d.client.Hook(ctx, &proto.HookRequest{Instance: pi, Phase: phase})
	return err
}

func (d *pluginDriver) CheckHealth(ctx context.Context, inst engine.Instance) error {
	if !d.has(capHealth) {
		return nil
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return err
	}
	_, err = d.client.CheckHealth(ctx, &proto.HealthRequest{Instance: pi})
	return err
}

func (d *pluginDriver) RestartPolicy(inst engine.Instance) engine.RestartSpec {
	if !d.has(capRestartable) {
		return engine.RestartSpec{Policy: engine.RestartNo}
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return engine.RestartSpec{Policy: engine.RestartNo}
	}
	resp, err := d.client.RestartPolicy(context.Background(), &proto.RestartPolicyRequest{Instance: pi})
	if err != nil {
		return engine.RestartSpec{Policy: engine.RestartNo}
	}
	return restartFromProto(resp)
}

func (d *pluginDriver) AdvertisedAddr(inst engine.Instance) (string, bool) {
	if !d.has(capPortBinder) {
		return "", false
	}
	pi, err := instanceToProto(inst)
	if err != nil {
		return "", false
	}
	resp, err := d.client.AdvertisedAddr(context.Background(), &proto.AddrRequest{Instance: pi})
	if err != nil {
		return "", false
	}
	return resp.Addr, resp.Ok
}
