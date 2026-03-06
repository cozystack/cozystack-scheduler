/*
Package merge defines the interface for merging scheduling constraints
from a pod's spec with those defined in a SchedulingClass custom resource.

The flow is:
 1. Read the pod's annotation "scheduler.cozystack.io/scheduling-class" to
    identify the SchedulingClass CR.
 2. Fetch the SchedulingClass resource from the cluster.
 3. Merge the CR's constraints with the pod's own spec-level constraints.
 4. Return the merged result for use by the scheduling plugin.
*/
package merge

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// SchedulingClassAnnotation is the pod annotation that references a SchedulingClass CR by name.
const SchedulingClassAnnotation = "scheduler.cozystack.io/scheduling-class"

// InterPodAffinityTerms holds the merged set of inter-pod affinity and
// anti-affinity terms for a single pod, combining pod spec and SchedulingClass.
type InterPodAffinityTerms struct {
	RequiredAffinityTerms      []framework.AffinityTerm
	RequiredAntiAffinityTerms  []framework.AffinityTerm
	PreferredAffinityTerms     []framework.WeightedAffinityTerm
	PreferredAntiAffinityTerms []framework.WeightedAffinityTerm
}

// NodeAffinityTerms holds the merged set of node affinity terms for a single
// pod, combining pod spec and SchedulingClass.
type NodeAffinityTerms struct {
	// NodeSelector is the merged result of pod.Spec.NodeSelector and the CR's
	// node selector requirements. It is nil if neither source has any.
	NodeSelector map[string]string

	// RequiredNodeAffinity is the merged node selector from both the pod spec
	// and the SchedulingClass CR.
	RequiredNodeAffinity *v1.NodeSelector

	// PreferredNodeAffinity is the merged list of preferred scheduling terms.
	PreferredNodeAffinity []v1.PreferredSchedulingTerm
}

// TopologySpreadTerms holds the merged set of topology spread constraints for
// a single pod, combining pod spec and SchedulingClass.
type TopologySpreadTerms struct {
	Constraints []v1.TopologySpreadConstraint
}

// ConstraintMerger fetches a SchedulingClass CR for a pod and merges its
// constraints with those already present on the pod spec.
//
type ConstraintMerger interface {
	// MergeInterPodAffinity returns the merged inter-pod affinity terms for the
	// given pod. It reads the pod's annotation to find the SchedulingClass CR,
	// parses both the pod spec and CR terms, and returns the combined result.
	// If no SchedulingClass annotation is present, it returns nil.
	MergeInterPodAffinity(pod *v1.Pod) (*InterPodAffinityTerms, error)

	// MergeNodeAffinity returns the merged node affinity for the given pod.
	// It combines pod.Spec.NodeSelector, pod.Spec.Affinity.NodeAffinity, and
	// the SchedulingClass CR's node affinity terms.
	// If no SchedulingClass annotation is present, it returns nil (use pod spec as-is).
	MergeNodeAffinity(pod *v1.Pod) (*NodeAffinityTerms, error)

	// MergeTopologySpreadConstraints returns the merged topology spread constraints.
	// It appends the CR's constraints to those from pod.Spec.TopologySpreadConstraints.
	// If no SchedulingClass annotation is present, it returns nil (use pod spec as-is).
	MergeTopologySpreadConstraints(pod *v1.Pod) (*TopologySpreadTerms, error)
}
