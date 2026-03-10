package schedulingclass

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const Name = "CozystackSchedulingClass"
const cycleStateKey framework.StateKey = "cozystack.io/schedulingclass/effective"

type Args struct {
	ClassLabelKey string                       `json:"classLabelKey,omitempty"`
	Classes       map[string]map[string]string `json:"classes,omitempty"`
}

type Effective struct {
	RequiredNodeLabels map[string]string
}

func (e *Effective) Clone() framework.StateData {
	if e == nil {
		return &Effective{}
	}

	out := &Effective{RequiredNodeLabels: map[string]string{}}
	for key, value := range e.RequiredNodeLabels {
		out.RequiredNodeLabels[key] = value
	}
	return out
}

type Plugin struct {
	args Args
}

var _ framework.PreFilterPlugin = &Plugin{}
var _ framework.FilterPlugin = &Plugin{}

func New(_ context.Context, obj runtime.Object, _ framework.Handle) (framework.Plugin, error) {
	args := Args{
		ClassLabelKey: "cozystack.io/schedulingClass",
		Classes:       map[string]map[string]string{},
	}

	if obj != nil {
		if raw, ok := obj.(*runtime.Unknown); ok && len(raw.Raw) > 0 {
			if err := json.Unmarshal(raw.Raw, &args); err != nil {
				return nil, fmt.Errorf("decode %s args: %w", Name, err)
			}
		}
	}

	return &Plugin{args: args}, nil
}

func (p *Plugin) Name() string { return Name }

// PreFilter: resolve SchedulingClass, compute “effective constraints”, store in CycleState.
func (p *Plugin) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	_ = ctx

	className := ""
	if pod.Labels != nil {
		className = pod.Labels[p.args.ClassLabelKey]
	}
	if className == "" {
		return nil, framework.NewStatus(framework.Success)
	}

	required := p.args.Classes[className]
	if len(required) == 0 {
		return nil, framework.NewStatus(framework.Success)
	}

	effective := &Effective{
		RequiredNodeLabels: required,
	}
	state.Write(cycleStateKey, effective)

	return nil, framework.NewStatus(framework.Success)
}

// Filter: enforce your effective constraints (read from CycleState) per node.
func (p *Plugin) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	_ = ctx

	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "nodeInfo has nil Node")
	}

	value, err := state.Read(cycleStateKey)
	if err == nil {
		effective, ok := value.(*Effective)
		if ok && effective != nil {
			for key, wantedValue := range effective.RequiredNodeLabels {
				if node.Labels[key] != wantedValue {
					return framework.NewStatus(
						framework.Unschedulable,
						fmt.Sprintf("SchedulingClass requires node label %q=%q", key, wantedValue),
					)
				}
			}
		}
	}

	if status := checkPodRequiredNodeAffinity(pod, node); status != nil {
		return status
	}

	return framework.NewStatus(framework.Success)
}

func (p *Plugin) PreFilterExtensions() framework.PreFilterExtensions { return nil }
