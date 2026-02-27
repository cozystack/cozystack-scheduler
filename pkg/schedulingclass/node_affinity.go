package schedulingclass

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// checkPodRequiredNodeAffinity evaluates pod required node affinity against a node.
func checkPodRequiredNodeAffinity(pod *v1.Pod, node *v1.Node) *framework.Status {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		return nil
	}

	required := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if required == nil {
		return nil
	}

	selector, err := nodeaffinity.NewNodeSelector(required)
	if err != nil {
		return framework.NewStatus(framework.Error, fmt.Sprintf("invalid pod required node affinity: %v", err))
	}

	ok := selector.Match(node)
	if !ok {
		return framework.NewStatus(framework.Unschedulable, "node does not match pod required node affinity")
	}
	return nil
}
